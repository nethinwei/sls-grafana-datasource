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
	userAgent = "grafana-sls-datasource"
	// Per HTTP request to SLS. A Grafana query that pages through logs makes several requests and is
	// bounded by the request context instead.
	requestTimeout = 30 * time.Second
	retryTimeout   = 45 * time.Second
)

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
	client sls.ClientInterface
	// project serves APIs that the client interface does not expose (GetContextLogs). Building it
	// makes no request.
	project   *sls.LogProject
	resources backend.CallResourceHandler
	catalogs  catalogCache
	// learned remembers fields that query results produced (name → SPL expression, "" when SLS
	// cannot filter on it), for filters on fields the catalog's sample missed.
	learned sync.Map
}

func newDatasource(_ context.Context, s backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	cfg, err := loadSettings(s)
	if err != nil {
		// Still create the instance so that health check and queries can report the problem.
		log.DefaultLogger.Warn("invalid data source settings", "error", err)
		ds := &datasource{}
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
	if cfg.Region != "" {
		project.AuthVersion = sls.AuthV4
		project.Region = cfg.Region
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
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, q := range req.Queries {
		wg.Add(1)
		go func(q backend.DataQuery) {
			defer wg.Done()
			r := ds.runQuery(ctx, q)
			mu.Lock()
			resp.Responses[q.RefID] = r
			mu.Unlock()
		}(q)
	}
	wg.Wait()
	return resp, nil
}

func (ds *datasource) runQuery(ctx context.Context, q backend.DataQuery) (res backend.DataResponse) {
	defer func() {
		if r := recover(); r != nil {
			log.DefaultLogger.Error("query panicked", "refId", q.RefID, "panic", r)
			res = errorResponse(backend.PluginError(fmt.Errorf("internal error: %v", r)))
		}
	}()

	if ds.cfg == nil {
		return errorResponse(userError("data source is not configured: check endpoint, project and AccessKey"))
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
		query, skipped = applyAdhocFilters(query, m.AdhocFilters, &ds.cfg.view, cat, ds.learnedField)
	}
	if m.QueryType == queryTypeLogsVolume {
		frames, err := ds.logsVolume(ctx, logstore, query, q)
		if err != nil {
			return errorResponse(err)
		}
		return backend.DataResponse{Frames: frames}
	}

	frame, err := ds.execute(ctx, logstore, query, m, q.TimeRange)
	if err != nil {
		return errorResponse(err)
	}
	for _, s := range skipped {
		frame.Meta.Notices = append(frame.Meta.Notices, data.Notice{Severity: data.NoticeSeverityWarning, Text: "Ad hoc filter ignored: " + s})
	}
	return backend.DataResponse{Frames: data.Frames{frame}}
}

// learnFields remembers what a logs frame says about the fields rules produced (see
// buildLogsFrame), for filters on fields the catalog's sample missed.
func (ds *datasource) learnFields(frame *data.Frame) {
	custom, _ := frame.Meta.Custom.(map[string]any)
	if names, ok := custom[grafanaOnlyFieldsKey].([]string); ok {
		for _, n := range names {
			ds.learned.Store(n, "")
		}
	}
	if exprs, ok := custom[fieldExprsKey].(map[string]string); ok {
		for n, e := range exprs {
			ds.learned.Store(n, e)
		}
	}
}

func (ds *datasource) learnedField(name string) (string, bool) {
	v, ok := ds.learned.Load(name)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// execute sends the query with GetLogsV3. Whether it is SQL is decided by SLS (meta.hasSQL), not by
// looking for "|" in the text: SPL also uses pipes but returns log rows.
func (ds *datasource) execute(ctx context.Context, logstore, query string, m *queryModel, tr backend.TimeRange) (*data.Frame, error) {
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
	first, err := ds.getLogs(ctx, logstore, req)
	if err != nil {
		return nil, err
	}

	var frame *data.Frame
	metas := []sls.GetLogsV3ResponseMeta{first.Meta}
	if first.Meta.HasSQL {
		frame, err = buildSQLFrame(first.Meta.Keys, first.Meta.ColumnTypes, first.Logs, m.TimeColumn, ds.cfg.sqlLocation)
		if err != nil {
			return nil, err
		}
	} else {
		// Page while the previous page was full and complete.
		logs, keys, last := first.Logs, first.Keys, first
		for int64(len(logs)) < m.MaxLines && last.Meta.Progress == "Complete" && int64(len(last.Logs)) == req.Lines {
			req.Offset += req.Lines
			req.Lines = min(m.MaxLines-int64(len(logs)), linesPerRequest)
			if last, err = ds.getLogs(ctx, logstore, req); err != nil {
				return nil, err
			}
			metas = append(metas, last.Meta)
			logs = append(logs, last.Logs...)
			keys = append(keys, last.Keys...)
		}
		frame = buildLogsFrame(logs, keys, &ds.cfg.view)
		ds.learnFields(frame)
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
