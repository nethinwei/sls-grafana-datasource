# Alibaba Cloud Log Service (SLS) data source for Grafana

[中文](README.md)

Query Alibaba Cloud Log Service (SLS) from Grafana: search and SPL return logs, SQL returns tables or time series, for dashboards, Explore and alerting.

> This is a fork of [aliyun/aliyun-log-grafana-datasource-plugin](https://github.com/aliyun/aliyun-log-grafana-datasource-plugin), rewritten for Grafana 13 with features that duplicate the Grafana ecosystem or are outdated removed. The plugin ID is unchanged, so it replaces the upstream plugin in place. See [docs/design/grafana-13-adaptation.md](docs/design/grafana-13-adaptation.md) for the design (Chinese).

## Compatibility

- Grafana **≥ 13.2**
- The plugin is unsigned; allow it with `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=aliyun-log-service-datasource`

## Install

Download `aliyun-log-service-datasource-<version>.zip` from [Releases](../../releases), verify it with the `.sha256` file, unzip it into the Grafana plugin directory (default `/var/lib/grafana/plugins`) and restart Grafana.

## Configure

| Setting | Description |
|---|---|
| Endpoint | Endpoint of the project's region, e.g. `cn-hangzhou.log.aliyuncs.com`; use the `-intranet` endpoint inside Alibaba Cloud |
| Project | SLS project |
| Default logstore | Used when a query does not choose a logstore, and by the health check |
| Region | Region ID such as `cn-hangzhou`; when set, requests use signature V4 |
| AccessKey | Prefer a RAM user granted only on the logstores you query |
| SQL time zone | Zone of date-time strings returned by SQL (timestamp columns, `from_unixtime`, `date_format`). SLS uses UTC+8, hence the default `Asia/Shanghai` |
| Logs | How logs are processed and shown: level field, time field, line fields, field rules; see "Log processing" below |

Least privilege: queries need only `log:GetLogStoreLogs`. Optional: `log:GetLogStore` (health check), `log:ListLogStores` (logstore list), `log:GetIndex` (field catalog: completion, hints, field browser and filters; otherwise inferred from sample logs), `log:GetLogStoreContextLogs` (log context), `log:GetLogStoreHistogram` (volume when the level field has no SQL statistics).

## Query

Queries use SLS syntax; the statement decides the result:

| Statement | Result |
|---|---|
| Search: `level: ERROR and service_name: api` | Logs. The line holds the log's own fields (`key="value"`, without `__tag__` and other meta fields); nanosecond time when the collector sends it |
| SPL: `* \| where cost_ms > 1000` | Logs |
| SQL: `* \| select service_name, count(*) as c group by service_name` | Table with typed columns (number, boolean, time) |
| SQL with a time column | Time series. The time column is `time` by default (configurable in the query options); other string columns are dimensions, one series per combination |

```sql
* | select $__timeGroup(__time__) as time, service_name, count(*) as requests
    group by time, service_name order by time limit 10000
```

### Macros

| Macro | Expands to |
|---|---|
| `$__timeGroup(col)` | `(col - col % <panel interval seconds>)`; fixed interval: `$__timeGroup(__time__, 5m)` |
| `$__interval_s` | Panel interval in seconds |
| `$__timeFrom` / `$__timeTo` | Range start / end, unix seconds |

The time range always comes from Grafana's time picker; do not add time conditions to the statement.

### Variables

- In search, write `key: $var`. Multi-value variables expand to `(key: "a" or key: "b")`; "All" with the `*` value expands to `key: *`.
- In SQL, use Grafana formats, e.g. `where service_name in (${service_name:sqlstring})`.
- Variable query: SQL whose first column holds the options, e.g. `* | select service_name from log group by service_name limit 500`.

### Query options

| Option | Description |
|---|---|
| Max lines | Max log rows for search, default 1000, at most 5000. SQL rows are limited by LIMIT |
| Time column | Time column of SQL results |
| Dedicated SQL | Run SQL on the SLS Dedicated SQL engine (more resources, billed separately) |

The Query inspector shows the executed statement and SLS execution statistics; incomplete results are reported as a warning.

## Logs

| Feature | Description |
|---|---|
| Logs volume | Explore shows log counts by level over the whole range, computed by SLS (not only the returned rows). Split by level when the level field has SQL statistics enabled, otherwise the total. Search statements only |
| Filter from fields | ➕ / ➖ in Explore's log details add `key: "value"` / `not key: "value"` to the search; clicking again removes it |
| Dashboard filters | ➕ / ➖ in dashboard logs panels create the dashboard Filters variable, applied to every query of this data source (SQL panels included). The backend translates filters with the field catalog: renamed fields use their SLS name, indexed fields become search conditions, fields without an index SPL `\| where`; simple regexes in `=~` / `!~` become wildcards, others `regexp_like`. Filters that cannot be translated are reported on the panel |
| Log context | "Show context" in the log row menu: SLS reads the lines around the log from the same source (GetContextLogs), regardless of the query filters |
| Level | "Logs → Level field" (default `level`) drives level colours and the volume |
| Completion and hints | The query editor completes fields from the catalog: Grafana name shown, SLS name inserted, with whether the field is searchable and usable in SQL. It warns about search on fields without an index, Grafana-only names in SLS statements, and SQL on fields without statistics |
| Field browser | The "Fields" button lists the logstore's fields: type, searchable, SQL statistics, and the rule behind them; refresh after changing the index |

Log context needs `__tag__:__pack_id__` and `__pack_meta__` on every row: the plugin appends `| with_pack_meta` to search and SPL queries.

## Log processing

"Logs" in the data source settings controls how logs are shown. It only changes what Grafana shows; SLS data is untouched.

| Setting | Description |
|---|---|
| Level field | Level field (name after the rules), default `level` |
| Time field | Field that becomes the log time, written by a `time` rule, default `timestamp` |
| Line fields | Fields opening the log line, in an adjustable order, e.g. `service_name`, `host_name`. Listing `msg` last makes it the boundary: standard fields before it, the application's own fields after it. Fields missing from a log are skipped; fields produced by rules join the line only when listed here |
| Hidden from line | Fields kept out of the line; they stay in the log details |

Line values are quoted as in logfmt, with `"` and `\` escaped. Nested objects (slog groups, logged structs or maps; collectors store them as JSON text) and arrays are written as they are, unquoted, and pretty-printed in the log details.

With a `json` rule, nested fields filter like any other field: by search when the field has a JSON index in SLS (`request.method: "POST"`), otherwise with SPL `| where json_extract_scalar("request", '$["method"]') = 'POST'`, like Loki's `| json | request_method="POST"`. Negative operators (`!=`, `!~`) treat a missing field as empty, as Loki does; regexes match the whole value.
| Other fields in line | Append the other business fields (not `__` meta fields) in the order they were written, default on. The level field is shown by Grafana as the level and left out of the line unless listed in Line fields |
| Field rules | Rules run in order |

Field rules:

| Rule | Example | Description |
|---|---|---|
| `time` | `__time__` + `__time_ns_part__` → `timestamp` | Time from a seconds field plus an optional nanoseconds field. Targeting the time field makes it the log time; without a nanoseconds field the nanoseconds are 0 |
| `rename` | `__tag__:__hostname__` → `host.name` | Rename. Filters and completion map back to the SLS name |
| `extract` | `log.file.path` with `/logs/(.+)\.log`, `$1` → `service.name` | Regex extraction, named groups as `${name}` |
| `drop` | `__topic__` | Hide a field |
| `default` | `deployment.environment` = `prod` | Value when the field is absent or empty |
| `template` | `${service}/${env}` → `app` | New field from other fields, `$$` is a literal `$` |
| `json` | `request` → `request_method`, `request_header_Content_Type` | Flattens nested JSON objects into fields, as Loki's `\| json`: keys joined with `_`, characters other than letters, digits and `_` become `_`, arrays and nulls skipped, `_extracted` appended on a name clash. Without from, every field holding a JSON object is flattened |

Without configured rules the defaults apply: build `timestamp` from `__time__` and `__time_ns_part__`, hide both, and flatten every nested object with `json`. "Add OpenTelemetry renames" appends renames to `host.name`, `host.ip` and `log.file.path`. Except for `rename`, fields produced by rules exist only in Grafana and cannot be filtered on. `__tag__:__pack_id__` and `__pack_meta__` are needed by log context and cannot be renamed or dropped; invalid rules are skipped and listed by "Save & test".

Provisioning:

```yaml
jsonData:
  project: my-project
  logstore: app-log
  logs:
    levelField: level
    line:
      fields: [service_name, host.name, host.ip, msg]
      exclude: [source]
    rules:
      - { type: time, from: __time__, nanos: __time_ns_part__, to: timestamp }
      - { type: drop, field: __time__ }
      - { type: drop, field: __time_ns_part__ }
      - { type: json }
      - { type: rename, from: __tag__:__hostname__, to: host.name }
      - { type: rename, from: __tag__:__host_ip__, to: host.ip }
```

## Migrating from 2.x

| 2.x | Now |
|---|---|
| `ycol` `group#:#value`, `xcol` chart modes `bar` / `pie` / `map` / `stat` / `table` | Removed. SQL returns a typed table; with a time column it is split into series by the string columns. Let Grafana panels choose the chart |
| `xcol` naming the time column | Still works, same as Time column |
| `$1m` to seconds, `#time_begin` / `#time_end` | Removed; use macros |
| `logsPerPage` / `currentPage` / `totalLogs` | Replaced by Max lines; old values are still read |
| MetricStore (PromQL) | Removed. Use Grafana's Prometheus data source with the SLS MetricStore Prometheus-compatible API |
| SLS Trace (`xcol=trace`) | Removed |
| STS console sign-in (roleArn) | Removed; the "SLS console" button remains and uses your own Alibaba Cloud login |
| `field_search` variable description rendering `"name":"value"` | Still supported; prefer `key: $var` |

## Development

Requires Node.js 24 and Go 1.26.

```bash
npm ci
npm run dev          # frontend, watch mode
npm run test:ci      # frontend tests
go test ./pkg/       # backend tests
make linux           # build linux backends into dist/
```

Pushing a tag such as `2.39.2-fork.1` (matching `package.json`) runs the Release workflow, which builds from source and publishes the package.

## License

Apache-2.0
