# SLS 插件 Grafana 13 适配设计

目标：在 Grafana 13.2 上，让 SLS 数据源的查询、日志浏览、变量、告警体验尽量接近 Grafana 原生日志数据源（Loki），同时充分利用 SLS 自身的接口能力。

- 基线：上游 `aliyun/aliyun-log-grafana-datasource-plugin` 2.39.2。
- 主线：`main`，版本号 `2.39.2-fork.N`。插件 ID 不变，数据源配置与已保存的查询向后兼容。
- 目标运行环境：**只支持 Grafana ≥ 13.2**（`grafanaDependency: ">=13.2.0"`），不再兼顾 7.x～12.x。

资料来源：
- SLS：OpenAPI 文档（`help.aliyun.com/zh/sls/developer-reference/api-sls-2020-12-30-*`）、`aliyun-log-go-sdk` v0.1.83 / v0.1.131 源码。
- Grafana：v13.2.5 源码、独立仓库 `grafana/grafana-loki-datasource` v13.2.5（Loki 已不在主仓库）、plugin-tools 文档、`grafana-plugin-sdk-go` release notes。

---

## 1. 能力对照

每一项都写明：Grafana 期望的接口，用 SLS 的哪个能力实现，以及具体怎么做。

### 1.1 查询与响应语义

| Grafana 期望 | SLS 能力 | 做法 |
|---|---|---|
| `DataResponse.Status`、`ErrorSource`：区分是用户写错，还是插件或下游故障 | SLS 错误码与 HTTP 状态码 | SLS 返回的 4xx（语法错误、权限、配额）一律标记为 Downstream；插件内部错误标记为 Plugin。修掉「查询成功也返回 500」 |
| `FrameMeta.ExecutedQueryString`：查询检查器里显示实际执行的语句 | 变量替换后的查询语句，以及 `GetLogsV3` meta 里的 `whereQuery` / `aggQuery` | 显示最终发给 SLS 的语句和时间范围 |
| `FrameMeta.Notices`：提示结果不完整 | meta 里的 `progress=Incomplete`、`limited` | Incomplete 作为 warning 提示，不当成错误。SQL 没写 LIMIT 被截到 100 行时，也给出提示 |
| `FrameMeta.Stats`：显示执行统计 | meta 里的 `processedRows`、`elapsedMillisecond`、`scanBytes`、`count` | 原样放进 Stats，在查询检查器里可见 |
| 字段带类型：数值列可以正确排序，可以设置单位 | meta 里的 `columnTypes`（只有 SQL 结果有；实测取值为 `varchar`、`long`、`double`、`timestamp`、`boolean`） | SQL 结果按 `columnTypes` 输出数值、时间、布尔字段；没有类型时按值推断。`timestamp` 和日期字符串不带时区，SLS 按 UTC+8 输出，所以按数据源配置的「SQL 时区」（默认 `Asia/Shanghai`）解析 |
| 取消与超时 | SDK 方法不接收 ctx；默认超时 60s | 每条查询用 ctx 派生超时控制等待。请求被取消时立即返回，不再等 SLS 的结果；同时缩短 SDK 自身的超时时间 |
| 结果不完整时重试 | `GetLogsToCompletedV3`：自动重试，最多 20 次或 5 分钟 | 看板查询不自动重试，以免拖慢页面；告警查询重试，因为告警需要完整结果 |

查询接口改用 `GetLogsV3`，直接读取结构化的 meta，不再解析旧 header 拼成的 `Contents` 字符串。时间范围用 `ByNano` 系列参数传入，`accurate` 保持 true，保证纳秒级有序。

### 1.2 日志数据帧

| Grafana 期望 | SLS 能力 | 做法 |
|---|---|---|
| 符合 dataplane 规范的日志帧：`meta.type = log-lines`、`preferredVisualisationType = logs` | — | 日志查询（不含 SQL）输出规范格式 |
| `timestamp`：time 类型，纳秒精度 | `__time__` + `__time_ns_part__`（采集端开启 `EnableTimestampNanosecond` 时才有） | 合成纳秒时间。SDK 序列化时会自动带上 `nanos`，前端会用到 |
| `body`：正文 | 这条日志自己的字段 | 按 `key="value"` 拼接，只拼这条日志自己有的、不以 `__` 开头的字段，按字段名排序（PR 1） |
| `severity`：日志级别 | 日志里的级别字段 | 从数据源配置的「级别字段」读取，默认 `level`。注意：13.2 默认不再从正文推断级别，必须显式提供 |
| `id`：唯一标识 | `__tag__:__pack_id__` + `__pack_meta__` | 用 pack_id 加 pack_meta，没有时用内容哈希。用于去重和永久链接 |
| `labels`：结构化字段 | 全部字段，包括 `__tag__:*` | 放进 JSON。日志详情里能看到所有字段，➕/➖ 筛选也基于这些字段 |

SQL 查询、带 xcol/ycol 的图表查询，帧结构不变。

### 1.3 日志交互

| Grafana 能力 | Grafana 接口 | SLS 能力 | 做法 |
|---|---|---|---|
| **Explore** 里点字段 ➕/➖ | `toggleQueryFilter`、`queryHasFilter`、`modifyQuery`（`ADD_FILTER`、`ADD_FILTER_OUT`、`ADD_STRING_FILTER`） | 检索语法 `key: "v"`、`not key: "v"` | 往查询的检索部分（第一个 `\|` 之前）追加或移除条件。同一个条件再点一次就移除；➖ 会先删掉同名的 ➕ 条件 |
| **看板**里点字段 ➕/➖，以及 Filters 控件 | `getTagKeys`、`getTagValues`，以及 `applyTemplateVariables(query, scopedVars, filters)` 的第三个参数 | GetIndex（字段名和类型）；字段取值用带统计的 SQL：`select k, count(*) group by k` | 字段列表取自索引。只有开启了统计（`doc_value`）的字段才提供取值候选。筛选条件翻译成检索语法，拼进查询 |
| 日志上下文 | `getLogRowContext`、`getLogRowContextQuery` | GetContextLogs：`pack_id`、`pack_meta`、前后各最多 100 行 | 日志查询自动在末尾追加 `\|with_pack_meta`，取回上下文需要的两个字段。没有 pack_id 的日志不显示上下文入口。`supportsAdjustableWindow = false`，因为 SLS 的上下文按行数取，不按时间窗口 |
| 日志量直方图 | `getSupplementaryQueries` / `getSupplementaryRequest`（LogsVolume） | SQL：`<检索> \| select __time__ - __time__ % N as t, level, count(*) group by t, level`；退路是 GetHistograms，只有总数 | 按级别每个分组一条序列，number 字段带 `labels{level}`，refId 加 `log-volume-` 前缀。级别字段没建统计时，退回 GetHistograms 显示总量 |
| 在 SLS 控制台打开 | 数据链接 / 面板菜单 | 控制台 URL：`queryString` 用 base64 编码，带时间范围 | 沿用现有的 gotoSLS，并带上当前查询和时间范围 |

### 1.4 变量与查询编辑器

| Grafana 期望 | 现状 | 做法 |
|---|---|---|
| 在 `applyTemplateVariables` 里替换变量，不修改传入的查询对象 | 在 `query()` 里替换，而且直接修改了 targets | 移到 `applyTemplateVariables`，返回新对象。`interpolateVariablesInQueries` 用默认实现，「在 Explore 中打开」和告警因此都正确 |
| 多值变量的格式化 | 私有约定 `field_search` | 新增自动识别：查询里出现 `key: $var` 时，展开为 `(key: "a" or key: "b")`，并转义引号。`field_search` 继续兼容 |
| `CustomVariableSupport` | 已废弃的 `setVariableQueryEditor`，加上手动 POST `/api/ds/query`（带数字 id） | 改为 `CustomVariableSupport`，查询走 `DataSourceWithBackend` |
| `getDefaultQuery(app)` | 固定一条统计 SQL | Explore 里默认 `*`（日志）；告警里默认统计 SQL；看板沿用当前的默认查询 |
| `getQueryDisplayText`、`filterQuery` | 无 | 在查询历史里显示 logstore 和语句；隐藏的查询不执行 |

### 1.5 健康检查

| 现状 | 做法 |
|---|---|
| 调用 `GetProject`，只有 logstore 级权限的 AK 必定失败 | 先调 `GetLogStore`（需要 `log:GetLogStore`）；失败时退而查 1 分钟窗口、`line=1` 的日志（需要 `log:GetLogStoreLogs`）。在 `JSONDetails` 里写明缺少哪个 RAM Action |

---

## 2. 工程与依赖

| 项 | 现在 | 目标 |
|---|---|---|
| 后端 SDK | `grafana-plugin-sdk-go` v0.102.0 | v0.296.x，与 Grafana 13.2.5 一致。改用 `datasource.Manage` 和带 ctx 的实例工厂；跨版本的破坏性改动见调研记录 |
| SLS SDK | `aliyun-log-go-sdk` v0.1.83 | v0.1.131 以上，以便使用 GetLogsV3、`GetHistogramsV2`（interval）。v0.1.117 起 `IsAccurate` 由 bool 改为 `*bool` |
| 前端工具链 | `@grafana/toolkit` 9.1.2，需要 Node 16 | `@grafana/create-plugin` 迁移；`@grafana/*` 13.2.x；React 19，由 Grafana 运行时提供，不打进插件包 |
| 构建 | 依赖阿里内网包，外部无法构建 | 已完成：用仓库自带的 SSRF 防护替换；CI 交叉编译；推送 tag 自动发布（阶段 0） |
| 安全 | STS 签名请求走 http，把凭据放在 URL 里 | 已改为 https（阶段 0） |

---

## 3. 删除的历史功能（阶段 1）

这些功能与 Grafana 13 的生态冲突或已经过时，fork 里直接删除，不保留兼容开关：

| 删除 | 原因 | 替代 |
|---|---|---|
| MetricStore（PromQL）及自定义请求头 | 与 Grafana 原生 Prometheus 数据源重复；请求头的值存在明文的 jsonData 里 | 用 Grafana 的 Prometheus 数据源连 SLS 时序库 |
| xcol / ycol 图表模式（`#:#`、bar、pie、map、stat、table） | 为 Grafana 7 的面板设计 | SQL 结果统一输出带类型的帧；有时间列时自动长表转宽表；饼图、柱状图交给 Grafana 面板和变换 |
| `$1m` 换算、`#time_begin` / `#time_end` | 私有语法 | 宏：`$__timeGroup(col[, 5m])`、`$__interval_s`、`$__timeFrom`、`$__timeTo` |
| 分页三件套（`logsPerPage`、`currentPage`、`totalLogs`） | 不填就只返回 1 行 | `maxLines`（默认 1000，上限 5000），插件内部翻页；旧字段仍会被读取 |
| SLS Trace（`xcol=trace`） | 链路数据通常放在 Tempo、Jaeger 等专用后端，Grafana 有原生数据源；维护前端转换代码的成本高 | — |
| STS 免登跳转（roleArn） | 凭据流转最多、安全面最大 | 普通的「在 SLS 控制台打开」 |
| 流式接口空实现、`legacy_compatible`、`flow_chart_max_points` | 模板遗留 / 废代码 | — |

新增的数据源配置：`sqlTimeZone`（默认 `Asia/Shanghai`）。

## 4. 不做的事

- **Logs Drilldown**：Grafana 只为 Loki 提供。
- **Tempo trace → logs**：Grafana 只支持固定几种日志数据源。日志 → 链路沿用字段链接，链路 → 日志沿用「链路详情」看板。
- **实时追踪**：SLS 没有流式推送接口。
- **Group by**（`getGroupByKeys`）：对日志查询意义不大，以后需要再加。
- **PDC / secure socks proxy**：用不到；阿里云 SDK 也不走 Grafana 的 httpclient。
- **签名与上架 grafana.com**：需要换插件 ID，等于发布一个新插件。

---

## 5. 分阶段

每个阶段都在本地 Grafana 13.2 上用真实的 SLS logstore 和浏览器验证。每个阶段结束时提交、发一个 `2.39.2-fork.N` 版本。

| 阶段 | 内容 | 状态 |
|---|---|---|
| 0 | 去掉内网依赖、加 CI 和发布工作流、STS 改走 https；日志正文只拼本条日志自己的字段（PR 1） | 已合入 main |
| 1 | 后端：两个 SDK 升级、响应语义（1.1）、健康检查（1.5）；删除 MetricStore、SLS Trace、STS 跳转、xcol/ycol 图表模式（第 3 节） | 已合入 main |
| 2 | 前端：迁移到 create-plugin、13.2 与 React 19、变量与默认查询（1.4）；构建产物不再入库，CI/Release 构建前端 | 代码完成，本地 Grafana 已验证 |
| 3 | 日志：规范日志帧（1.2）、➕/➖ 与 Ad hoc 筛选、上下文、日志量（1.3） | — |

阶段 1 和阶段 2 互不依赖，可以交替进行；阶段 3 依赖前两个阶段。

## 6. 待验证的风险

1. **每条日志查询都追加 `|with_pack_meta`，对性能和返回结构的影响**：阶段 3 开始前先实测。如果有影响，改为只在 Explore 或日志面板里追加。
2. **SLS SDK 不支持取消**：取消只能让 Grafana 不再等待，SLS 那边的查询会继续跑完。
3. ~~`columnTypes` 的取值~~：已实测（见 1.1）。
4. **改成规范日志帧后，对现有看板的影响**：用表格面板显示日志查询结果时，列会变成 `timestamp`、`body`、`severity`、`labels`。如果看板里日志都用日志面板、表格都用 SQL，则不受影响；否则这是一个破坏性变化。
5. **用 GetIndex 做字段补全需要 `log:GetIndex` 权限**：没有这个权限时，➕/➖ 仍然可用，只是 Filters 控件里没有字段候选。
