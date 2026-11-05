package main

import (
	"context"
	"errors"
	"fmt"

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
	switch {
	case errors.As(err, &slsErr) && slsErr.HTTPCode > 0:
		msg := fmt.Sprintf("SLS %s: %s", slsErr.Code, slsErr.Message)
		if slsErr.RequestID != "" {
			msg += " (request ID " + slsErr.RequestID + ")"
		}
		return backend.Status(slsErr.HTTPCode), backend.ErrorSourceDownstream, msg
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
