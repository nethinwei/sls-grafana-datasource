# 阿里云日志服务（SLS）Grafana 数据源

[English](README_EN.md)

在 Grafana 中查询阿里云日志服务（SLS）：检索与 SPL 返回日志，SQL 返回表格或时间序列，可用于看板、Explore 和告警。

> 本仓库是 [aliyun/aliyun-log-grafana-datasource-plugin](https://github.com/aliyun/aliyun-log-grafana-datasource-plugin) 的 fork，面向 Grafana 13 重写，删除了与 Grafana 生态重复或过时的功能。插件 ID 不变，可以直接替换官方版本。设计与取舍见 [docs/design/grafana-13-adaptation.md](docs/design/grafana-13-adaptation.md)。

## 兼容性

- Grafana **≥ 13.2**
- 插件未签名，需要在 Grafana 配置中允许加载：`GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=aliyun-log-service-datasource`

## 安装

从 [Releases](../../releases) 下载 `aliyun-log-service-datasource-<版本>.zip`，用同目录的 `.sha256` 校验后解压到 Grafana 的插件目录（默认 `/var/lib/grafana/plugins`），重启 Grafana。

## 配置数据源

| 配置 | 说明 |
|---|---|
| Endpoint | project 所在地域的接入点，例如 `cn-hangzhou.log.aliyuncs.com`；在阿里云内网使用 `-intranet` 接入点 |
| Project | SLS project |
| Default logstore | 查询未指定 logstore 时使用，健康检查也用它 |
| Region | 地域 ID，例如 `cn-hangzhou`。填写后使用 V4 签名 |
| AccessKey | 建议使用只授权了目标 logstore 的 RAM 用户 |
| SQL time zone | SQL 返回的日期时间字符串（`timestamp` 列、`from_unixtime`、`date_format`）的时区，SLS 默认 UTC+8，所以默认 `Asia/Shanghai` |
| Logs | 日志的处理与展示：级别字段、时间字段、日志行字段、字段规则，见下文「日志处理」 |

最小权限：查询只需要 `log:GetLogStoreLogs`。以下权限可选：`log:GetLogStore`（健康检查）、`log:ListLogStores`（logstore 下拉列表）、`log:GetIndex`（字段目录：补全、字段提示、字段浏览与筛选，没有时从样本日志中归纳）、`log:GetLogStoreContextLogs`（日志上下文）、`log:GetLogStoreHistogram`（级别字段未开启统计时的日志量）。

## 查询

查询语句就是 SLS 的语法，结果形式由语句决定：

| 语句 | 结果 |
|---|---|
| 检索：`level: ERROR and service_name: api` | 日志。正文是这条日志自己的字段（`key="value"`，不含 `__tag__` 等元数据），时间精确到纳秒（采集端开启纳秒时间戳时） |
| SPL：`* \| where cost_ms > 1000` | 日志 |
| SQL：`* \| select service_name, count(*) as c group by service_name` | 表格，列按 SLS 返回的类型输出（数值、布尔、时间） |
| 带时间列的 SQL | 时间序列。时间列默认叫 `time`（可在查询选项里修改）；其余的字符串列作为维度，每个组合一条序列 |

时间序列示例：

```sql
* | select $__timeGroup(__time__) as time, service_name, count(*) as requests
    group by time, service_name order by time limit 10000
```

### 宏

| 宏 | 展开为 |
|---|---|
| `$__timeGroup(col)` | `(col - col % <面板间隔秒数>)`；指定间隔：`$__timeGroup(__time__, 5m)` |
| `$__interval_s` | 面板间隔，单位秒 |
| `$__timeFrom` / `$__timeTo` | 查询时间范围的起止，unix 秒 |

查询的时间范围总是由 Grafana 的时间选择器决定，不需要在语句里写时间条件。

### 变量

- 在检索语句里写 `key: $var`。多值变量会自动展开成 `(key: "a" or key: "b")`；选「All」且全部值为 `*` 时展开为 `key: *`。
- 在 SQL 里使用 Grafana 的格式化写法，例如 `where service_name in (${service_name:sqlstring})`。
- 变量查询：一条 SQL，第一列作为选项，例如 `* | select service_name from log group by service_name limit 500`。

### 查询选项

| 选项 | 说明 |
|---|---|
| Max lines | 检索返回的最多日志条数，默认 1000，最多 5000；SQL 的行数由 LIMIT 决定 |
| Time column | SQL 结果中的时间列 |
| Dedicated SQL | 使用 SLS SQL 独享版（资源更多，单独计费） |

查询检查器（Query inspector）会显示实际执行的语句和 SLS 的执行统计；结果不完整（Incomplete）时会给出提示。

## 日志浏览

| 功能 | 说明 |
|---|---|
| 日志量直方图 | Explore 中按级别统计整个时间范围的日志量（由 SLS 计算，不只是已返回的行）。级别字段开启了 SQL 统计时按级别拆分，否则只显示总量。只对检索语句生效 |
| 点字段筛选 | Explore 的日志详情中点 ➕ / ➖，在检索语句中加入 `key: "value"` / `not key: "value"`；再点一次移除 |
| 看板筛选 | 看板日志面板中点 ➕ / ➖ 会生成看板的 Filters 变量，作用于该数据源的所有查询（包括 SQL 面板）。筛选由后端按字段目录翻译：改过名的字段用 SLS 原名，有索引的字段用检索条件，没有索引的字段用 SPL `\| where`，`=~` / `!~` 中的简单正则转为通配符，其余用 `regexp_like`。无法翻译的筛选会在面板上提示 |
| 日志上下文 | 日志行菜单中的「Show context」：由 SLS 从同一来源读取前后的日志（GetContextLogs），不受当前查询条件限制 |
| 级别 | 「Logs → Level field」（默认 `level`）用于级别着色和日志量 |
| 字段补全与提示 | 查询编辑器按字段目录补全：显示 Grafana 中的字段名、插入 SLS 字段名，并标出能否检索、能否用于 SQL。检索没有索引的字段、在 SLS 语句里写 Grafana 字段名、SQL 用到没有统计的字段时，编辑器会给出提示 |
| 字段浏览 | 查询编辑器的「Fields」按钮列出 logstore 的全部字段：类型、能否检索、能否用于 SQL、来自哪条规则；修改索引后可刷新 |

上下文依赖每行日志的 `__tag__:__pack_id__` 与 `__pack_meta__`：插件会给检索和 SPL 查询自动追加 `| with_pack_meta`。

## 日志处理

数据源配置的「Logs」决定日志怎样显示，只影响 Grafana 中看到的结果，不改动 SLS 中的数据。

| 配置 | 说明 |
|---|---|
| Level field | 级别字段（规则处理之后的名字），默认 `level` |
| Time field | 作为日志时间的字段，由 `time` 规则写入，默认 `timestamp` |
| Line fields | 日志行开头依次显示的字段，例如 `host.name`、`host.ip`，用来一眼看出日志来自哪台机器。规则产生的字段只有列在这里才进入日志行 |
| Other fields in line | 是否在后面接着显示其余业务字段（不含 `__` 开头的元数据），按日志写入时的顺序，默认开。级别字段已由 Grafana 单独显示，不重复放进日志行，需要时可列入 Line fields |
| Field rules | 按顺序执行的字段规则 |

字段规则：

| 规则 | 示例 | 说明 |
|---|---|---|
| `time` | `__time__` + `__time_ns_part__` → `timestamp` | 由秒和可选的纳秒字段合成时间。目标是时间字段时成为日志时间；采集端没有纳秒字段时纳秒按 0 处理 |
| `rename` | `__tag__:__hostname__` → `host.name` | 改名。筛选与补全会自动换回 SLS 原名 |
| `extract` | `log.file.path` 按 `/logs/(.+)\.log` 取 `$1` → `service.name` | 正则提取，支持命名分组 `${name}` |
| `drop` | `__topic__` | 隐藏字段 |
| `default` | `deployment.environment` = `prod` | 字段缺失或为空时的取值 |
| `template` | `${service}/${env}` → `app` | 由其他字段拼出新字段，`$$` 表示 `$` |

没有配置规则时使用默认规则：用 `__time__` 和 `__time_ns_part__` 合成 `timestamp`，再隐藏这两个字段。配置页的「Add OpenTelemetry renames」会追加 `host.name`、`host.ip`、`log.file.path` 三条改名规则。除 `rename` 外，规则产生的字段只存在于 Grafana，不能用于筛选。`__tag__:__pack_id__` 与 `__pack_meta__` 是日志上下文需要的字段，不能改名或隐藏；无效的规则会被跳过，并在「Save & test」中列出。

用配置文件创建数据源时：

```yaml
jsonData:
  project: my-project
  logstore: app-log
  logs:
    levelField: level
    line:
      fields: [host.name, host.ip]
    rules:
      - { type: time, from: __time__, nanos: __time_ns_part__, to: timestamp }
      - { type: drop, field: __time__ }
      - { type: drop, field: __time_ns_part__ }
      - { type: rename, from: __tag__:__hostname__, to: host.name }
      - { type: rename, from: __tag__:__host_ip__, to: host.ip }
```

## 从 2.x 迁移

| 2.x | 现在 |
|---|---|
| `ycol` 的 `分组列#:#数值列` 写法，`xcol` 的 `bar` / `pie` / `map` / `stat` / `table` 等图表模式 | 删除。SQL 输出带类型的表格，带时间列时自动按字符串列拆分序列；图表形式交给 Grafana 面板 |
| `xcol` 填时间列名 | 仍然可用，等同于 Time column |
| `$1m` 换算成秒、`#time_begin` / `#time_end` | 删除，改用宏 |
| `logsPerPage` / `currentPage` / `totalLogs` | 改为 Max lines；旧值仍会被读取 |
| 时序库（MetricStore / PromQL） | 删除。请用 Grafana 的 Prometheus 数据源连接 SLS 时序库的 Prometheus 兼容接口 |
| SLS Trace（`xcol=trace`） | 删除 |
| STS 免登跳转控制台（roleArn） | 删除；保留「SLS console」按钮，用自己的阿里云账号登录 |
| 变量描述含 `field_search` 时展开为 `"name":"value"` | 仍然兼容，推荐改用 `key: $var` |

## 开发

需要 Node.js 24 与 Go 1.26。

```bash
npm ci
npm run dev          # 前端，监听变更
npm run test:ci      # 前端测试
go test ./pkg/       # 后端测试
make linux           # 构建 linux 后端到 dist/
```

推送形如 `2.39.2-fork.1` 的 tag（与 `package.json` 版本一致）后，Release 工作流会从源码构建并发布安装包。

## 许可

Apache-2.0
