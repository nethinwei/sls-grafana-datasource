package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestErrorResponse(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status backend.Status
		source backend.ErrorSource
	}{
		{"SLS syntax error", &sls.Error{HTTPCode: 400, Code: "ParameterInvalid", Message: "bad query", RequestID: "r1"}, 400, backend.ErrorSourceDownstream},
		{"SLS permission", fmt.Errorf("wrapped: %w", &sls.Error{HTTPCode: 401, Code: "Unauthorized"}), 401, backend.ErrorSourceDownstream},
		{"no HTTP response", &sls.Error{HTTPCode: -1, Code: "ClientError", Message: "timeout"}, backend.StatusBadGateway, backend.ErrorSourceDownstream},
		{"deadline", context.DeadlineExceeded, backend.StatusTimeout, backend.ErrorSourceDownstream},
		{"user error", userError("no logstore"), backend.StatusBadRequest, backend.ErrorSourceDownstream},
		{"plugin bug", errors.New("nil map"), backend.StatusInternal, backend.ErrorSourcePlugin},
	}
	for _, c := range cases {
		r := errorResponse(c.err)
		if r.Status != c.status || r.ErrorSource != c.source || r.Error == nil {
			t.Errorf("%s: status=%v source=%v err=%v; want %v %v", c.name, r.Status, r.ErrorSource, r.Error, c.status, c.source)
		}
	}
	if r := errorResponse(&sls.Error{HTTPCode: 400, Code: "X", Message: "m", RequestID: "req-9"}); r.Error.Error() != "SLS X: m (request ID req-9)" {
		t.Errorf("message = %q", r.Error)
	}
}
