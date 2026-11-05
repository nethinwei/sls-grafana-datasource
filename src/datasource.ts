import {
  type AdHocVariableFilter,
  CoreApp,
  CustomVariableSupport,
  type DataFrame,
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
import { lastValueFrom, type Observable, tap } from 'rxjs';

import { VariableQueryEditor } from './components/VariableQueryEditor';
import { addStringFilter, hasFieldFilter, hasWhereFilter, toggleFieldFilter, toggleWhereFilter } from './queryModify';
import { interpolateLogstore, interpolateQuery } from './templating';
import {
  type Catalog,
  type CatalogField,
  QUERY_TYPE_LOGS_CONTEXT,
  QUERY_TYPE_LOGS_VOLUME,
  type SLSDataSourceOptions,
  type SLSQuery,
} from './types';

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
const CATALOG_TTL_MS = 5 * 60 * 1000; // same as the backend cache

interface CatalogEntry {
  at: number;
  promise: Promise<Catalog>;
  value?: Catalog;
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
  /** Field catalogs per logstore ('' = default), see getCatalog. */
  private readonly catalogs = new Map<string, CatalogEntry>();
  /**
   * What query results said about fields the rules produced (see buildLogsFrame in the backend):
   * name → SPL expression for flattened JSON fields, '' for fields that exist only in Grafana.
   */
  private readonly learnedFields = new Map<string, string>();

  constructor(
    settings: DataSourceInstanceSettings<SLSDataSourceOptions>,
    private readonly templateSrv: TemplateSrv = getTemplateSrv()
  ) {
    super(settings);
    this.defaultLogstore = settings.jsonData.logstore ?? '';
    this.project = settings.jsonData.project ?? '';
    this.levelField = settings.jsonData.logs?.levelField || 'level';
    this.variables = new SLSVariableSupport(this);
  }

  // ---------- field catalog ----------

  /**
   * Fields of a logstore as the user sees them: index state, SQL statistics, SLS name of renamed
   * fields and the rule behind derived ones. Completion, query hints, the field browser and filters
   * all read it. Cached for a few minutes; refresh after changing the index.
   */
  getCatalog(logstore?: string, refresh = false): Promise<Catalog> {
    const key = logstore ?? '';
    const hit = this.catalogs.get(key);
    if (hit && !refresh && Date.now() - hit.at < CATALOG_TTL_MS) {
      return hit.promise;
    }
    const entry: CatalogEntry = { at: Date.now(), promise: Promise.resolve({ fields: [], indexKnown: false }) };
    entry.promise = this.postResource<Partial<Catalog>>('api/catalog', { logstore, refresh }).then(
      (c) => {
        entry.value = { fields: c.fields ?? [], indexKnown: Boolean(c.indexKnown), message: c.message };
        return entry.value;
      },
      (e) => {
        this.catalogs.delete(key);
        throw e;
      }
    );
    this.catalogs.set(key, entry);
    return entry.promise;
  }

  /** A field of an already loaded catalog; undefined when not loaded or unknown. */
  private catalogField(name: string, logstore?: string): CatalogField | undefined {
    return this.catalogs.get(this.resolveLogstore(logstore))?.value?.fields.find((f) => f.name === name);
  }

  private resolveLogstore(logstore?: string): string {
    const resolved = interpolateLogstore(logstore, {}, this.templateSrv) ?? '';
    return resolved === this.defaultLogstore ? '' : resolved;
  }

  query(request: DataQueryRequest<SLSQuery>): Observable<DataQueryResponse> {
    return super.query(request).pipe(tap((res) => res.data?.forEach((frame) => this.learnFields(frame))));
  }

  private learnFields(frame: DataFrame) {
    const custom = frame.meta?.custom as
      { grafanaOnlyFields?: string[]; fieldExprs?: Record<string, string> } | undefined;
    custom?.grafanaOnlyFields?.forEach((name) => this.learnedFields.set(name, ''));
    Object.entries(custom?.fieldExprs ?? {}).forEach(([name, expr]) => this.learnedFields.set(name, expr));
  }

  /** Explore starts with a log search; panels and alert rules with an event count over time. */
  getDefaultQuery(app: CoreApp): Partial<SLSQuery> {
    return app === CoreApp.Explore ? { query: '*' } : { query: DEFAULT_SQL };
  }

  filterQuery(query: SLSQuery): boolean {
    return !query.hide;
  }

  /**
   * Template variables. Dashboard ad hoc filters travel with the query: the backend turns them into
   * search conditions or SPL where clauses with the field catalog.
   */
  applyTemplateVariables(query: SLSQuery, scopedVars: ScopedVars, filters?: AdHocVariableFilter[]): SLSQuery {
    const out: SLSQuery = {
      ...query,
      query: interpolateQuery(query.query ?? '', scopedVars, this.templateSrv),
      logstore: interpolateLogstore(query.logstore, scopedVars, this.templateSrv),
    };
    if (filters?.length && query.queryType !== QUERY_TYPE_LOGS_CONTEXT) {
      out.adhocFilters = filters;
    }
    if (!query.queryType) {
      // Field filters (➕ / ➖) need the catalog synchronously: load it with the first query.
      this.getCatalog(this.resolveLogstore(out.logstore) || undefined).catch(() => undefined);
    }
    return out;
  }

  // ---------- Explore: ➕ / ➖ on log fields ----------

  /**
   * SLS field behind a field shown in Grafana: detected_level is the level field, renamed fields
   * (host.name) go back to their SLS name (__tag__:__hostname__). Fields added by the plugin or
   * produced by rules exist only in Grafana and cannot be filtered.
   */
  private filterTarget(
    key: string,
    logstore?: string,
    frame?: DataFrame
  ): { key: string; indexed: boolean; expr?: string } | undefined {
    const name = key === 'detected_level' ? this.levelField : key;
    if (SYNTHETIC_FIELDS.has(name)) {
      return undefined;
    }
    const field = this.catalogField(name, logstore);
    if (field) {
      if (field.slsName) {
        return { key: field.slsName, indexed: field.indexed !== false };
      }
      return field.expr ? { key: name, indexed: false, expr: field.expr } : undefined;
    }
    // Fields the rules produced in this or earlier results (see buildLogsFrame in the backend).
    if (frame) {
      this.learnFields(frame);
    }
    const learned = this.learnedFields.get(name);
    if (learned !== undefined) {
      return learned ? { key: name, indexed: false, expr: learned } : undefined;
    }
    return { key: name, indexed: true }; // catalog not loaded yet or field not seen: try as is
  }

  /**
   * Indexed fields get a search condition (`key: "value"`); fields without an index an SPL
   * `| where` clause, since SLS search cannot use them; flattened JSON fields a where clause on
   * json_extract_scalar, like Loki's `| json | key="value"`.
   */
  toggleQueryFilter(query: SLSQuery, filter: ToggleFilterAction): SLSQuery {
    const target = this.filterTarget(filter.options.key, query.logstore, filter.frame);
    if (!target) {
      return query;
    }
    const negate = filter.type === 'FILTER_OUT';
    const text = query.query ?? '*';
    return {
      ...query,
      query: target.indexed
        ? toggleFieldFilter(text, target.key, filter.options.value, negate)
        : toggleWhereFilter(text, target.key, filter.options.value, negate, target.expr),
    };
  }

  queryHasFilter(query: SLSQuery, filter: QueryFilterOptions): boolean {
    const target = this.filterTarget(filter.key, query.logstore);
    if (!target) {
      return false;
    }
    const text = query.query ?? '';
    return (
      hasFieldFilter(text, target.key, filter.value) || hasWhereFilter(text, target.key, filter.value, target.expr)
    );
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
          frame: action.frame,
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

  /** Filterable fields: SLS fields under their Grafana names, without SLS meta fields. */
  async getTagKeys(_options?: DataSourceGetTagKeysOptions<SLSQuery>): Promise<MetricFindValue[]> {
    const catalog = await this.getCatalog();
    return catalog.fields.filter((f) => f.slsName && !f.name.startsWith('__')).map((f) => ({ text: f.name }));
  }

  /** Most frequent values of a field, restricted by the other filters. */
  async getTagValues(options: DataSourceGetTagValuesOptions<SLSQuery>): Promise<MetricFindValue[]> {
    const res = await this.postResource<{ values?: string[] }>('api/fieldValues', {
      key: options.key,
      filters: options.filters.filter((f) => f.key !== options.key),
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
