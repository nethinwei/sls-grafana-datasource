package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// fakeSLS serves GetLogs from handle; the project has no name so requests go to the server itself.
func fakeSLS(t *testing.T, handle func(n int32, w http.ResponseWriter)) (*datasource, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle(n.Add(1), w)
	}))
	t.Cleanup(srv.Close)
	project, err := sls.NewLogProjectV2("", srv.URL, sls.NewStaticCredentialsProvider("id", "secret", ""))
	if err != nil {
		t.Fatal(err)
	}
	project = project.WithRequestTimeout(5 * time.Second)
	view := compileLogsView(logsConfig{})
	return &datasource{cfg: &settings{Project: "p", LogStore: "ls", view: view, sqlLocation: time.UTC}, project: project}, &n
}

func writeLogs(w http.ResponseWriter, progress string, rows ...map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"meta": map[string]any{"progress": progress, "count": len(rows)}, "data": rows})
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"errorCode": code, "errorMessage": code})
}

func fastRetries(t *testing.T) {
	old := []time.Duration{completeRetryDashboard, completeRetryAlert, completeFirstWait, completeMaxWait}
	completeRetryDashboard, completeRetryAlert, completeFirstWait, completeMaxWait = 300*time.Millisecond, 300*time.Millisecond, 10*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() {
		completeRetryDashboard, completeRetryAlert, completeFirstWait, completeMaxWait = old[0], old[1], old[2], old[3]
	})
}

func TestRawRequestRetriesServerErrors(t *testing.T) {
	ds, n := fakeSLS(t, func(n int32, w http.ResponseWriter) {
		if n == 1 {
			writeError(w, http.StatusServiceUnavailable, "ServerBusy")
			return
		}
		writeLogs(w, "Complete", map[string]string{"msg": "ok"})
	})
	resp, err := ds.getLogs(context.Background(), "ls", &sls.GetLogRequest{Query: "*"})
	if err != nil || len(resp.Logs) != 1 || n.Load() != 2 {
		t.Fatalf("resp=%v err=%v requests=%d", resp, err, n.Load())
	}
	// 4xx is the query's fault: no retry.
	ds, n = fakeSLS(t, func(_ int32, w http.ResponseWriter) { writeError(w, http.StatusBadRequest, "ParameterInvalid") })
	_, err = ds.getLogs(context.Background(), "ls", &sls.GetLogRequest{Query: "*"})
	if err == nil || n.Load() != 1 {
		t.Fatalf("err=%v requests=%d", err, n.Load())
	}
	if status, src, _ := classifyError(err); src != backend.ErrorSourceDownstream || status != http.StatusBadRequest {
		t.Errorf("status %d source %s", status, src)
	}
}

func TestNonJSONErrorIsDownstream(t *testing.T) {
	ds, _ := fakeSLS(t, func(_ int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	})
	old := retryTimeout
	t.Cleanup(func() { retryTimeout = old })
	retryTimeout = 300 * time.Millisecond
	_, err := ds.getLogs(context.Background(), "ls", &sls.GetLogRequest{Query: "*"})
	status, src, msg := classifyError(err)
	if status != http.StatusBadGateway || src != backend.ErrorSourceDownstream || !strings.Contains(msg, "502") {
		t.Errorf("%d %s %s", status, src, msg)
	}
}

func TestInvalidRawSizeDoesNotPanic(t *testing.T) {
	ds, _ := fakeSLS(t, func(_ int32, w http.ResponseWriter) {
		w.Header().Set("x-log-compresstype", "lz4")
		w.Header().Set("x-log-bodyrawsize", "-1")
		_, _ = w.Write([]byte("x"))
	})
	if _, err := ds.getLogs(context.Background(), "ls", &sls.GetLogRequest{Query: "*"}); err == nil {
		t.Fatal("want an error")
	}
}

func TestCallRecoversPanics(t *testing.T) {
	_, err := call(context.Background(), func() (int, error) { panic("boom") })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestIncompleteResultsAreRetried(t *testing.T) {
	fastRetries(t)
	ds, n := fakeSLS(t, func(n int32, w http.ResponseWriter) {
		progress := "InComplete"
		if n >= 3 {
			progress = "Complete"
		}
		writeLogs(w, progress, map[string]string{"__time__": "1700000000", "msg": "ok"})
	})
	q := backend.DataQuery{RefID: "A", JSON: []byte(`{"query":"*"}`), TimeRange: backend.TimeRange{From: time.Unix(1700000000, 0), To: time.Unix(1700000060, 0)}}
	res := ds.runQuery(context.Background(), q, false)
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	if n.Load() != 3 || len(res.Frames[0].Meta.Notices) != 0 {
		t.Fatalf("err=%v requests=%d notices=%v", res.Error, n.Load(), res.Frames[0].Meta.Notices)
	}
}

func TestAlertRejectsIncompleteResults(t *testing.T) {
	fastRetries(t)
	ds, _ := fakeSLS(t, func(_ int32, w http.ResponseWriter) {
		writeLogs(w, "InComplete", map[string]string{"c": "5"})
	})
	q := backend.DataQuery{RefID: "A", JSON: []byte(`{"query":"* | select count(*) as c"}`), TimeRange: backend.TimeRange{From: time.Unix(1700000000, 0), To: time.Unix(1700003600, 0)}}
	if res := ds.runQuery(context.Background(), q, true); res.Error == nil || res.ErrorSource != backend.ErrorSourceDownstream {
		t.Fatalf("alert: err=%v source=%s", res.Error, res.ErrorSource)
	}
	// Dashboards still get the data, with a notice.
	res := ds.runQuery(context.Background(), q, false)
	if res.Error != nil || len(res.Frames) == 0 || len(res.Frames[0].Meta.Notices) == 0 {
		t.Fatalf("dashboard: err=%v frames=%v", res.Error, res.Frames)
	}
}

func TestTableFormatKeepsTheTable(t *testing.T) {
	ds, _ := fakeSLS(t, func(_ int32, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"meta": map[string]any{"progress": "Complete", "hasSQL": true, "keys": []string{"time", "host", "path"}, "columnTypes": []string{"long", "varchar", "varchar"}},
			"data": []map[string]string{{"time": "1700000000", "host": "a", "path": "/x"}, {"time": "1700000000", "host": "a", "path": "/y"}},
		})
	})
	q := backend.DataQuery{RefID: "A", JSON: []byte(`{"query":"* | select __time__ as time, host, path","format":"table"}`),
		TimeRange: backend.TimeRange{From: time.Unix(1700000000, 0), To: time.Unix(1700000060, 0)}}
	res := ds.runQuery(context.Background(), q, false)
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	if n, _ := res.Frames[0].RowLen(); n != 2 || res.Frames[0].Meta.Type != "" {
		t.Errorf("rows=%d type=%q, want the 2-row table", n, res.Frames[0].Meta.Type)
	}
}

func TestLogsArePagedInParallel(t *testing.T) {
	const total = 250
	ds, n := fakeSLS(t, func(_ int32, w http.ResponseWriter) {})
	// Serve pages by offset.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		var req struct {
			Offset int64 `json:"offset"`
			Line   int64 `json:"line"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var rows []map[string]string
		for i := req.Offset; i < min(req.Offset+req.Line, total); i++ {
			rows = append(rows, map[string]string{"__time__": "1700000000", "i": fmt.Sprint(i)})
		}
		writeLogs(w, "Complete", rows...)
	}))
	t.Cleanup(srv.Close)
	project, _ := sls.NewLogProjectV2("", srv.URL, sls.NewStaticCredentialsProvider("id", "secret", ""))
	ds.project = project.WithRequestTimeout(5 * time.Second)
	q := backend.DataQuery{RefID: "A", JSON: []byte(`{"query":"*","maxLines":1000}`), TimeRange: backend.TimeRange{From: time.Unix(1700000000, 0), To: time.Unix(1700000060, 0)}}
	res := ds.runQuery(context.Background(), q, false)
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	f := res.Frames[0]
	if rows, _ := f.RowLen(); rows != total {
		t.Fatalf("rows = %d, want %d", rows, total)
	}
	var col *data.Field
	for _, fl := range f.Fields {
		if fl.Name == "i" {
			col = fl
		}
	}
	for i := 0; i < total; i++ {
		if col.At(i) != fmt.Sprint(i) {
			t.Fatalf("row %d = %v: pages out of order", i, col.At(i))
		}
	}
	if n.Load() > 5 { // 1 + one batch of 4
		t.Errorf("requests = %d", n.Load())
	}
}
