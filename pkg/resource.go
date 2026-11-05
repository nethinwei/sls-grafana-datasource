package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"
)

// Resource routes used by the query editor. Response shapes are kept from plugin 2.x so the current
// frontend keeps working.
func newResourceHandler(ds *datasource) backend.CallResourceHandler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/getLogstoreList", ds.handleLogstoreList)
	mux.HandleFunc("/api/gotoSLS", ds.handleGotoSLS)
	mux.HandleFunc("/api/catalog", ds.handleCatalog)
	mux.HandleFunc("/api/fieldValues", ds.handleFieldValues)
	return httpadapter.New(mux)
}

// handleLogstoreList lists logstores of the configured project. Least-privilege AccessKeys usually
// cannot list logstores (log:ListLogStores is project level); then the default logstore is offered.
func (ds *datasource) handleLogstoreList(w http.ResponseWriter, r *http.Request) {
	if ds.cfg == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"data": []string{}, "message": "data source is not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	list, err := call(ctx, func() ([]string, error) { return ds.client.ListLogStoreV2(ds.cfg.Project, 0, 500, "None") })
	if err != nil {
		var fallback []string
		if ds.cfg.LogStore != "" {
			fallback = []string{ds.cfg.LogStore}
		}
		_, _, msg := classifyError(err)
		writeJSON(w, http.StatusOK, map[string]any{"data": fallback, "res": fallback, "message": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": list, "res": list, "message": ""})
}

type gotoSLSRequest struct {
	Encoding string `json:"encoding"` // query string built by the frontend (queryString, time range, ...)
	Logstore string `json:"logstore"`
}

// handleGotoSLS returns the SLS console URL for the current query. The user signs in to the console
// with their own Alibaba Cloud account.
func (ds *datasource) handleGotoSLS(w http.ResponseWriter, r *http.Request) {
	if ds.cfg == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "data source is not configured"})
		return
	}
	var req gotoSLSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": err.Error()})
		return
	}
	logstore := req.Logstore
	if logstore == "" {
		logstore = ds.cfg.LogStore
	}
	u := "https://sls.console." + ds.cfg.consoleDomain() + ".com/lognext/project/" +
		url.PathEscape(ds.cfg.Project) + "/logsearch/" + url.PathEscape(logstore) + "?" + req.Encoding
	writeJSON(w, http.StatusOK, map[string]any{"url": u, "message": "", "err": ""})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type catalogRequest struct {
	Logstore string `json:"logstore"`
	Refresh  bool   `json:"refresh"` // skip the cache, e.g. after the index changed
}

// handleCatalog returns the field catalog of a logstore (catalog.go).
func (ds *datasource) handleCatalog(w http.ResponseWriter, r *http.Request) {
	var req catalogRequest
	if !ds.decode(w, r, &req) {
		return
	}
	logstore := ds.logstoreOr(req.Logstore)
	if req.Refresh {
		ds.catalogs.forget(logstore)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := ds.catalog(ctx, logstore)
	if err != nil {
		_, _, msg := classifyError(err)
		writeJSON(w, http.StatusOK, map[string]any{"fields": []catalogField{}, "message": msg})
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type fieldValuesRequest struct {
	Logstore string `json:"logstore"`
	Key      string `json:"key"`   // field name shown in Grafana
	Query    string `json:"query"` // search statement restricting the logs
	// Filters are the other ad hoc filters, so suggestions match what the dashboard shows.
	Filters []adhocFilter `json:"filters"`
	From    int64         `json:"from"` // unix ms
	To      int64         `json:"to"`
	Limit   int           `json:"limit"`
}

// handleFieldValues returns the most frequent values of one field in the time range. It groups with
// SQL, which needs SQL statistics on the field; otherwise it collects values from a sample of logs.
func (ds *datasource) handleFieldValues(w http.ResponseWriter, r *http.Request) {
	var req fieldValuesRequest
	if !ds.decode(w, r, &req) {
		return
	}
	if req.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "key is required"})
		return
	}
	logstore := ds.logstoreOr(req.Logstore)
	search := strings.TrimSpace(req.Query)
	if search == "" {
		search = "*"
	}
	limit := min(max(req.Limit, 1), 1000)
	if req.Limit == 0 {
		limit = 200
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	view := &ds.cfg.view
	cat, _ := ds.catalog(ctx, logstore) // nil: fields assumed indexed

	// Fields produced by rules only exist in Grafana: their values come from a sample run through
	// the rules. Other fields are grouped by SQL under their SLS name.
	err := errDerivedField
	if !view.derived[req.Key] {
		name := req.Key
		if sls, ok := view.slsNames[name]; ok {
			name = sls
		}
		sql := fmt.Sprintf("%s | select %s as value, count(*) as count group by value order by count desc limit %d", search, quoteIdentifier(name), limit)
		sql, _ = applyAdhocFilters(sql, req.Filters, view, cat)
		var resp *logsResponse
		resp, err = ds.getLogs(ctx, logstore, &sls.GetLogRequest{From: req.From / 1000, To: max(req.To/1000, req.From/1000+1), Query: sql})
		if err == nil {
			values := make([]string, 0, len(resp.Logs))
			for _, row := range resp.Logs {
				if v := row["value"]; !isNull(v) {
					values = append(values, v)
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"values": values, "source": "sql"})
			return
		}
	}
	search, _ = applyAdhocFilters(search, req.Filters, view, cat)
	logs, sampleErr := ds.sample(ctx, logstore, search, req.From, req.To)
	if sampleErr != nil {
		_, _, msg := classifyError(err)
		writeJSON(w, http.StatusOK, map[string]any{"values": []string{}, "message": msg})
		return
	}
	counts := map[string]int{}
	if t, _ := view.table(logs); t.cols[req.Key] != nil {
		c := t.cols[req.Key]
		for i, ok := range c.present {
			if ok && c.values[i] != "" {
				counts[c.values[i]]++
			}
		}
	}
	values := make([]string, 0, len(counts))
	for v := range counts {
		values = append(values, v)
	}
	sort.Slice(values, func(a, b int) bool { return counts[values[a]] > counts[values[b]] })
	writeJSON(w, http.StatusOK, map[string]any{"values": values[:min(len(values), limit)], "source": "sample"})
}

// sample returns up to 100 recent logs; the range defaults to the last 15 minutes.
func (ds *datasource) sample(ctx context.Context, logstore, query string, fromMs, toMs int64) ([]map[string]string, error) {
	to := time.Now().Unix()
	from := to - 900
	if fromMs > 0 && toMs > fromMs {
		from, to = fromMs/1000, max(toMs/1000, fromMs/1000+1)
	}
	resp, err := ds.getLogs(ctx, logstore, &sls.GetLogRequest{From: from, To: to, Query: query, Lines: linesPerRequest, Reverse: true})
	if err != nil {
		return nil, err
	}
	return resp.Logs, nil
}

func (ds *datasource) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if ds.cfg == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "data source is not configured"})
		return false
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(v); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": err.Error()})
			return false
		}
	}
	return true
}

func (ds *datasource) logstoreOr(logstore string) string {
	if logstore != "" {
		return logstore
	}
	return ds.cfg.LogStore
}
