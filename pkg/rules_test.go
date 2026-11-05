package main

import (
	"fmt"
	"testing"
)

// applyRow runs rules on a single row and returns its fields.
func applyRow(t *testing.T, rules []fieldRule, row map[string]string) map[string]string {
	t.Helper()
	v := testView(rules, nil, true)
	tab, _ := v.table([]map[string]string{row})
	out := map[string]string{}
	for k, c := range tab.cols {
		if c.present[0] {
			out[k] = c.values[0]
		}
	}
	return out
}

func TestApplyRules(t *testing.T) {
	in := map[string]string{
		"__tag__:__path__": "/data/logs/payment.log", "url": "https://api.example.com/v1/pay?x=1",
		"region": "cn", "env": "", "start": "1700000000", "__source__": "10.0.0.1", "service": "pay",
	}
	got := applyRow(t, []fieldRule{
		{Type: "extract", From: "__tag__:__path__", To: "service.name", Pattern: `/logs/(?P<svc>[^/]+)\.log$`, Template: "${svc}"},
		{Type: "extract", From: "url", To: "url.path", Pattern: `^https?://[^/]+(/[^?]*)`},
		{Type: "extract", From: "url", To: "nomatch", Pattern: `^ftp://`},
		{Type: "default", To: "deployment.environment", Value: "dev"},
		{Type: "default", To: "region", Value: "sg"},
		{Type: "default", To: "env", Value: "prod"},
		{Type: "time", From: "start", To: "start.time"},
		{Type: "time", From: "start", To: "start.ms", Format: "unix_ms"},
		{Type: "template", To: "app", Template: "${service}/${region}"},
		{Type: "drop", Field: "__source__"},
		{Type: "rename", From: "missing", To: "x"},
	}, in)
	want := map[string]string{
		"service.name":           "payment",
		"url.path":               "/v1/pay",
		"deployment.environment": "dev",
		"region":                 "cn",   // default does not overwrite a value
		"env":                    "prod", // but fills an empty one
		"start.time":             "2023-11-14T22:13:20Z",
		"start.ms":               "1700000000000",
		"app":                    "pay/cn",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for _, k := range []string{"__source__", "x", "nomatch"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s should not exist", k)
		}
	}
}

func TestRenameKeepsOwnField(t *testing.T) {
	rows := []map[string]string{{"__tag__:__hostname__": "web-1"}, {"__tag__:__hostname__": "web-2", "host.name": "app"}}
	tab, _ := testView([]fieldRule{{Type: "rename", From: "__tag__:__hostname__", To: "host.name"}}, nil, true).table(rows)
	c := tab.cols["host.name"]
	if c.values[0] != "web-1" || c.values[1] != "app" || tab.cols["__tag__:__hostname__"] != nil {
		t.Errorf("host.name = %v, source column left = %v", c.values, tab.cols["__tag__:__hostname__"] != nil)
	}
}

func TestTimeRuleCombinesNanos(t *testing.T) {
	rules := []fieldRule{
		{Type: "time", From: "__time__", Nanos: "__time_ns_part__", To: "ts"},
		{Type: "time", From: "__time__", Nanos: "__time_ns_part__", To: "ts_ns", Format: "unix_ns"},
	}
	with := applyRow(t, rules, map[string]string{"__time__": "1700000000", "__time_ns_part__": "123456789"})
	if with["ts"] != "2023-11-14T22:13:20.123456789Z" || with["ts_ns"] != "1700000000123456789" {
		t.Errorf("with nanos: %v %v", with["ts"], with["ts_ns"])
	}
	// Collectors without nanosecond timestamps: the nanoseconds are 0.
	without := applyRow(t, rules, map[string]string{"__time__": "1700000000"})
	if without["ts"] != "2023-11-14T22:13:20Z" || without["ts_ns"] != "1700000000000000000" {
		t.Errorf("without nanos: %v %v", without["ts"], without["ts_ns"])
	}
}

func TestTemplateRule(t *testing.T) {
	got := applyRow(t, []fieldRule{
		{Type: "template", To: "app", Template: "${service}/${env}"},
		{Type: "template", To: "where", Template: "${__tag__:__hostname__}:${__tag__:__path__} $$"},
		{Type: "template", To: "none", Template: "${missing}-x"},
	}, map[string]string{"service": "pay", "__tag__:__hostname__": "web-1", "__tag__:__path__": "/a.log"})
	if got["app"] != "pay/" {
		t.Errorf("app = %q (absent fields render empty)", got["app"])
	}
	if got["where"] != "web-1:/a.log $" {
		t.Errorf("where = %q", got["where"])
	}
	if _, ok := got["none"]; ok {
		t.Error("a template whose fields are all absent must not create the target")
	}
}

func TestCompileRulesRejectsInvalid(t *testing.T) {
	_, problems := compileRules([]fieldRule{
		{Type: "extract", From: "a", To: "b", Pattern: "("},
		{Type: "rename", From: "a"},
		{Type: "rename", From: "__tag__:__pack_id__", To: "pack"},
		{Type: "drop", Field: "__pack_meta__"},
		{Type: "time", From: "a", To: "b", Format: "iso"},
		{Type: "template", To: "a", Template: "${x"},
		{Type: "upper", From: "a", To: "b"},
	})
	if len(problems) != 7 {
		t.Errorf("want 7 problems, got %v", problems)
	}
}

// 5000 rows (the most one query returns) with the default rules plus a typical OpenTelemetry set.
func BenchmarkLogsFrame(b *testing.B) {
	logs := make([]map[string]string, 5000)
	keys := make([][]string, len(logs))
	order := []string{"level", "msg", "order_id", "trace_id", "span_id", "__pack_meta__", "__topic__", "__source__",
		"__tag__:__pack_id__", "__tag__:__path__", "__tag__:__hostname__", "__tag__:__host_ip__", "__time__", "__time_ns_part__"}
	for i := range logs {
		keys[i] = order
		logs[i] = map[string]string{
			"__time__": "1700000000", "__time_ns_part__": fmt.Sprint(i), "__topic__": "", "__source__": "10.0.0.1",
			"__tag__:__hostname__": "web-1", "__tag__:__host_ip__": "10.0.0.1", "__tag__:__path__": "/logs/payment.log",
			"__tag__:__pack_id__": "P", "__pack_meta__": "0|x|1|" + fmt.Sprint(i),
			"level": "INFO", "msg": "pay routed", "order_id": "TP123", "trace_id": "abc", "span_id": "def",
		}
	}
	otel := append(append([]fieldRule{}, defaultFieldRules...),
		fieldRule{Type: "rename", From: "__tag__:__hostname__", To: "host.name"},
		fieldRule{Type: "rename", From: "__tag__:__host_ip__", To: "host.ip"},
		fieldRule{Type: "rename", From: "__tag__:__path__", To: "log.file.path"},
		fieldRule{Type: "extract", From: "log.file.path", To: "service.name", Pattern: `/logs/(.+)\.log`},
		fieldRule{Type: "drop", Field: "__topic__"},
		fieldRule{Type: "default", To: "deployment.environment", Value: "prod"},
	)
	cases := []struct {
		name  string
		rules []fieldRule
		line  []string
	}{
		{"no-rules", []fieldRule{}, nil},
		{"default-rules", defaultFieldRules, nil},
		{"otel-rules+line", otel, []string{"host.name", "host.ip"}},
	}
	for _, c := range cases {
		view := testView(c.rules, c.line, true)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				buildLogsFrame(logs, keys, view)
			}
		})
	}
}
