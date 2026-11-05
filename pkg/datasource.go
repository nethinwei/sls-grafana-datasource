package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

const (
	userAgent = "nethinwei-sls-datasource"
	// Per HTTP request to SLS. A Grafana query that pages through logs makes several requests and is
	// bounded by the request context instead.
	requestTimeout = 60 * time.Second
)

// pageParallelism is how many pages of log rows are requested at once.
const pageParallelism = 4

// retryTimeout bounds the retries of one SLS request (network errors and 5xx responses).
var retryTimeout = 90 * time.Second

var (
	_ backend.QueryDataHandler      = (*datasource)(nil)
	_ backend.CheckHealthHandler    = (*datasource)(nil)
	_ backend.CallResourceHandler   = (*datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*datasource)(nil)
)

// datasource is one configured SLS data source. Grafana creates a new instance whenever the
// configuration changes and disposes the old one.
type datasource struct {
	cfg    *settings
	cfgErr error // why cfg is nil
	client sls.ClientInterface
	// project serves APIs that the client interface does not expose (GetContextLogs). Building it
	// makes no request.
	project   *sls.LogProject
	resources backend.CallResourceHandler
	catalogs  catalogCache
	// learned remembers, per logstore, fields that query results produced (name → SPL expression,
	// "" when SLS cannot filter on it), for filters on fields the catalog's sample missed.
	learnedMu sync.Mutex
	learned   map[string]map[string]string
}

func newDatasource(_ context.Context, s backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	cfg, err := loadSettings(s)
	if err != nil {
		// Still create the instance so that health check and queries can report the problem.
		log.DefaultLogger.Warn("invalid data source settings", "error", err)
		ds := &datasource{cfgErr: err}
		ds.resources = newResourceHandler(ds)
		return ds, nil
	}
	credentials := sls.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret, "")
	client := sls.CreateNormalInterfaceV2(cfg.Endpoint, credentials)
	client.SetUserAgent(userAgent)
	client.SetRetryTimeout(retryTimeout)
	if c, ok := client.(*sls.Client); ok {
		c.RequestTimeOut = requestTimeout
	}
	if cfg.Region != "" {
		client.SetAuthVersion(sls.AuthV4)
		client.SetRegion(cfg.Region)
	}
	project, err := sls.NewLogProjectV2(cfg.Project, cfg.Endpoint, credentials)
	if err != nil {
		return nil, err
	}
	project = project.WithRequestTimeout(requestTimeout).WithRetryTimeout(retryTimeout)
	project.UserAgent = userAgent
	// Sign like the client, which also switches to V4 by itself for some regions (acdr).
	if c, ok := client.(*sls.Client); ok {
		project.AuthVersion, project.Region = c.AuthVersion, c.Region
	}

	ds := &datasource{cfg: cfg, client: client, project: project}
	ds.resources = newResourceHandler(ds)
	return ds, nil
}

func (ds *datasource) Dispose() {
	if ds.client != nil {
		_ = ds.client.Close()
	}
}

func (ds *datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	return ds.resources.CallResource(ctx, req, sender)
}

// QueryData runs every query concurrently.
func (ds *datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	resp := backend.NewQueryDataResponse()
	// Alert rules evaluate whatever data they get: they must not get partial results silently.
	fromAlert := req.Headers["FromAlert"] == "true"
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, q := range req.Queries {
		wg.Add(1)
		go func(q backend.DataQuery) {
			defer wg.Done()
			r := ds.runQuery(ctx, q, fromAlert)
			mu.Lock()
			resp.Responses[q.RefID] = r
			mu.Unlock()
		}(q)
	}
	wg.Wait()
	return resp, nil
}

func (ds *datasource) runQuery(ctx context.Context, q backend.DataQuery, fromAlert bool) (res backend.DataResponse) {
	defer func() {
		if r := recover(); r != nil {
			log.DefaultLogger.Error("query panicked", "refId", q.RefID, "panic", r)
			res = errorResponse(backend.PluginError(fmt.Errorf("internal error: %v", r)))
		}
	}()

	if ds.cfg == nil {
		return errorResponse(userError("data source settings are invalid: %v", ds.cfgErr))
	}
	m, err := parseQuery(q)
	if err != nil {
		return errorResponse(err)
	}
	logstore := m.LogStore
	if logstore == "" {
		logstore = ds.cfg.LogStore
	}
	if logstore == "" {
		return errorResponse(userError("no logstore: choose one in the query editor or set a default in the data source"))
	}
	if m.QueryType == queryTypeLogsContext {
		frames, err := ds.logsContext(ctx, logstore, m.Context)
		if err != nil {
			return errorResponse(err)
		}
		return backend.DataResponse{Frames: frames}
	}
	query := strings.TrimSpace(m.Query)
	if query == "" {
		query = "*"
	}
	query, err = applyMacros(query, q.TimeRange, q.Interval)
	if err != nil {
		return errorResponse(err)
	}
	var skipped []string
	if len(m.AdhocFilters) > 0 {
		cat, err := ds.catalog(ctx, logstore)
		if err != nil {
			log.DefaultLogger.Debug("field catalog unavailable, assuming indexed fields for ad hoc filters", "error", err)
		}
		query, skipped = applyAdhocFilters(query, m.AdhocFilters, &ds.cfg.view, cat, ds.learnedFields(logstore))
	}
	if m.QueryType == queryTypeLogsVolume {
		frames, err := ds.logsVolume(ctx, logstore, query, q)
		if err != nil {
			return errorResponse(err)
		}
		return backend.DataResponse{Frames: frames}
	}

	frame, err := ds.execute(ctx, logstore, query, m, q.TimeRange, fromAlert)
	if err != nil {
		return errorResponse(err)
	}
	for _, s := range skipped {
		frame.Meta.Notices = append(frame.Meta.Notices, data.Notice{Severity: data.NoticeSeverityWarning, Text: "Ad hoc filter ignored: " + s})
	}
	return backend.DataResponse{Frames: data.Frames{frame}}
}

// maxLearnedFields bounds the fields remembered per logstore; past it the memory starts over.
const maxLearnedFields = 5000

// learnFields remembers what a logs frame of a logstore says about the fields rules produced (see
// buildLogsFrame), for filters on fields the catalog's sample missed.
func (ds *datasource) learnFields(logstore string, frame *data.Frame) {
	custom, _ := frame.Meta.Custom.(map[string]any)
	names, _ := custom[grafanaOnlyFieldsKey].([]string)
	exprs, _ := custom[fieldExprsKey].(map[string]string)
	if len(names) == 0 && len(exprs) == 0 {
		return
	}
	ds.learnedMu.Lock()
	defer ds.learnedMu.Unlock()
	if ds.learned == nil {
		ds.learned = map[string]map[string]string{}
	}
	m := ds.learned[logstore]
	if m == nil || len(m)+len(names)+len(exprs) > maxLearnedFields {
		m = map[string]string{}
		ds.learned[logstore] = m
	}
	for _, n := range names {
		m[n] = ""
	}
	for n, e := range exprs {
		m[n] = e
	}
}

// learnedFields returns what was learned about the fields of a logstore.
func (ds *datasource) learnedFields(logstore string) learnedFields {
	return func(name string) (string, bool) {
		ds.learnedMu.Lock()
		defer ds.learnedMu.Unlock()
		e, ok := ds.learned[logstore][name]
		return e, ok
	}
}

// execute sends the query with GetLogsV3. Whether it is SQL is decided by SLS (meta.hasSQL), not by
// looking for "|" in the text: SPL also uses pipes but returns log rows.
func (ds *datasource) execute(ctx context.Context, logstore, query string, m *queryModel, tr backend.TimeRange, fromAlert bool) (*data.Frame, error) {
	budget := completeRetryDashboard
	if fromAlert {
		budget = completeRetryAlert
	}
	req := &sls.GetLogRequest{
		From:       tr.From.Unix(),
		FromNsPart: int32(tr.From.Nanosecond()),
		To:         tr.To.Unix(),
		ToNsPart:   int32(tr.To.Nanosecond()),
		Query:      withPackMeta(query),
		Lines:      min(m.MaxLines, linesPerRequest),
		Reverse:    true,
		PowerSQL:   m.PowerSQL,
	}
	first, err := ds.getLogsComplete(ctx, logstore, req, budget)
	if err != nil {
		return nil, err
	}

	var frame *data.Frame
	metas := []sls.GetLogsV3ResponseMeta{first.Meta}
	if first.Meta.HasSQL {
		timeColumn := m.TimeColumn
		if m.Format == formatTable {
			timeColumn = "" // keep the table as it is
		}
		frame, err = buildSQLFrame(first.Meta.Keys, first.Meta.ColumnTypes, first.Logs, timeColumn, ds.cfg.sqlLocation, ds.cfg.Limits.MaxWideCells)
		if err != nil {
			return nil, err
		}
	} else {
		// Page while pages come back full and complete, a few pages at a time.
		logs, keys := first.Logs, first.Keys
		more := !incomplete(first.Meta) && int64(len(first.Logs)) == req.Lines
		for more && int64(len(logs)) < m.MaxLines {
			var batch []*sls.GetLogRequest
			for offset := int64(len(logs)); offset < m.MaxLines && len(batch) < pageParallelism; offset += linesPerRequest {
				r := *req
				r.Offset, r.Lines = offset, min(m.MaxLines-offset, linesPerRequest)
				batch = append(batch, &r)
			}
			pages := make([]*logsResponse, len(batch))
			errs := make([]error, len(batch))
			var wg sync.WaitGroup
			for i, r := range batch {
				wg.Add(1)
				go func() {
					defer wg.Done()
					pages[i], errs[i] = ds.getLogsComplete(ctx, logstore, r, budget)
				}()
			}
			wg.Wait()
			for i, page := range pages {
				if errs[i] != nil {
					return nil, errs[i]
				}
				metas = append(metas, page.Meta)
				logs = append(logs, page.Logs...)
				keys = append(keys, page.Keys...)
				if incomplete(page.Meta) || int64(len(page.Logs)) < batch[i].Lines {
					more = false
					break
				}
			}
		}
		frame = buildLogsFrame(logs, keys, &ds.cfg.view)
		ds.learnFields(logstore, frame)
	}

	if fromAlert {
		for _, meta := range metas {
			if incomplete(meta) {
				return nil, backend.DownstreamErrorf("SLS returned incomplete results after retrying for %s (progress: %s); "+
					"an alert must not evaluate partial data. Narrow the time range, or enable Dedicated SQL (powerSql)", budget, meta.Progress)
			}
		}
	}
	if frame.Meta == nil {
		frame.Meta = &data.FrameMeta{}
	}
	frame.Meta.ExecutedQueryString = fmt.Sprintf("logstore: %s\nfrom: %s\nto: %s\nquery: %s",
		logstore, tr.From.UTC().Format(time.RFC3339Nano), tr.To.UTC().Format(time.RFC3339Nano), req.Query)
	frame.Meta.Stats = queryStats(metas)
	frame.Meta.Notices = queryNotices(metas)
	return frame, nil
}

// How long a query keeps asking SLS until its results are complete (SLS returns progress
// "Incomplete" while it is still reading data, typically for heavy queries or the latest seconds).
// Alert rules need complete counts; dashboards prefer a prompt answer with a notice.
var (
	completeRetryDashboard = 5 * time.Second
	completeRetryAlert     = 30 * time.Second
	completeFirstWait      = 100 * time.Millisecond
	completeMaxWait        = 2 * time.Second
)

func incomplete(m sls.GetLogsV3ResponseMeta) bool {
	return m.Progress != "" && m.Progress != "Complete"
}

// getLogsComplete repeats a GetLogs request while SLS reports incomplete results, as the SDK's
// GetLogsToCompleted does, within budget. It returns the last response, complete or not.
func (ds *datasource) getLogsComplete(ctx context.Context, logstore string, req *sls.GetLogRequest, budget time.Duration) (*logsResponse, error) {
	deadline := time.Now().Add(budget)
	wait := completeFirstWait
	for {
		resp, err := ds.getLogs(ctx, logstore, req)
		if err != nil || !incomplete(resp.Meta) || time.Now().Add(wait).After(deadline) {
			return resp, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, completeMaxWait)
	}
}

func queryStats(metas []sls.GetLogsV3ResponseMeta) []data.QueryStat {
	var rows, bytes, elapsed, count float64
	for _, m := range metas {
		rows += float64(m.ProcessedRows)
		bytes += float64(m.ProcessedBytes)
		elapsed += float64(m.ElapsedMillisecond)
		count += float64(m.Count)
	}
	stat := func(name, unit string, v float64) data.QueryStat {
		return data.QueryStat{FieldConfig: data.FieldConfig{DisplayName: name, Unit: unit}, Value: v}
	}
	return []data.QueryStat{
		stat("SLS processed rows", "short", rows),
		stat("SLS processed bytes", "bytes", bytes),
		stat("SLS elapsed", "ms", elapsed),
		stat("SLS returned rows", "short", count),
		stat("SLS requests", "short", float64(len(metas))),
	}
}

func queryNotices(metas []sls.GetLogsV3ResponseMeta) []data.Notice {
	var notices []data.Notice
	for _, m := range metas {
		if m.Progress != "" && m.Progress != "Complete" {
			notices = append(notices, data.Notice{
				Severity: data.NoticeSeverityWarning,
				Text:     "SLS returned incomplete results (progress: " + m.Progress + "). Narrow the time range, or enable Dedicated SQL (powerSql) for heavy queries.",
				Inspect:  data.InspectTypeMeta,
			})
			break
		}
	}
	// SLS reports limited=100 for every SQL without LIMIT; only worth a notice when it actually cut rows.
	if m := metas[0]; m.HasSQL && m.Limited > 0 && m.Count >= m.Limited {
		notices = append(notices, data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     fmt.Sprintf("SQL without LIMIT returns at most %d rows; add LIMIT to get more.", m.Limited),
		})
	}
	return notices
}
