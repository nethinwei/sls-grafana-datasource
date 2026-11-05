package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

const maxContextLines = 100 // GetContextLogs limit per direction

// logsContext returns the lines before ("backward") or after ("forward") one log with
// GetContextLogs, which reads them from the same pack and source file as the original log. Rows are
// ordered from the closest line outwards, as Grafana's log context expects.
func (ds *datasource) logsContext(ctx context.Context, logstore string, p *contextParams) (data.Frames, error) {
	if p == nil || p.PackID == "" || p.PackMeta == "" {
		return nil, userError("log context needs __tag__:__pack_id__ and __pack_meta__ on the log")
	}
	limit := int32(min(max(p.Limit, 1), maxContextLines))
	back, forward := limit, int32(1) // SLS requires both to be positive
	if p.Direction == "forward" {
		back, forward = 1, limit
	}
	resp, err := ds.contextLogs(ctx, logstore, back, forward, p.PackID, p.PackMeta)
	if err != nil {
		return nil, err
	}

	type row struct {
		idx  int
		log  map[string]string
		keys []string
	}
	var rows []row
	for i, l := range resp.Logs {
		idx, _ := strconv.Atoi(l["__index_number__"])
		if (p.Direction == "forward" && idx > 0) || (p.Direction != "forward" && idx < 0) {
			rows = append(rows, row{idx, l, resp.Keys[i]})
		}
	}
	sort.Slice(rows, func(a, b int) bool {
		if p.Direction == "forward" {
			return rows[a].idx < rows[b].idx
		}
		return rows[a].idx > rows[b].idx
	})
	logs := make([]map[string]string, len(rows))
	keys := make([][]string, len(rows))
	for i, r := range rows {
		logs[i], keys[i] = r.log, r.keys
	}
	frame := buildLogsFrame(logs, keys, &ds.cfg.view)
	keepOrder(frame, p.Direction == "forward")
	ds.describeFilters(ctx, logstore, frame)
	frame.Meta.ExecutedQueryString = fmt.Sprintf("logstore: %s\nGetContextLogs pack_id=%s pack_meta=%s back_lines=%d forward_lines=%d",
		logstore, p.PackID, p.PackMeta, back, forward)
	return data.Frames{frame}, nil
}

// logsVolume counts logs over time by level for the logs volume histogram. It runs SQL on the
// search statement, which needs SQL statistics (doc_value) on the level field; without them it falls
// back to GetHistograms, which only gives the total.
//
// SQL and SPL statements have no volume: SPL filters cannot be combined with this SQL, and SQL
// results are not logs.
func (ds *datasource) logsVolume(ctx context.Context, logstore, query string, q backend.DataQuery) (data.Frames, error) {
	if strings.Contains(query, "|") {
		return data.Frames{}, nil
	}
	sql, err := applyMacros(fmt.Sprintf(`%s | select $__timeGroup(__time__) as time, %s as level, count(*) as count group by time, level order by time limit 100000`,
		query, quoteIdentifier(ds.cfg.view.slsName(ds.cfg.view.levelField))), q.TimeRange, q.Interval)
	if err != nil {
		return nil, err
	}
	resp, err := ds.getLogsComplete(ctx, logstore, &sls.GetLogRequest{
		From: q.TimeRange.From.Unix(), FromNsPart: int32(q.TimeRange.From.Nanosecond()),
		To: q.TimeRange.To.Unix(), ToNsPart: int32(q.TimeRange.To.Nanosecond()),
		Query: sql,
	}, completeRetryDashboard)
	if err == nil {
		frames, err := buildVolumeFrames(resp.Logs, ds.cfg.sqlLocation)
		if err != nil {
			return nil, err
		}
		annotate(frames, logstore, sql, []sls.GetLogsV3ResponseMeta{resp.Meta})
		return frames, nil
	}
	log.DefaultLogger.Debug("logs volume by level failed, falling back to GetHistograms", "error", err)

	hist, histErr := call(ctx, func() (*sls.GetHistogramsResponse, error) {
		return ds.client.GetHistograms(ds.cfg.Project, logstore, "", q.TimeRange.From.Unix(), q.TimeRange.To.Unix(), query)
	})
	if histErr != nil {
		return nil, err // the SQL error is the more useful one
	}
	times := make([]time.Time, len(hist.Histograms))
	counts := make([]float64, len(hist.Histograms))
	for i, h := range hist.Histograms {
		times[i] = time.Unix(h.From, 0)
		counts[i] = float64(h.Count)
	}
	f := data.NewFrame("", data.NewField("time", nil, times), data.NewField("count", data.Labels{"level": "unknown"}, counts))
	f.Meta = &data.FrameMeta{
		Type:                data.FrameTypeTimeSeriesMulti,
		ExecutedQueryString: fmt.Sprintf("logstore: %s\nGetHistograms query: %s", logstore, query),
		Notices: []data.Notice{{Severity: data.NoticeSeverityInfo,
			Text: fmt.Sprintf("Logs volume by level is not available (%s); showing the total. Enabling SQL statistics on field %q usually fixes it.",
				errorText(err), ds.cfg.view.slsName(ds.cfg.view.levelField))}},
	}
	return data.Frames{f}, nil
}

// keepOrder makes the times of context rows strictly monotonic in their order (closest first), so
// that rows written in the same second, often without nanoseconds, keep their order when Grafana
// sorts them by time and are not taken for duplicates.
func keepOrder(frame *data.Frame, forward bool) {
	if len(frame.Fields) == 0 || frame.Fields[0].Type() != data.FieldTypeTime {
		return
	}
	f := frame.Fields[0]
	for i := 1; i < f.Len(); i++ {
		prev, cur := f.At(i-1).(time.Time), f.At(i).(time.Time)
		if forward && !cur.After(prev) {
			f.Set(i, prev.Add(time.Nanosecond))
		} else if !forward && !cur.Before(prev) {
			f.Set(i, prev.Add(-time.Nanosecond))
		}
	}
}

// errorText is the readable message of an error, as shown in panels.
func errorText(err error) string {
	_, _, msg := classifyError(err)
	return msg
}

func annotate(frames data.Frames, logstore, query string, metas []sls.GetLogsV3ResponseMeta) {
	for _, f := range frames {
		if f.Meta == nil {
			f.Meta = &data.FrameMeta{}
		}
		f.Meta.ExecutedQueryString = fmt.Sprintf("logstore: %s\nquery: %s", logstore, query)
		f.Meta.Stats = queryStats(metas)
		f.Meta.Notices = queryNotices(metas)
	}
}

// quoteIdentifier quotes a field name for SLS SQL ("__tag__:__path__", names with dots, ...).
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
