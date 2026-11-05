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

Least privilege: queries need only `log:GetLogStoreLogs`. `log:GetLogStore` improves the health check and `log:ListLogStores` fills the logstore list; without them the plugin falls back to the default logstore.

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
