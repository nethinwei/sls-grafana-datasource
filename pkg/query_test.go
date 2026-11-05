package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestParseQueryLegacyFields(t *testing.T) {
	cases := []struct {
		json       string
		maxLines   int64
		timeColumn string
	}{
		{`{"query":"*"}`, defaultMaxLines, "time"},
		{`{"query":"*","logsPerPage":100,"totalLogs":500}`, 500, "time"},
		{`{"query":"*","maxLines":99999}`, maxMaxLines, "time"},
		{`{"query":"*","xcol":"t","ycol":"svc#:#c"}`, defaultMaxLines, "t"},
		{`{"query":"*","xcol":"pie"}`, defaultMaxLines, "time"},
		{`{"query":"*","xcol":"t","timeColumn":"ts"}`, defaultMaxLines, "ts"},
	}
	for _, c := range cases {
		m, err := parseQuery(backend.DataQuery{JSON: json.RawMessage(c.json)})
		if err != nil {
			t.Fatalf("%s: %v", c.json, err)
		}
		if m.MaxLines != c.maxLines || m.TimeColumn != c.timeColumn {
			t.Errorf("%s: maxLines=%d timeColumn=%q, want %d %q", c.json, m.MaxLines, m.TimeColumn, c.maxLines, c.timeColumn)
		}
	}
}

func TestApplyMacros(t *testing.T) {
	tr := backend.TimeRange{From: time.Unix(1700000000, 0), To: time.Unix(1700003600, 0)}
	cases := map[string]string{
		"* | select $__timeGroup(__time__) as time, count(*) group by time":            "* | select (__time__ - __time__ % 60) as time, count(*) group by time",
		"* | select $__timeGroup( __time__ , 5m ) as time":                             "* | select (__time__ - __time__ % 300) as time",
		"* | select __time__ - __time__ % $__interval_s as t":                          "* | select __time__ - __time__ % 60 as t",
		"* | select count(*) where __time__ >= $__timeFrom and __time__ < $__timeTo()": "* | select count(*) where __time__ >= 1700000000 and __time__ < 1700003600",
	}
	for in, want := range cases {
		got, err := applyMacros(in, tr, time.Minute)
		if err != nil || got != want {
			t.Errorf("applyMacros(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if got, _ := applyMacros("$__interval_s", tr, 200*time.Millisecond); got != "1" {
		t.Errorf("sub-second interval should round up to 1, got %q", got)
	}
	if _, err := applyMacros("$__timeGroup(__time__, abc)", tr, time.Minute); err == nil {
		t.Error("expected an error for an invalid $__timeGroup interval")
	}
}

func TestWithPackMeta(t *testing.T) {
	cases := map[string]string{
		"level: ERROR":                        "level: ERROR | with_pack_meta",
		"* | where cost > 1":                  "* | where cost > 1 | with_pack_meta",
		"* | select count(*) as c":            "* | select count(*) as c",
		"* |SELECT a from log":                "* |SELECT a from log",
		"level: ERROR | with_pack_meta":       "level: ERROR | with_pack_meta",
		"* | where a = 'x' | WITH_PACK_META ": "* | where a = 'x' | WITH_PACK_META ",
	}
	for in, want := range cases {
		if got := withPackMeta(in); got != want {
			t.Errorf("withPackMeta(%q) = %q, want %q", in, got, want)
		}
	}
}
