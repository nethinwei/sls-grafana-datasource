package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

// CheckHealth works with least-privilege AccessKeys that are only granted on one logstore:
//  1. GetLogStore on the default logstore (log:GetLogStore);
//  2. otherwise read one log from the last minute (log:GetLogStoreLogs), which is all queries need.
//
// Without a default logstore it lists logstores (log:ListLogStores, project level).
func (ds *datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if ds.cfg == nil {
		msg := "Data source is not configured: set the endpoint (URL), project and AccessKey."
		if ds.cfgErr != nil {
			msg = "Invalid data source settings: " + ds.cfgErr.Error()
		}
		return healthError(msg, nil), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if ds.cfg.LogStore == "" {
		if _, err := call(ctx, func() ([]string, error) { return ds.client.ListLogStoreV2(ds.cfg.Project, 0, 1, "") }); err != nil {
			return healthError(fmt.Sprintf("Cannot list logstores in project %q. Set a default logstore, or grant log:ListLogStores.", ds.cfg.Project), err), nil
		}
		res := &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: fmt.Sprintf("Connected to project %q.", ds.cfg.Project)}
		if len(ds.cfg.view.problems) > 0 {
			res.Message += " Ignored field rules: " + strings.Join(ds.cfg.view.problems, "; ")
		}
		return res, nil
	}

	_, storeErr := call(ctx, func() (*sls.LogStore, error) { return ds.client.GetLogStore(ds.cfg.Project, ds.cfg.LogStore) })
	if storeErr == nil {
		return ok(ds.cfg), nil
	}
	now := time.Now()
	_, logsErr := ds.getLogs(ctx, ds.cfg.LogStore, &sls.GetLogRequest{From: now.Add(-time.Minute).Unix(), To: now.Unix(), Query: "*", Lines: 1})
	if logsErr == nil {
		res := ok(ds.cfg)
		res.Message += " (log:GetLogStore is not granted; queries work.)"
		return res, nil
	}
	if unreachable(logsErr) {
		return healthError(fmt.Sprintf("Cannot reach the SLS endpoint %s: check the URL (region, -intranet only inside Alibaba Cloud) and the network.",
			ds.cfg.Endpoint), logsErr), nil
	}
	return healthError(fmt.Sprintf("Cannot read logstore %q in project %q. The AccessKey needs log:GetLogStoreLogs on it (log:GetLogStore is optional).",
		ds.cfg.LogStore, ds.cfg.Project), errors.Join(storeErr, logsErr)), nil
}

func ok(cfg *settings) *backend.CheckHealthResult {
	msg := fmt.Sprintf("Connected to %s/%s.", cfg.Project, cfg.LogStore)
	if len(cfg.view.problems) > 0 {
		msg += " Ignored field rules: " + strings.Join(cfg.view.problems, "; ")
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: msg}
}

// unreachable reports errors that never got an SLS response (DNS, connection, TLS, timeout).
func unreachable(err error) bool {
	var se *sls.Error
	if errors.As(err, &se) {
		return se.HTTPCode <= 0
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func healthError(msg string, err error) *backend.CheckHealthResult {
	res := &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: msg}
	if err != nil {
		_, _, detail := classifyError(err)
		res.JSONDetails, _ = json.Marshal(map[string]string{"message": msg, "verboseMessage": detail})
	}
	return res
}

// call runs an SLS SDK call (which takes no context) and stops waiting when ctx is done.
func call[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		// A panic here (a malformed response, an SDK bug) would end the whole plugin process: the
		// query's own recover runs on another goroutine.
		defer func() {
			if r := recover(); r != nil {
				log.DefaultLogger.Error("SLS call panicked", "panic", r)
				var zero T
				ch <- result{zero, backend.PluginError(fmt.Errorf("internal error: %v", r))}
			}
		}()
		v, err := fn()
		ch <- result{v, err}
	}()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case r := <-ch:
		return r.v, r.err
	}
}
