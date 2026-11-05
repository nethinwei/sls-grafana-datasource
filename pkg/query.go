package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	defaultMaxLines = 1000
	maxMaxLines     = 5000 // logs are fetched 100 per request; keep a panel refresh bounded
	linesPerRequest = 100  // SLS GetLogs limit per request
)

// queryModel is what the frontend sends for one query.
type queryModel struct {
	Query      string `json:"query"`
	LogStore   string `json:"logstore"`
	MaxLines   int64  `json:"maxLines"`
	TimeColumn string `json:"timeColumn"`
	PowerSQL   bool   `json:"powerSql"`
	// Format of SQL results: "time_series" (default) turns a result with the time column into time
	// series, one per combination of string columns; "table" returns the typed table as it is.
	Format string `json:"format"`

	// QueryType selects a special query sent by the frontend; empty for normal queries.
	QueryType string         `json:"queryType"`
	Context   *contextParams `json:"context"`
	// AdhocFilters are the dashboard's ad hoc filters, translated with the field catalog (filters.go).
	AdhocFilters []adhocFilter `json:"adhocFilters"`
}

const (
	formatTimeSeries = "time_series"
	formatTable      = "table"
)

const (
	queryTypeLogsVolume  = "logsVolume"  // log counts over time by level, for the logs volume histogram
	queryTypeLogsContext = "logsContext" // lines around one log, for log context
)

// contextParams identifies a log by its pack (returned thanks to `| with_pack_meta`) and the lines
// wanted before ("backward") or after ("forward") it.
type contextParams struct {
	PackID    string `json:"packId"`
	PackMeta  string `json:"packMeta"`
	Direction string `json:"direction"`
	Limit     int    `json:"limit"`
}

var (
	sqlStart     = regexp.MustCompile(`(?i)^\|\s*(select\b|set\s+session\b)`)
	packMetaPipe = regexp.MustCompile(`(?i)\|\s*with_pack_meta\s*$`)
)

// isSQL reports whether the statement has an SQL part: its first pipe outside quotes starts with
// select (or set session ...; select). Search and SPL (`| where`, `| extend`, ...) return log rows.
func isSQL(query string) bool {
	_, rest := splitStatement(query)
	return sqlStart.MatchString(rest)
}

// withPackMeta asks SLS to return __pack_meta__ with every log row, which GetContextLogs needs to
// locate a log. It costs nothing measurable and only applies to search and SPL.
func withPackMeta(query string) string {
	if isSQL(query) || packMetaPipe.MatchString(query) {
		return query
	}
	return query + " | with_pack_meta"
}

func parseQuery(q backend.DataQuery) (*queryModel, error) {
	m := &queryModel{}
	if err := json.Unmarshal(q.JSON, m); err != nil {
		return nil, userError("invalid query: %v", err)
	}
	if m.MaxLines <= 0 {
		m.MaxLines = defaultMaxLines
	}
	m.MaxLines = min(m.MaxLines, maxMaxLines)
	switch m.Format {
	case "":
		m.Format = formatTimeSeries
	case formatTimeSeries, formatTable:
	default:
		return nil, userError("invalid format %q: use time_series or table", m.Format)
	}
	if m.TimeColumn == "" {
		m.TimeColumn = "time"
	}
	return m, nil
}

var (
	intervalMacro = regexp.MustCompile(`\$__interval_s\b`)
	timeFromMacro = regexp.MustCompile(`\$__timeFrom\(\s*\)|\$__timeFrom\b`)
	timeToMacro   = regexp.MustCompile(`\$__timeTo\(\s*\)|\$__timeTo\b`)
)

const timeGroupName = "$__timeGroup("

// expandTimeGroup expands $__timeGroup(col[, interval]) to ((col) - (col) % step). Arguments are
// split at top-level commas, so columns can be expressions with calls and strings, e.g.
// $__timeGroup(to_unixtime(time), 1m) or $__timeGroup(__time__ + 28800, 24h).
func expandTimeGroup(sql string, defaultStep int64) (string, error) {
	var out strings.Builder
	for {
		i := strings.Index(sql, timeGroupName)
		if i < 0 {
			out.WriteString(sql)
			return out.String(), nil
		}
		out.WriteString(sql[:i])
		args, end, ok := macroArgs(sql[i+len(timeGroupName):])
		if !ok || len(args) == 0 || len(args) > 2 || strings.TrimSpace(args[0]) == "" {
			return "", userError("$__timeGroup: use $__timeGroup(column) or $__timeGroup(column, 5m)")
		}
		col, step := strings.TrimSpace(args[0]), defaultStep
		if len(args) == 2 {
			d, err := time.ParseDuration(strings.TrimSpace(args[1]))
			if err != nil || d < time.Second {
				return "", userError("$__timeGroup: invalid interval %q, use a Go duration such as 30s or 5m", strings.TrimSpace(args[1]))
			}
			step = int64(d / time.Second)
		}
		fmt.Fprintf(&out, "((%s) - (%s) %% %d)", col, col, step)
		sql = sql[i+len(timeGroupName)+end:]
	}
}

// macroArgs reads the arguments of a macro call up to its closing parenthesis, keeping nested
// parentheses and quoted strings intact. end is the offset just after the closing parenthesis.
func macroArgs(s string) (args []string, end int, ok bool) {
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 {
				return append(args, s[start:i]), i + 1, true
			}
			depth--
		case c == ',' && depth == 0:
			args = append(args, s[start:i])
			start = i + 1
		}
	}
	return nil, 0, false
}

// applyMacros expands the plugin's SQL macros. Grafana has already replaced template variables
// ($__interval, $var, ...) in the frontend; these macros need the time range and the interval the
// backend actually received, the same way as Grafana's built-in SQL data sources.
//
//	$__timeGroup(col)        → ((col) - (col) % <interval seconds>)
//	$__timeGroup(col, 5m)    → ((col) - (col) % 300)
//	$__interval_s            → interval in seconds (at least 1)
//	$__timeFrom / $__timeTo  → range start / end as unix seconds
func applyMacros(sql string, tr backend.TimeRange, interval time.Duration) (string, error) {
	sec := max(int64(interval/time.Second), 1)

	sql, err := expandTimeGroup(sql, sec)
	if err != nil {
		return "", err
	}
	sql = intervalMacro.ReplaceAllString(sql, strconv.FormatInt(sec, 10))
	sql = timeFromMacro.ReplaceAllString(sql, strconv.FormatInt(tr.From.Unix(), 10))
	sql = timeToMacro.ReplaceAllString(sql, strconv.FormatInt(tr.To.Unix(), 10))
	return strings.TrimSpace(sql), nil
}
