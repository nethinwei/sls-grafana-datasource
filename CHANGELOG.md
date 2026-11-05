# Changelog

# v0.1.0 (2026-11-05)

首个独立版本。衍生自阿里云官方插件 aliyun/aliyun-log-grafana-datasource-plugin 2.39.2，面向 Grafana 13.2 重写并独立维护。官方插件 2.39.2 及以前的版本记录见其仓库的 [CHANGELOG](https://github.com/aliyun/aliyun-log-grafana-datasource-plugin/blob/master/CHANGELOG.md)。

**与官方插件的区别（不兼容）**

- 插件 ID 改为 `nethinwei-sls-datasource`，与官方插件 `aliyun-log-service-datasource` 是两个插件；迁移见 README「从官方插件迁移」。
- 只支持 Grafana ≥ 13.2。
- 不读取 2.x 的查询写法：xcol/ycol 图表模式、`logsPerPage`/`totalLogs`、`field_search`、`__custom__@` logstore、两列变量约定。
- 删除 MetricStore（PromQL）与自定义请求头、SLS Trace、STS 免登跳转、`$1m` 与 `#time_begin` / `#time_end` 写法。
- 许可证按仓库 LICENSE 为 MIT（官方插件的 package.json 误写为 Apache-2.0），来源与署名见 NOTICE；不再使用阿里云的产品 logo。
- 构建产物不再提交到仓库，由 Release 工作流从源码构建；安装包包含 Go 依赖的许可证。

**查询与告警**

- 后端基于 grafana-plugin-sdk-go v0.296 与 GetLogsV3 重写：返回 HTTP 状态与错误来源（下游 / 插件），查询检查器显示实际语句与 SLS 执行统计，支持取消与超时。
- SLS 返回不完整结果时自动重试到完整；告警查询仍不完整时报错，看板查询给出提示。网络错误与 5xx 自动重试。
- 用 SLS 返回的 `hasSQL` 区分检索与 SQL，SPL 按日志返回。
- SQL 结果按列类型输出，超过 2^53 的整数按字符串保留原值；Format 选择 Time series（带时间列时按字符串列拆分序列）或 Table。时间列兼容常见格式，无法解析的行跳过并提示。
- 宏：`$__timeGroup`（支持表达式参数）、`$__interval_s`、`$__timeFrom`、`$__timeTo`；数据源配置新增 SQL 时区。默认 SQL 按时间倒序截断。
- 单次结果的内存上限可配置：SQL 转时间序列的单元格数、`json` 展开的字段数。
- 健康检查与 logstore 列表支持只授权到 logstore 的 AccessKey；endpoint 默认使用 https。

**日志**

- 日志浏览：完整时间范围的日志量直方图（按级别）；Explore 中点字段 ➕/➖ 筛选；看板 Ad hoc 筛选；日志上下文（GetContextLogs）；日志行带 `id`，检索与 SPL 自动追加 `| with_pack_meta`。
- 日志处理（`jsonData.logs`）：级别字段、时间字段、可排序的日志行字段（Line fields）与不进日志行的字段（Hidden from line）；字段规则 `time` / `rename` / `extract` / `drop` / `default` / `template` / `json`。纳秒时间由默认的 `time` 规则合成。
- 日志行按日志写入时的字段顺序拼接，值按 logfmt 转义，嵌套 JSON 原样输出；级别字段不重复出现在日志行。
- `json` 规则（默认包含）按 Loki `| json` 的规则展开嵌套对象；展开的字段可筛选（有 JSON 索引时用检索，否则用 SPL `json_extract_scalar`，同时匹配采集端已展开的同名字段）。
- 「Add OpenTelemetry renames」生成 `host_name`、`host_ip`、`log_file_path`。
- 筛选由后端按字段目录翻译：改名字段用 SLS 原名，无索引字段用 SPL `| where`，正则用 `regexp_like` 匹配整个值，否定条件对缺失字段按空值处理，与 Loki 一致；无法翻译时在面板上提示。

**编辑器**

- 前端迁移到 @grafana/create-plugin、@grafana/* 13.2、React 19；新的查询编辑器（SLS 语法高亮）、配置页与变量编辑器；检索中的 `key: $var` 多值变量自动展开。
- 字段目录（`/api/catalog`）：索引与样本日志合并，再按规则换算。补全显示 Grafana 字段名并插入 SLS 字段名；编辑器提示无索引检索、Grafana 字段名误用与无统计的 SQL 字段；字段浏览。
