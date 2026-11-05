import {
  CoreApp,
  CustomVariableSupport,
  type DataQueryRequest,
  type DataQueryResponse,
  type DataSourceInstanceSettings,
  type ScopedVars,
  type TimeRange,
} from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv, type TemplateSrv } from '@grafana/runtime';
import type { Observable } from 'rxjs';

import { VariableQueryEditor } from './components/VariableQueryEditor';
import { interpolateLogstore, interpolateQuery } from './templating';
import type { SLSDataSourceOptions, SLSQuery } from './types';

/** Default SQL for new panels and alert rules: event count over time. */
export const DEFAULT_SQL =
  '* | select $__timeGroup(__time__) as time, count(*) as count group by time order by time limit 10000';

export class SLSDataSource extends DataSourceWithBackend<SLSQuery, SLSDataSourceOptions> {
  readonly defaultLogstore: string;
  readonly project: string;

  constructor(
    settings: DataSourceInstanceSettings<SLSDataSourceOptions>,
    private readonly templateSrv: TemplateSrv = getTemplateSrv()
  ) {
    super(settings);
    this.defaultLogstore = settings.jsonData.logstore ?? '';
    this.project = settings.jsonData.project ?? '';
    this.variables = new SLSVariableSupport(this);
  }

  /** Explore starts with a log search; panels and alert rules with an event count over time. */
  getDefaultQuery(app: CoreApp): Partial<SLSQuery> {
    return app === CoreApp.Explore ? { query: '*' } : { query: DEFAULT_SQL };
  }

  filterQuery(query: SLSQuery): boolean {
    return !query.hide;
  }

  applyTemplateVariables(query: SLSQuery, scopedVars: ScopedVars): SLSQuery {
    return {
      ...query,
      query: interpolateQuery(query.query ?? '', scopedVars, this.templateSrv),
      logstore: interpolateLogstore(query.logstore, scopedVars, this.templateSrv),
    };
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
