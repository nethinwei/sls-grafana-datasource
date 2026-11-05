import type { AdHocVariableFilter, DataSourceJsonData } from '@grafana/data';
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
  /** Dashboard ad hoc filters, translated by the backend with the field catalog. */
  adhocFilters?: AdHocVariableFilter[];
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
  /** How log rows are processed and presented. */
  logs?: LogsConfig;
}

export interface LogsConfig {
  /** Rules reshaping log rows; absent means DEFAULT_FIELD_RULES, an empty list means none. */
  rules?: FieldRule[];
  /** Field holding the row timestamp after the rules. Default `timestamp`. */
  timeField?: string;
  /** Field holding the log level: level colours and the logs volume. Default `level`. */
  levelField?: string;
  line?: {
    /** Fields opening the log line, in order (e.g. host.name, host.ip). */
    fields?: string[];
    /** Append the other business fields to the line. Default true. */
    rest?: boolean;
  };
}

export interface SLSSecureJsonData {
  accessKeyId?: string;
  accessKeySecret?: string;
}

export type FieldRuleType = 'time' | 'rename' | 'extract' | 'drop' | 'default' | 'template';

/**
 * A rule reshaping log rows for display; rules run in order. Only `rename` maps back to SLS for
 * filtering; fields produced by the other rules exist only in Grafana.
 */
export interface FieldRule {
  type: FieldRuleType;
  /** Source field (time, rename, extract). */
  from?: string;
  /** Target field (time, rename, extract, default, template). */
  to?: string;
  /** Field to hide (drop). */
  field?: string;
  /** Regular expression (extract). */
  pattern?: string;
  /** extract: value from the match, `$1` by default; template: `${field}` placeholders. */
  template?: string;
  /** Value used when the field is absent or empty (default). */
  value?: string;
  /** Field holding the nanosecond part of a seconds timestamp, e.g. `__time_ns_part__` (time). */
  nanos?: string;
  /** rfc3339 (default), unix_ms or unix_ns (time); ignored when writing the time field. */
  format?: 'rfc3339' | 'unix_ms' | 'unix_ns';
}

export const DEFAULT_TIME_FIELD = 'timestamp';

/**
 * Rules used when the data source configures none: build the row timestamp from SLS seconds plus
 * nanoseconds (when the collector sends them) and hide the raw fields. Mirrors the backend.
 */
export const DEFAULT_FIELD_RULES: FieldRule[] = [
  { type: 'time', from: '__time__', nanos: '__time_ns_part__', to: DEFAULT_TIME_FIELD },
  { type: 'drop', field: '__time__' },
  { type: 'drop', field: '__time_ns_part__' },
];

/** SLS meta fields added by LoongCollector / Logtail, renamed to OpenTelemetry semantic names. */
export const OTEL_FIELD_RULES: FieldRule[] = [
  { type: 'rename', from: '__tag__:__hostname__', to: 'host.name' },
  { type: 'rename', from: '__tag__:__host_ip__', to: 'host.ip' },
  { type: 'rename', from: '__tag__:__path__', to: 'log.file.path' },
];

/**
 * A field as the user sees it, from the backend's field catalog: the logstore index combined with
 * what the field rules do.
 */
export interface CatalogField {
  /** Name shown in Grafana. */
  name: string;
  /** text, long, double, json (from the index). */
  type?: string;
  /** `field: value` search works; undefined when the index could not be read. */
  indexed?: boolean;
  /** SQL statistics enabled (SQL, group by, value suggestions); undefined when unknown. */
  stats?: boolean;
  /** Field to write in SLS statements; absent for fields produced by rules (Grafana only). */
  slsName?: string;
  /** Rule type that produced or renamed the field. */
  rule?: FieldRuleType;
}

export interface Catalog {
  fields: CatalogField[];
  /** False when the index could not be read: fields come from sample logs. */
  indexKnown: boolean;
  message?: string;
}
