import type { DataSourceJsonData } from '@grafana/data';
import type { DataQuery } from '@grafana/schema';

/**
 * One SLS query. A search statement (`level: ERROR`) or SPL returns log rows; a statement with SQL
 * (`* | select ...`) returns a typed table, or time series when it has a time column.
 *
 * Fields from plugin 2.x (xcol, ycol, logsPerPage, totalLogs, ...) may still be present in saved
 * dashboards. The backend reads the ones that still make sense; the editor drops the rest on change.
 */
export interface SLSQuery extends DataQuery {
  query?: string;
  /** Logstore; empty means the data source default. */
  logstore?: string;
  /** Max log rows for search queries (SQL is limited by its LIMIT). */
  maxLines?: number;
  /** Column used as time for SQL results. Default `time`. */
  timeColumn?: string;
  /** Run SQL on the Dedicated SQL engine. */
  powerSql?: boolean;
  /** Special queries built by the plugin itself; never set by the query editor. */
  queryType?: typeof QUERY_TYPE_LOGS_VOLUME | typeof QUERY_TYPE_LOGS_CONTEXT;
  context?: LogsContextParams;
}

export const QUERY_TYPE_LOGS_VOLUME = 'logsVolume';
export const QUERY_TYPE_LOGS_CONTEXT = 'logsContext';

/** Identifies a log by its pack (SLS returns __tag__:__pack_id__ and __pack_meta__ with every row). */
export interface LogsContextParams {
  packId: string;
  packMeta: string;
  direction: 'backward' | 'forward';
  limit: number;
}

export const DEFAULT_MAX_LINES = 1000;
export const MAX_MAX_LINES = 5000;

export interface SLSDataSourceOptions extends DataSourceJsonData {
  project?: string;
  logstore?: string;
  /** Region ID such as cn-hangzhou; when set, requests are signed with AuthV4. */
  region?: string;
  /** Open the console on alibabacloud.com instead of aliyun.com. */
  international?: boolean;
  /** Time zone of date-time strings returned by SLS SQL. Default Asia/Shanghai (UTC+8). */
  sqlTimeZone?: string;
  /** Field holding the log level. Default `level`. */
  levelField?: string;
}

export interface SLSSecureJsonData {
  accessKeyId?: string;
  accessKeySecret?: string;
}
