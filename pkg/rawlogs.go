package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/pierrec/lz4/v4"
)

// SLS returns every log as a JSON object whose keys keep the order in which the fields were
// written (level, msg, ... then the meta fields). The SDK decodes it into a map and loses that
// order, so log requests are sent through LogProject.RawRequest (same signing, same *sls.Error on
// failure) and decoded here, keeping each row's key order for the log line.

// logsResponse is a GetLogs result with the key order of every row.
type logsResponse struct {
	Meta sls.GetLogsV3ResponseMeta
	Logs []map[string]string
	// Keys holds each row's keys in SLS order; nil when unknown.
	Keys [][]string
}

// getLogs runs GetLogs (v3 API) and stops waiting when ctx is done. The SDK takes no context, so
// the HTTP request itself is only bounded by the project's request timeout.
func (ds *datasource) getLogs(ctx context.Context, logstore string, req *sls.GetLogRequest) (*logsResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return call(ctx, func() (*logsResponse, error) {
		raw, err := ds.rawRequest(ctx, "POST", "/logstores/"+url.PathEscape(logstore)+"/logs", map[string]string{
			"x-log-bodyrawsize": strconv.Itoa(len(body)),
			"Content-Type":      "application/json",
			"Accept-Encoding":   "lz4",
		}, body)
		if err != nil {
			return nil, err
		}
		var out struct {
			Meta sls.GetLogsV3ResponseMeta `json:"meta"`
			Data []json.RawMessage         `json:"data"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("decode GetLogs response: %w", err)
		}
		resp := &logsResponse{Meta: out.Meta}
		resp.Logs, resp.Keys, err = decodeRows(out.Data)
		return resp, err
	})
}

// contextLogs runs GetContextLogs, keeping the key order of every row.
func (ds *datasource) contextLogs(ctx context.Context, logstore string, back, forward int32, packID, packMeta string) (*logsResponse, error) {
	q := url.Values{}
	q.Set("type", "context_log")
	q.Set("back_lines", strconv.Itoa(int(back)))
	q.Set("forward_lines", strconv.Itoa(int(forward)))
	q.Set("pack_id", packID)
	q.Set("pack_meta", packMeta)
	return call(ctx, func() (*logsResponse, error) {
		raw, err := ds.rawRequest(ctx, "GET", "/logstores/"+url.PathEscape(logstore)+"?"+q.Encode(),
			map[string]string{"x-log-bodyrawsize": "0", "Accept": "application/json"}, nil)
		if err != nil {
			return nil, err
		}
		var out struct {
			Logs []json.RawMessage `json:"logs"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("decode GetContextLogs response: %w", err)
		}
		resp := &logsResponse{}
		resp.Logs, resp.Keys, err = decodeRows(out.Logs)
		return resp, err
	})
}

// Retry budget of one raw request, as the SDK's own requests use (retryTimeout).
const (
	retryFirstWait = 200 * time.Millisecond
	retryMaxWait   = 3 * time.Second
	// maxResponseSize bounds the decompressed body a response may declare.
	maxResponseSize = 1 << 30
)

// rawRequest sends a signed request and returns the (decompressed) body. RawRequest skips the
// SDK's retry, so it is done here with the same rule (network errors and 5xx responses, until
// retryTimeout). Non-200 responses come back from the SDK as *sls.Error or *sls.BadResponseError;
// network errors are wrapped as SLS client errors so they count as downstream.
func (ds *datasource) rawRequest(ctx context.Context, method, uri string, headers map[string]string, body []byte) ([]byte, error) {
	deadline := time.Now().Add(retryTimeout)
	wait := retryFirstWait
	for {
		h := make(map[string]string, len(headers)) // the SDK adds signature headers to the map
		for k, v := range headers {
			h[k] = v
		}
		buf, err := ds.rawRequestOnce(method, uri, h, body)
		if err == nil || !retryable(err) || time.Now().Add(wait).After(deadline) {
			return buf, asSLSError(err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, retryMaxWait)
	}
}

func (ds *datasource) rawRequestOnce(method, uri string, headers map[string]string, body []byte) ([]byte, error) {
	r, err := ds.project.RawRequest(method, uri, headers, body)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if r.Header.Get("x-log-compresstype") != "lz4" {
		return buf, nil
	}
	size, err := strconv.Atoi(r.Header.Get("x-log-bodyrawsize"))
	if err != nil || size <= 0 || size > maxResponseSize {
		return nil, fmt.Errorf("lz4 response with an invalid x-log-bodyrawsize %q", r.Header.Get("x-log-bodyrawsize"))
	}
	out := make([]byte, size)
	n, err := lz4.UncompressBlock(buf, out)
	if err != nil {
		return nil, fmt.Errorf("decompress response: %w", err)
	}
	return out[:n], nil
}

// retryable mirrors the SDK's rule for read requests: network failures and 5xx responses.
func retryable(err error) bool {
	var ue *url.Error
	var se *sls.Error
	var be *sls.BadResponseError
	switch {
	case errors.As(err, &ue):
		return true
	case errors.As(err, &se):
		return se.HTTPCode >= 500 && se.HTTPCode <= 599
	case errors.As(err, &be):
		return be.HTTPCode >= 500 && be.HTTPCode <= 599
	}
	return false
}

// asSLSError wraps errors that are not SLS responses (DNS, connection, TLS) as SLS client errors.
func asSLSError(err error) error {
	var se *sls.Error
	var be *sls.BadResponseError
	var ue *url.Error
	if err == nil || errors.As(err, &se) || errors.As(err, &be) {
		return err
	}
	if errors.As(err, &ue) {
		return sls.NewClientError(err)
	}
	return err
}

func decodeRows(raw []json.RawMessage) ([]map[string]string, [][]string, error) {
	logs := make([]map[string]string, len(raw))
	keys := make([][]string, len(raw))
	var prev []string
	for i, r := range raw {
		var err error
		if logs[i], keys[i], err = decodeObject(r, prev); err != nil {
			return nil, nil, fmt.Errorf("decode log %d: %w", i, err)
		}
		prev = keys[i]
	}
	return logs, keys, nil
}

// decodeObject decodes a flat JSON object of string values (an SLS log) into a map plus its keys
// in order. Strings without escapes, the usual case, are taken without going through
// encoding/json; numbers, booleans and null are kept as their JSON text.
//
// Logs of one result mostly share their fields: keys equal to the previous row's (prev) reuse its
// strings, and when all of them match, its key slice, which buildLogsFrame then resolves once.
func decodeObject(b []byte, prev []string) (map[string]string, []string, error) {
	p := parser{b: b}
	p.ws()
	if !p.eat('{') {
		return nil, nil, p.errorf("object expected")
	}
	m := make(map[string]string, 24)
	var keys []string
	p.ws()
	if p.eat('}') {
		return m, keys, nil
	}
	keys = make([]string, 0, max(len(prev), 8))
	same := true
	for {
		p.ws()
		var k string
		var err error
		if n := len(keys); n < len(prev) {
			k, err = p.strReusing(prev[n])
		} else {
			k, err = p.str()
		}
		if err != nil {
			return nil, nil, err
		}
		p.ws()
		if !p.eat(':') {
			return nil, nil, p.errorf("':' expected")
		}
		p.ws()
		var v string
		if p.i < len(p.b) && p.b[p.i] == '"' {
			if v, err = p.str(); err != nil {
				return nil, nil, err
			}
		} else {
			start := p.i
			for p.i < len(p.b) && p.b[p.i] != ',' && p.b[p.i] != '}' && !isJSONSpace(p.b[p.i]) {
				p.i++
			}
			if v = string(p.b[start:p.i]); v == "null" {
				v = ""
			}
		}
		if _, dup := m[k]; !dup {
			n := len(keys)
			same = same && n < len(prev) && prev[n] == k
			keys = append(keys, k)
		}
		m[k] = v
		p.ws()
		if p.eat('}') {
			if same && len(keys) == len(prev) {
				keys = prev
			}
			return m, keys, nil
		}
		if !p.eat(',') {
			return nil, nil, p.errorf("',' or '}' expected")
		}
	}
}

type parser struct {
	b []byte
	i int
}

func isJSONSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (p *parser) ws() {
	for p.i < len(p.b) && isJSONSpace(p.b[p.i]) {
		p.i++
	}
}

func (p *parser) eat(c byte) bool {
	if p.i < len(p.b) && p.b[p.i] == c {
		p.i++
		return true
	}
	return false
}

func (p *parser) errorf(msg string) error { return fmt.Errorf("%s at offset %d", msg, p.i) }

// strReusing reads a string and returns want instead of a new copy when they are equal.
func (p *parser) strReusing(want string) (string, error) {
	start := p.i
	if end := start + 1 + len(want); end < len(p.b) && p.b[start] == '"' && p.b[end] == '"' &&
		string(p.b[start+1:end]) == want && strings.IndexByte(want, '\\') < 0 {
		p.i = end + 1
		return want, nil
	}
	return p.str()
}

func (p *parser) str() (string, error) {
	if !p.eat('"') {
		return "", p.errorf("string expected")
	}
	start, escaped := p.i, false
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case '\\':
			escaped = true
			p.i += 2
			continue
		case '"':
			s := p.b[start:p.i]
			p.i++
			if !escaped {
				return string(s), nil
			}
			var out string
			if err := json.Unmarshal(p.b[start-1:p.i], &out); err != nil {
				return "", err
			}
			return out, nil
		}
		p.i++
	}
	return "", p.errorf("unterminated string")
}
