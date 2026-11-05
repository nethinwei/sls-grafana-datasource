import {
  type AdHocVariableFilter,
  CoreApp,
  CustomVariableSupport,
  type DataQueryRequest,
  type DataQueryResponse,
  type DataSourceGetTagKeysOptions,
  type DataSourceGetTagValuesOptions,
  type DataSourceInstanceSettings,
  type DataSourceWithLogsContextSupport,
  type DataSourceWithQueryModificationSupport,
  type DataSourceWithSupplementaryQueriesSupport,
  type DataSourceWithToggleableQueryFiltersSupport,
  dateTime,
  type LogRowContextOptions,
  LogRowContextQueryDirection,
  type LogRowModel,
  type MetricFindValue,
  type QueryFilterOptions,
  type QueryFixAction,
  type ScopedVars,
  SupplementaryQueryType,
  type SupplementaryQueryOptions,
  type TimeRange,
  type ToggleFilterAction,
} from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv, type TemplateSrv } from '@grafana/runtime';
import { lastValueFrom, type Observable } from 'rxjs';

import { VariableQueryEditor } from './components/VariableQueryEditor';
import { adHocCondition, addStringFilter, applyAdHocFilters, hasFieldFilter, toggleFieldFilter } from './queryModify';
import { interpolateLogstore, interpolateQuery } from './templating';
import { QUERY_TYPE_LOGS_CONTEXT, QUERY_TYPE_LOGS_VOLUME, type SLSDataSourceOptions, type SLSQuery } from './types';

/** Fields the plugin adds to log rows; filtering on them makes no sense in SLS. */
const SYNTHETIC_FIELDS = new Set([
  'message',
  'id',
  'time',
  '__time__',
  '__time_ns_part__',
  '__pack_meta__',
  '__index_number__',
]);

const CONTEXT_WINDOW_MS = 24 * 3600 * 1000; // GetContextLogs reads at most one day around the log

export interface FieldInfo {
  name: string;
  type?: string;
  /** SQL statistics enabled; needed for value suggestions and group-by. */
  stats?: boolean;
}

/** A search or SPL statement without SQL returns logs; only plain search has a volume histogram. */
function isPlainSearch(query: SLSQuery): boolean {
  return !query.queryType && !(query.query ?? '').includes('|');
}

/** Default SQL for new panels and alert rules: event count over time. */
export const DEFAULT_SQL =
  '* | select $__timeGroup(__time__) as time, count(*) as count group by time order by time limit 10000';

export class SLSDataSource
  extends DataSourceWithBackend<SLSQuery, SLSDataSourceOptions>
  implements
    DataSourceWithLogsContextSupport<SLSQuery>,
    DataSourceWithSupplementaryQueriesSupport<SLSQuery>,
    DataSourceWithToggleableQueryFiltersSupport<SLSQuery>,
    DataSourceWithQueryModificationSupport<SLSQuery>
{
  readonly defaultLogstore: string;
  readonly project: string;
  readonly levelField: string;

  constructor(
    settings: DataSourceInstanceSettings<SLSDataSourceOptions>,
    private readonly templateSrv: TemplateSrv = getTemplateSrv()
  ) {
    super(settings);
    this.defaultLogstore = settings.jsonData.logstore ?? '';
    this.project = settings.jsonData.project ?? '';
    this.levelField = settings.jsonData.levelField || 'level';
    this.variables = new SLSVariableSupport(this);
  }

  /** Explore starts with a log search; panels and alert rules with an event count over time. */
  getDefaultQuery(app: CoreApp): Partial<SLSQuery> {
    return app === CoreApp.Explore ? { query: '*' } : { query: DEFAULT_SQL };
  }

  filterQuery(query: SLSQuery): boolean {
    return !query.hide;
  }

  /** Template variables, then the dashboard's ad hoc filters as search conditions. */
  applyTemplateVariables(query: SLSQuery, scopedVars: ScopedVars, filters?: AdHocVariableFilter[]): SLSQuery {
    const interpolated = interpolateQuery(query.query ?? '', scopedVars, this.templateSrv);
    return {
      ...query,
      query: query.queryType === QUERY_TYPE_LOGS_CONTEXT ? interpolated : applyAdHocFilters(interpolated, filters),
      logstore: interpolateLogstore(query.logstore, scopedVars, this.templateSrv),
    };
  }

  // ---------- Explore: ➕ / ➖ on log fields ----------

  /** Grafana shows the level of rows through detected_level when the level field has another name. */
  private filterKey(key: string): string | undefined {
    if (key === 'detected_level') {
      return this.levelField;
    }
    return SYNTHETIC_FIELDS.has(key) ? undefined : key;
  }

  toggleQueryFilter(query: SLSQuery, filter: ToggleFilterAction): SLSQuery {
    const key = this.filterKey(filter.options.key);
    if (!key) {
      return query;
    }
    return {
      ...query,
      query: toggleFieldFilter(query.query ?? '*', key, filter.options.value, filter.type === 'FILTER_OUT'),
    };
  }

  queryHasFilter(query: SLSQuery, filter: QueryFilterOptions): boolean {
    const key = this.filterKey(filter.key);
    return Boolean(key) && hasFieldFilter(query.query ?? '', key!, filter.value);
  }

  getSupportedQueryModifications(): string[] {
    return ['ADD_FILTER', 'ADD_FILTER_OUT', 'ADD_STRING_FILTER', 'ADD_STRING_FILTER_OUT'];
  }

  modifyQuery(query: SLSQuery, action: QueryFixAction): SLSQuery {
    const value = action.options?.value ?? '';
    switch (action.type) {
      case 'ADD_FILTER':
      case 'ADD_FILTER_OUT':
        return this.toggleQueryFilter(query, {
          type: action.type === 'ADD_FILTER' ? 'FILTER_FOR' : 'FILTER_OUT',
          options: { key: action.options?.key ?? '', value },
        });
      case 'ADD_STRING_FILTER':
      case 'ADD_STRING_FILTER_OUT':
        return value
          ? { ...query, query: addStringFilter(query.query ?? '*', value, action.type === 'ADD_STRING_FILTER_OUT') }
          : query;
      default:
        return query;
    }
  }

  // ---------- dashboards: ad hoc filters ----------

  async getFields(logstore?: string, range?: TimeRange): Promise<FieldInfo[]> {
    const res = await this.postResource<{ fields?: FieldInfo[] }>('api/fields', {
      logstore,
      from: range?.from.valueOf(),
      to: range?.to.valueOf(),
    });
    return res.fields ?? [];
  }

  async getTagKeys(options?: DataSourceGetTagKeysOptions<SLSQuery>): Promise<MetricFindValue[]> {
    const fields = await this.getFields(undefined, options?.timeRange);
    return fields.filter((f) => !f.name.startsWith('__')).map((f) => ({ text: f.name }));
  }

  /** Most frequent values of a field, restricted by the other filters. */
  async getTagValues(options: DataSourceGetTagValuesOptions<SLSQuery>): Promise<MetricFindValue[]> {
    const others = options.filters.filter((f) => f.key !== options.key);
    const res = await this.postResource<{ values?: string[] }>('api/fieldValues', {
      key: options.key,
      query: adHocCondition(others),
      from: options.timeRange?.from.valueOf(),
      to: options.timeRange?.to.valueOf(),
    });
    return (res.values ?? []).map((v) => ({ text: v }));
  }

  // ---------- log context ----------

  /**
   * Lines before or after a log, read by SLS from the same source (GetContextLogs). Needs
   * __tag__:__pack_id__ and __pack_meta__, which the backend requests for every log query.
   */
  async getLogRowContext(
    row: LogRowModel,
    options?: LogRowContextOptions,
    query?: SLSQuery
  ): Promise<DataQueryResponse> {
    const packId = fieldValue(row, '__tag__:__pack_id__');
    const packMeta = fieldValue(row, '__pack_meta__');
    if (!packId || !packMeta) {
      return { data: [] };
    }
    const direction = options?.direction === LogRowContextQueryDirection.Forward ? 'forward' : 'backward';
    const target: SLSQuery = {
      refId: `log-context-${row.dataFrame.refId ?? 'A'}-${direction}`,
      datasource: { type: this.type, uid: this.uid },
      logstore: query?.logstore,
      queryType: QUERY_TYPE_LOGS_CONTEXT,
      context: { packId, packMeta, direction, limit: options?.limit ?? 50 },
    };
    const from = dateTime(row.timeEpochMs - CONTEXT_WINDOW_MS);
    const to = dateTime(row.timeEpochMs + CONTEXT_WINDOW_MS);
    const request = {
      targets: [target],
      range: { from, to, raw: { from, to } },
      scopedVars: options?.scopedVars ?? {},
      interval: '1s',
      intervalMs: 1000,
      requestId: target.refId,
      timezone: 'browser',
      app: CoreApp.Explore,
      startTime: Date.now(),
    } as DataQueryRequest<SLSQuery>;
    return lastValueFrom(this.query(request));
  }

  // ---------- logs volume ----------

  getSupportedSupplementaryQueryTypes(): SupplementaryQueryType[] {
    return [SupplementaryQueryType.LogsVolume];
  }

  /** Log counts over time by level, computed by SLS over the whole range (not only returned rows). */
  getSupplementaryQuery(options: SupplementaryQueryOptions, query: SLSQuery): SLSQuery | undefined {
    if (options.type !== SupplementaryQueryType.LogsVolume || !isPlainSearch(query)) {
      return undefined;
    }
    return { ...query, refId: `log-volume-${query.refId}`, queryType: QUERY_TYPE_LOGS_VOLUME };
  }

  getSupplementaryRequest(
    type: SupplementaryQueryType,
    request: DataQueryRequest<SLSQuery>,
    options?: SupplementaryQueryOptions
  ): DataQueryRequest<SLSQuery> | undefined {
    if (type !== SupplementaryQueryType.LogsVolume) {
      return undefined;
    }
    const targets = request.targets
      .filter((q) => !q.hide)
      .map((q) => this.getSupplementaryQuery(options ?? { type }, q))
      .filter((q): q is SLSQuery => q !== undefined);
    return targets.length ? { ...request, targets } : undefined;
  }

  getQueryDisplayText(query: SLSQuery): string {
    const logstore = query.logstore || this.defaultLogstore;
    return (logstore ? `${logstore}: ` : '') + (query.query ?? '');
  }

  /** Logstores of the project; least-privilege AccessKeys only get the default logstore back. */
  async getLogstores(): Promise<string[]> {
    const res = await this.postResource<{ data?: string[] | null }>('api/getLogstoreList', {});
    return res.data ?? [];
  }

  /** SLS console URL showing the same query and time range. */
  async getConsoleUrl(query: SLSQuery, range: TimeRange, scopedVars: ScopedVars = {}): Promise<string> {
    const q = this.applyTemplateVariables(query, scopedVars);
    const params = new URLSearchParams({
      encode: 'base64',
      queryString: base64(q.query || '*'),
      queryTimeType: '99',
      startTime: String(range.from.unix()),
      endTime: String(range.to.unix()),
    });
    const res = await this.postResource<{ url?: string; message?: string }>('api/gotoSLS', {
      logstore: q.logstore,
      encoding: params.toString(),
    });
    if (!res.url) {
      throw new Error(res.message || 'cannot build the SLS console URL');
    }
    return res.url;
  }
}

function fieldValue(row: LogRowModel, name: string): string | undefined {
  const field = row.dataFrame.fields.find((f) => f.name === name);
  const value = field?.values[row.rowIndex];
  return typeof value === 'string' && value !== '' ? value : undefined;
}

function base64(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let bin = '';
  bytes.forEach((b) => (bin += String.fromCharCode(b)));
  return btoa(bin);
}

/** Variable query in dashboards saved by plugin 2.x: a plain string, or wrapped by schema v2. */
type StoredVariableQuery = SLSQuery | string | { __legacyStringValue?: string };

export function toVariableQuery(stored: StoredVariableQuery | undefined): SLSQuery {
  if (typeof stored === 'string') {
    return { refId: 'variable', query: stored };
  }
  if (stored && '__legacyStringValue' in stored && typeof stored.__legacyStringValue === 'string') {
    return { refId: 'variable', query: stored.__legacyStringValue };
  }
  const q = (stored ?? {}) as Partial<SLSQuery>;
  return { ...q, refId: q.refId || 'variable' };
}

/**
 * Variables run a normal query through the backend; Grafana turns the first field of the result
 * (or fields named `text` / `value`) into options, e.g.
 * `* | select service_name from log group by service_name limit 500`.
 */
export class SLSVariableSupport extends CustomVariableSupport<SLSDataSource, SLSQuery> {
  editor = VariableQueryEditor;

  constructor(private readonly datasource: SLSDataSource) {
    super();
  }

  query(request: DataQueryRequest<SLSQuery>): Observable<DataQueryResponse> {
    const targets = request.targets.map((t) => toVariableQuery(t as StoredVariableQuery));
    return this.datasource.query({ ...request, targets });
  }
}
