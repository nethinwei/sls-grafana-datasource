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

// queryModel is what the frontend sends for one query. Legacy fields from plugin 2.x are still read
// so that existing dashboards keep working:
//   - logsPerPage / totalLogs → maxLines
//   - xcol, when it names a column → timeColumn (chart modes such as "pie" or "bar" are ignored)
//
// ycol and the "#:#" grouping syntax are gone: SQL results are returned as a typed table and
// long-to-wide conversion splits series by the string columns automatically.
type queryModel struct {
	Query      string `json:"query"`
	LogStore   string `json:"logstore"`
	MaxLines   int64  `json:"maxLines"`
	TimeColumn string `json:"timeColumn"`
	PowerSQL   bool   `json:"powerSql"`

	// QueryType selects a special query sent by the frontend; empty for normal queries. Plugin 2.x
	// stored PromQL types ("range", "instant") here, which are treated as normal queries.
	QueryType string         `json:"queryType"`
	Context   *contextParams `json:"context"`

	LegacyXcol        string `json:"xcol"`
	LegacyLogsPerPage int64  `json:"logsPerPage"`
	LegacyTotalLogs   int64  `json:"totalLogs"`
}

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
	sqlPipe      = regexp.MustCompile(`(?i)\|\s*select\b`)
	packMetaPipe = regexp.MustCompile(`(?i)\|\s*with_pack_meta\s*$`)
)

// isSQL reports whether the statement has an SQL part. Search and SPL (`| where`, `| extend`, ...)
// return log rows; SQL always starts with `| select`.
func isSQL(query string) bool { return sqlPipe.MatchString(query) }

// withPackMeta asks SLS to return __pack_meta__ with every log row, which GetContextLogs needs to
// locate a log. It costs nothing measurable and only applies to search and SPL.
func withPackMeta(query string) string {
	if isSQL(query) || packMetaPipe.MatchString(query) {
		return query
	}
	return query + " | with_pack_meta"
}

var legacyChartModes = map[string]bool{"": true, "table": true, "bar": true, "pie": true, "map": true, "stat": true, "trace": true}

func parseQuery(q backend.DataQuery) (*queryModel, error) {
	m := &queryModel{}
	if err := json.Unmarshal(q.JSON, m); err != nil {
		return nil, userError("invalid query: %v", err)
	}
	if m.MaxLines <= 0 {
		m.MaxLines = max(m.LegacyTotalLogs, m.LegacyLogsPerPage)
	}
	if m.MaxLines <= 0 {
		m.MaxLines = defaultMaxLines
	}
	m.MaxLines = min(m.MaxLines, maxMaxLines)
	if m.TimeColumn == "" && !legacyChartModes[m.LegacyXcol] {
		m.TimeColumn = m.LegacyXcol
	}
	if m.TimeColumn == "" {
		m.TimeColumn = "time"
	}
	return m, nil
}

var (
	timeGroupMacro = regexp.MustCompile(`\$__timeGroup\(\s*([^,)]+?)\s*(?:,\s*([^)]+?)\s*)?\)`)
	intervalMacro  = regexp.MustCompile(`\$__interval_s\b`)
	timeFromMacro  = regexp.MustCompile(`\$__timeFrom\(\s*\)|\$__timeFrom\b`)
	timeToMacro    = regexp.MustCompile(`\$__timeTo\(\s*\)|\$__timeTo\b`)
)

// applyMacros expands the plugin's SQL macros. Grafana has already replaced template variables
// ($__interval, $var, ...) in the frontend; these macros need the time range and the interval the
// backend actually received, the same way as Grafana's built-in SQL data sources.
//
//	$__timeGroup(col)        → (col - col % <interval seconds>)
//	$__timeGroup(col, 5m)    → (col - col % 300)
//	$__interval_s            → interval in seconds (at least 1)
//	$__timeFrom / $__timeTo  → range start / end as unix seconds
func applyMacros(sql string, tr backend.TimeRange, interval time.Duration) (string, error) {
	sec := max(int64(interval/time.Second), 1)

	var macroErr error
	sql = timeGroupMacro.ReplaceAllStringFunc(sql, func(m string) string {
		parts := timeGroupMacro.FindStringSubmatch(m)
		col, step := parts[1], sec
		if parts[2] != "" {
			d, err := time.ParseDuration(parts[2])
			if err != nil || d < time.Second {
				macroErr = userError("$__timeGroup: invalid interval %q, use a Go duration such as 30s or 5m", parts[2])
				return m
			}
			step = int64(d / time.Second)
		}
		return fmt.Sprintf("(%s - %s %% %d)", col, col, step)
	})
	if macroErr != nil {
		return "", macroErr
	}
	sql = intervalMacro.ReplaceAllString(sql, strconv.FormatInt(sec, 10))
	sql = timeFromMacro.ReplaceAllString(sql, strconv.FormatInt(tr.From.Unix(), 10))
	sql = timeToMacro.ReplaceAllString(sql, strconv.FormatInt(tr.To.Unix(), 10))
	return strings.TrimSpace(sql), nil
}
