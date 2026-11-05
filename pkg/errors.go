package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// errorResponse turns an error into a DataResponse with an HTTP status and an error source, so
// Grafana can tell a user's mistake or an SLS-side problem (downstream) from a plugin bug (plugin).
//
//   - SLS API errors (syntax, missing index, permission, quota, server errors) are downstream and keep
//     SLS's HTTP status code, error code and request ID.
//   - Timeouts and cancellation are downstream.
//   - Anything else that was not explicitly marked is a plugin error.
func errorResponse(err error) backend.DataResponse {
	status, src, msg := classifyError(err)
	return backend.ErrDataResponseWithSource(status, src, msg)
}

func classifyError(err error) (backend.Status, backend.ErrorSource, string) {
	var slsErr *sls.Error
	var badResp *sls.BadResponseError
	switch {
	case errors.As(err, &slsErr) && slsErr.HTTPCode > 0:
		msg := fmt.Sprintf("SLS %s: %s", slsErr.Code, slsErr.Message)
		if strings.Contains(slsErr.Message, "is not config as key value") {
			msg += ". The field has no index: add a field index in SLS to search it, or filter with SPL, e.g. | where \"field\" = 'value'"
		}
		if slsErr.RequestID != "" {
			msg += " (request ID " + slsErr.RequestID + ")"
		}
		return backend.Status(slsErr.HTTPCode), backend.ErrorSourceDownstream, msg
	case errors.As(err, &badResp):
		// A response SLS did not produce as JSON, e.g. an HTML 502 from a load balancer.
		return backend.Status(badResp.HTTPCode), backend.ErrorSourceDownstream,
			fmt.Sprintf("SLS returned HTTP %d: %s", badResp.HTTPCode, truncate(badResp.RespBody, 200))
	case errors.As(err, &slsErr):
		// HTTPCode -1: the request never got an HTTP response (DNS, TLS, connection, client timeout).
		return backend.StatusBadGateway, backend.ErrorSourceDownstream, "SLS request failed: " + slsErr.Message
	case errors.Is(err, context.DeadlineExceeded):
		return backend.StatusTimeout, backend.ErrorSourceDownstream, "SLS query timed out"
	case errors.Is(err, context.Canceled):
		return backend.Status(499), backend.ErrorSourceDownstream, "query cancelled"
	}

	var withSource backend.ErrorWithSource
	if errors.As(err, &withSource) {
		status := backend.StatusBadRequest
		if withSource.ErrorSource() == backend.ErrorSourcePlugin {
			status = backend.StatusInternal
		}
		return status, withSource.ErrorSource(), err.Error()
	}
	return backend.StatusInternal, backend.ErrorSourcePlugin, err.Error()
}

// userError marks an error caused by the query or settings the user wrote (HTTP 400, downstream).
func userError(format string, a ...any) error {
	return backend.DownstreamErrorf(format, a...)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
