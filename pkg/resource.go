package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"
)

// Resource routes used by the query editor. Response shapes are kept from plugin 2.x so the current
// frontend keeps working.
func newResourceHandler(ds *datasource) backend.CallResourceHandler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/getLogstoreList", ds.handleLogstoreList)
	mux.HandleFunc("/api/gotoSLS", ds.handleGotoSLS)
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
