// type
export enum CompletionItemKind {
  Method = 0,
  Function = 1,
  Constructor = 2,
  Field = 3,
  Variable = 4,
  Class = 5,
  Struct = 6,
  Interface = 7,
  Module = 8,
  Property = 9,
  Event = 10,
  Operator = 11,
  Unit = 12,
  Value = 13,
  Constant = 14,
  Enum = 15,
  EnumMember = 16,
  Keyword = 17,
  Text = 18,
  Color = 19,
  File = 20,
  Reference = 21,
  Customcolor = 22,
  Folder = 23,
  TypeParameter = 24,
  User = 25,
  Issue = 26,
  Snippet = 27,
}

export const keywords = [
  {
    label: 'SELECT',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'FROM',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'AS',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'DISTINCT',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'GROUP',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'WHERE',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'BY',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'HAVING',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'ORDER',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'LIMIT',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'OR',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'AND',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'IN',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'NOT',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'BETWEEN',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'LIKE',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'IS',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'ASC',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'DESC',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'SUBSTRING',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'CASE',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'WHEN',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'THEN',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'ELSE',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'END',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'JOIN',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'INNER',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'LEFT',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'RIGHT',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'OUTER',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'ON',
    type: CompletionItemKind.Keyword,
  },
  {
    label: 'UNION',
    type: CompletionItemKind.Keyword,
  },
];

export const operators = [
  // Logical
  {
    label: 'ALL',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'AND',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'ANY',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'BETWEEN',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'IN',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'LIKE',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'NOT',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'OR',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'SOME',
    type: CompletionItemKind.Operator,
  },
  // Set
  {
    label: 'EXCEPT',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'INTERSECT',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'UNION',
    type: CompletionItemKind.Operator,
  },
  // Join
  {
    label: 'APPLY',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'CROSS',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'FULL',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'INNER',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'JOIN',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'LEFT',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'OUTER',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'RIGHT',
    type: CompletionItemKind.Operator,
  },
  // Predicates
  {
    label: 'CONTAINS',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'FREETEXT',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'IS',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'NULL',
    type: CompletionItemKind.Operator,
  },
  // Pivoting
  {
    label: 'PIVOT',
    type: CompletionItemKind.Operator,
  },
  {
    label: 'UNPIVOT',
    type: CompletionItemKind.Operator,
  },
  // Merging
  {
    label: 'MATCHED',
    type: CompletionItemKind.Operator,
  },
];

export const functions = {
  // 通用聚合函数
  arbitrary: {
    type: CompletionItemKind.Field,
  },
  avg: {
    type: CompletionItemKind.Field,
  },
  checksum: {
    type: CompletionItemKind.Field,
  },
  COUNT: {
    type: CompletionItemKind.Field,
  },
  count_if: {
    type: CompletionItemKind.Field,
  },
  geometric_mean: {
    type: CompletionItemKind.Field,
  },
  max_by: {
    type: CompletionItemKind.Field,
  },
  min_by: {
    type: CompletionItemKind.Field,
  },
  max: {
    type: CompletionItemKind.Field,
  },
  min: {
    type: CompletionItemKind.Field,
  },
  sum: {
    type: CompletionItemKind.Field,
  },
  bitwise_and_agg: {
    type: CompletionItemKind.Field,
  },
  bitwise_or_agg: {
    type: CompletionItemKind.Field,
  },
  // 映射函数
  histogram: {
    type: CompletionItemKind.Method,
  },
  map_agg: {
    type: CompletionItemKind.Method,
  },
  multimap_agg: {
    type: CompletionItemKind.Method,
  },
  cardinality: {
    type: CompletionItemKind.Method,
  },
  element_at: {
    type: CompletionItemKind.Method,
  },
  map: {
    type: CompletionItemKind.Method,
  },
  map_from_entries: {
    type: CompletionItemKind.Method,
  },
  map_entries: {
    type: CompletionItemKind.Method,
  },
  map_concat: {
    type: CompletionItemKind.Method,
  },
  map_filter: {
    type: CompletionItemKind.Method,
  },
  transform_keys: {
    type: CompletionItemKind.Method,
  },
  transform_values: {
    type: CompletionItemKind.Method,
  },
  map_keys: {
    type: CompletionItemKind.Method,
  },
  map_values: {
    type: CompletionItemKind.Method,
  },
  map_zip_with: {
    type: CompletionItemKind.Method,
  },
  // 估算函数
  approx_distinct: {
    type: CompletionItemKind.Method,
  },
  approx_percentile: {
    type: CompletionItemKind.Method,
  },
  numeric_histogram: {
    type: CompletionItemKind.Method,
  },
  // 数学统计函数
  corr: {
    type: CompletionItemKind.Function,
  },
  covar_pop: {
    type: CompletionItemKind.Function,
  },
  covar_samp: {
    type: CompletionItemKind.Function,
  },
  regr_intercept: {
    type: CompletionItemKind.Function,
  },
  regr_slope: {
    type: CompletionItemKind.Function,
  },
  stddev: {
    type: CompletionItemKind.Function,
  },
  stddev_samp: {
    type: CompletionItemKind.Function,
  },
  stddev_pop: {
    type: CompletionItemKind.Function,
  },
  variance: {
    type: CompletionItemKind.Function,
  },
  var_samp: {
    type: CompletionItemKind.Function,
  },
  var_pop: {
    type: CompletionItemKind.Function,
  },
  // 数学计算函数
  abs: {
    type: CompletionItemKind.Function,
  },
  cbrt: {
    type: CompletionItemKind.Function,
  },
  ceiling: {
    type: CompletionItemKind.Function,
  },
  cosine_similarity: {
    type: CompletionItemKind.Function,
  },
  degrees: {
    type: CompletionItemKind.Function,
  },
  e: {
    type: CompletionItemKind.Function,
  },
  exp: {
    type: CompletionItemKind.Function,
  },
  floor: {
    type: CompletionItemKind.Function,
  },
  from_base: {
    type: CompletionItemKind.Function,
  },
  ln: {
    type: CompletionItemKind.Function,
  },
  log2: {
    type: CompletionItemKind.Function,
  },
  log10: {
    type: CompletionItemKind.Function,
  },
  log: {
    type: CompletionItemKind.Function,
  },
  pi: {
    type: CompletionItemKind.Function,
  },
  pow: {
    type: CompletionItemKind.Function,
  },
  radians: {
    type: CompletionItemKind.Function,
  },
  rand: {
    type: CompletionItemKind.Function,
  },
  random: {
    type: CompletionItemKind.Function,
  },
  round: {
    type: CompletionItemKind.Function,
  },
  sqrt: {
    type: CompletionItemKind.Function,
  },
  to_base: {
    type: CompletionItemKind.Function,
  },
  truncate: {
    type: CompletionItemKind.Function,
  },
  acos: {
    type: CompletionItemKind.Function,
  },
  asin: {
    type: CompletionItemKind.Function,
  },
  atan: {
    type: CompletionItemKind.Function,
  },
  atan2: {
    type: CompletionItemKind.Function,
  },
  cos: {
    type: CompletionItemKind.Function,
  },
  sin: {
    type: CompletionItemKind.Function,
  },
  cosh: {
    type: CompletionItemKind.Function,
  },
  tan: {
    type: CompletionItemKind.Function,
  },
  tanh: {
    type: CompletionItemKind.Function,
  },
  infinity: {
    type: CompletionItemKind.Function,
  },
  is_infinity: {
    type: CompletionItemKind.Function,
  },
  is_finity: {
    type: CompletionItemKind.Function,
  },
  is_nan: {
    type: CompletionItemKind.Function,
  },
  // 字符串函数
  chr: {
    type: CompletionItemKind.Function,
  },
  length: {
    type: CompletionItemKind.Function,
  },
  levenshtein_distance: {
    type: CompletionItemKind.Function,
  },
  lpad: {
    type: CompletionItemKind.Function,
  },
  rpad: {
    type: CompletionItemKind.Function,
  },
  ltrim: {
    type: CompletionItemKind.Function,
  },
  replace: {
    type: CompletionItemKind.Function,
  },
  reverse: {
    type: CompletionItemKind.Function,
  },
  rtrim: {
    type: CompletionItemKind.Function,
  },
  split: {
    type: CompletionItemKind.Function,
  },
  split_part: {
    type: CompletionItemKind.Function,
  },
  split_to_map: {
    type: CompletionItemKind.Function,
  },
  position: {
    type: CompletionItemKind.Function,
  },
  strpos: {
    type: CompletionItemKind.Function,
  },
  substr: {
    type: CompletionItemKind.Function,
  },
  trim: {
    type: CompletionItemKind.Function,
  },
  upper: {
    type: CompletionItemKind.Function,
  },
  concat: {
    type: CompletionItemKind.Function,
  },
  hamming_distance: {
    type: CompletionItemKind.Function,
  },
  // 日期函数
  current_date: {
    type: CompletionItemKind.Function,
  },
  current_time: {
    type: CompletionItemKind.Function,
  },
  current_timestamp: {
    type: CompletionItemKind.Function,
  },
  current_timezone: {
    type: CompletionItemKind.Function,
  },
  from_iso8601_timestamp: {
    type: CompletionItemKind.Function,
  },
  from_iso8601_date: {
    type: CompletionItemKind.Function,
  },
  from_unixtime: {
    type: CompletionItemKind.Function,
  },
  localtime: {
    type: CompletionItemKind.Function,
  },
  localtimestamp: {
    type: CompletionItemKind.Function,
  },
  now: {
    type: CompletionItemKind.Function,
  },
  to_unixtime: {
    type: CompletionItemKind.Function,
  },
  date_trunc: {
    type: CompletionItemKind.Function,
  },
  date_format: {
    type: CompletionItemKind.Function,
  },
  date_parse: {
    type: CompletionItemKind.Function,
  },
  date_add: {
    type: CompletionItemKind.Function,
  },
  date_diff: {
    type: CompletionItemKind.Function,
  },
  time_series: {
    type: CompletionItemKind.Function,
  },
  // URL函数
  url_extract_fragment: {
    type: CompletionItemKind.Function,
  },
  url_extract_host: {
    type: CompletionItemKind.Function,
  },
  url_extract_parameter: {
    type: CompletionItemKind.Function,
  },
  url_extract_path: {
    type: CompletionItemKind.Function,
  },
  url_extract_port: {
    type: CompletionItemKind.Function,
  },
  url_extract_protocol: {
    type: CompletionItemKind.Function,
  },
  url_extract_query: {
    type: CompletionItemKind.Function,
  },
  url_encode: {
    type: CompletionItemKind.Function,
  },
  url_decode: {
    type: CompletionItemKind.Function,
  },
  // 正则式函数
  regexp_extract_all: {
    type: CompletionItemKind.Function,
  },
  regexp_extract: {
    type: CompletionItemKind.Function,
  },
  regexp_like: {
    type: CompletionItemKind.Function,
  },
  regexp_replace: {
    type: CompletionItemKind.Function,
  },
  regexp_split: {
    type: CompletionItemKind.Function,
  },
  // JSON函数
  json_parse: {
    type: CompletionItemKind.Function,
  },
  json_format: {
    type: CompletionItemKind.Function,
  },
  json_array_contains: {
    type: CompletionItemKind.Function,
  },
  json_array_get: {
    type: CompletionItemKind.Function,
  },
  json_array_length: {
    type: CompletionItemKind.Function,
  },
  json_extract: {
    type: CompletionItemKind.Function,
  },
  json_extract_scalar: {
    type: CompletionItemKind.Function,
  },
  json_size: {
    type: CompletionItemKind.Function,
  },
  // 类型转换函数
  cast: {
    type: CompletionItemKind.Method,
  },
  try_cast: {
    type: CompletionItemKind.Method,
  },
  // IP地理函数
  ip_to_domain: {
    type: CompletionItemKind.Function,
  },
  ip_to_country: {
    type: CompletionItemKind.Function,
  },
  ip_to_province: {
    type: CompletionItemKind.Function,
  },
  ip_to_city: {
    type: CompletionItemKind.Function,
  },
  ip_to_geo: {
    type: CompletionItemKind.Function,
  },
  ip_to_city_geo: {
    type: CompletionItemKind.Function,
  },
  ip_to_provider: {
    type: CompletionItemKind.Function,
  },
  ip_to_country_code: {
    type: CompletionItemKind.Function,
  },
  // 同比和环比函数
  compare: {
    type: CompletionItemKind.Function,
  },
  // 机器学习函数
  ts_smooth_simple: {
    type: CompletionItemKind.Interface,
  },
  ts_smooth_fir: {
    type: CompletionItemKind.Interface,
  },
  ts_smooth_iir: {
    type: CompletionItemKind.Interface,
  },
  ts_period_detect: {
    type: CompletionItemKind.Interface,
  },
  ts_cp_detect: {
    type: CompletionItemKind.Interface,
  },
  ts_breakout_detect: {
    type: CompletionItemKind.Interface,
  },
  ts_predicate_simple: {
    type: CompletionItemKind.Interface,
  },
  ts_predicate_ar: {
    type: CompletionItemKind.Interface,
  },
  ts_predicate_arma: {
    type: CompletionItemKind.Interface,
  },
  ts_predicate_arima: {
    type: CompletionItemKind.Interface,
  },
  ts_regression_predict: {
    type: CompletionItemKind.Interface,
  },
  ts_decompose: {
    type: CompletionItemKind.Interface,
  },
  ts_density_cluster: {
    type: CompletionItemKind.Interface,
  },
  ts_hierarchical_cluster: {
    type: CompletionItemKind.Interface,
  },
  ts_similar_instance: {
    type: CompletionItemKind.Interface,
  },
  pattern_stat: {
    type: CompletionItemKind.Interface,
  },
  pattern_diff: {
    type: CompletionItemKind.Interface,
  },
  ts_find_peaks: {
    type: CompletionItemKind.Interface,
  },
  // PromQL函数
  promql_query: {
    type: CompletionItemKind.Struct,
  },
  promql_query_range: {
    type: CompletionItemKind.Struct,
  },
  promql_labels: {
    type: CompletionItemKind.Struct,
  },
  promql_label_values: {
    type: CompletionItemKind.Struct,
  },
  promql_series: {
    type: CompletionItemKind.Struct,
  },
};
