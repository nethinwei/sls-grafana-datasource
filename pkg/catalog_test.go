package main

import (
	"fmt"
	"testing"

	sls "github.com/aliyun/aliyun-log-go-sdk"
)

var otelTestRules = append(append([]fieldRule{}, defaultFieldRules...),
	fieldRule{Type: "rename", From: "__tag__:__hostname__", To: "host.name"},
	fieldRule{Type: "rename", From: "__tag__:__path__", To: "log.file.path"},
	fieldRule{Type: "extract", From: "log.file.path", To: "service.name", Pattern: `/logs/(.+)\.log`},
	fieldRule{Type: "drop", Field: "__topic__"},
)

func TestLogsViewNames(t *testing.T) {
	v := testView(otelTestRules, nil, true)
	if v.slsNames["host.name"] != "__tag__:__hostname__" || v.slsNames["log.file.path"] != "__tag__:__path__" {
		t.Errorf("slsNames = %v", v.slsNames)
	}
	if !v.derived["service.name"] || v.derived["host.name"] || v.derived[defaultTimeField] {
		t.Errorf("derived = %v (the time field is the row timestamp, not a field)", v.derived)
	}
	if v.shownNames["__tag__:__hostname__"] != "host.name" {
		t.Errorf("shownNames = %v", v.shownNames)
	}
}

func TestBuildCatalog(t *testing.T) {
	index := &sls.Index{Keys: map[string]sls.IndexKey{
		"level":                {Type: "text", DocValue: true},
		"__tag__:__hostname__": {Type: "text", DocValue: true},
		"attrs":                {Type: "json", JsonKeys: map[string]*sls.JsonKey{"code": {Type: "long", DocValue: true}}},
	}}
	sample := []map[string]string{{
		"__time__": "1", "__time_ns_part__": "2", "__topic__": "", "__tag__:__path__": "/logs/pay.log",
		"__tag__:__hostname__": "web-1", "level": "INFO", "msg": "hi",
	}}
	c := buildCatalog(index, sample, testView(otelTestRules, nil, true))

	get := func(name string) catalogField {
		f, ok := c.byName(name)
		if !ok {
			t.Fatalf("missing %s in %+v", name, c.Fields)
		}
		return f
	}
	if f := get("host.name"); f.SLSName != "__tag__:__hostname__" || f.Rule != "rename" || !*f.Indexed || !*f.Stats || f.Type != "text" {
		t.Errorf("host.name = %+v", f)
	}
	if f := get("log.file.path"); f.SLSName != "__tag__:__path__" || !*f.Indexed { // meta fields need no field index
		t.Errorf("log.file.path = %+v", f)
	}
	if f := get("msg"); *f.Indexed || *f.Stats {
		t.Errorf("msg is not in the index: %+v", f)
	}
	if f := get("service.name"); f.SLSName != "" || f.Rule != "extract" || f.Indexed != nil {
		t.Errorf("service.name = %+v", f)
	}
	if f := get("attrs_code"); f.Type != "long" || !*f.Stats || f.SLSName != "attrs.code" { // flattened by the default json rule
		t.Errorf("attrs_code = %+v", f)
	}
	for _, gone := range []string{"__time__", "__time_ns_part__", "__topic__", "__tag__:__hostname__", "__tag__:__path__", defaultTimeField} {
		if _, ok := c.byName(gone); ok {
			t.Errorf("%s should not be in the catalog", gone)
		}
	}
	if !c.IndexKnown {
		t.Error("IndexKnown")
	}

	// Without the index, fields come from the sample with unknown index state.
	c = buildCatalog(nil, sample, testView(otelTestRules, nil, true))
	if f, _ := c.byName("msg"); f.Indexed != nil || c.IndexKnown {
		t.Errorf("unknown index: %+v %v", f, c.IndexKnown)
	}
}

func TestApplyAdhocFilters(t *testing.T) {
	v := testView(otelTestRules, nil, true)
	no := false
	cat := &catalog{Fields: []catalogField{{Name: "msg", Indexed: &no, SLSName: "msg"}}}
	cases := []struct {
		name, query string
		filters     []adhocFilter
		want        string
		skipped     int
	}{
		{"rename back to SLS name", "*", []adhocFilter{{Key: "host.name", Operator: "=", Value: "web-1"}},
			`__tag__:__hostname__: "web-1"`, 0},
		{"keeps SQL after search", "level: ERROR or level: WARN | select count(*)",
			[]adhocFilter{{Key: "level", Operator: "!=", Value: `a"b`}},
			`(level: ERROR or level: WARN) and not level: "a\"b" | select count(*)`, 0},
		{"one of", "*", []adhocFilter{{Key: "level", Operator: "=|", Values: []string{"a", "b"}}},
			`(level: "a" or level: "b")`, 0},
		{"wildcard regex", "*", []adhocFilter{{Key: "level", Operator: "=~", Value: "^ERR.*"}}, `level: ERR*`, 0},
		{"complex regex falls back to where", "*", []adhocFilter{{Key: "level", Operator: "=~", Value: "(a|b)"}},
			`* | where regexp_like("level", '^(?:(a|b))$')`, 0},
		{"no index uses where", "x", []adhocFilter{{Key: "msg", Operator: "=", Value: "it's"}, {Key: "level", Operator: ">", Value: "3"}},
			`x and level > 3 | where "msg" = 'it''s'`, 0},
		{"no index in SQL is skipped", "* | select 1", []adhocFilter{{Key: "msg", Operator: "=", Value: "a"}}, "* | select 1", 1},
		{"derived field is skipped", "*", []adhocFilter{{Key: "service.name", Operator: "=", Value: "pay"}}, "*", 1},
	}
	for _, c := range cases {
		got, skipped := applyAdhocFilters(c.query, c.filters, v, cat, nil)
		if got != c.want || len(skipped) != c.skipped {
			t.Errorf("%s:\n got %q %v\nwant %q (%d skipped)", c.name, got, skipped, c.want, c.skipped)
		}
	}
}

func TestJSONRule(t *testing.T) {
	logs := []map[string]string{{
		"__time__": "1700000000", "msg": "psp request", "request_url": "own",
		"request": `{"url":"https://x/charge","header":{"X-Request-Id":["r-1"],"Content-Type":"application/json"},"retry":2,"ok":true,"note":null}`,
		"order":   `{"id":"TP1","amount":{"value":"12.50"},"items":[{"sku":"a"}]}`,
		"plain":   "{not json",
	}}
	got := applyRow(t, append(append([]fieldRule{}, defaultFieldRules...), fieldRule{Type: "json"}), logs[0])
	want := map[string]string{
		"request_url_extracted":       "https://x/charge", // request_url exists: Loki's _extracted suffix
		"request_url":                 "own",
		"request_header_Content_Type": "application/json",
		"request_retry":               "2",
		"request_ok":                  "true",
		"order_id":                    "TP1",
		"order_amount_value":          "12.50",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for _, k := range []string{"request_header_X_Request_Id", "request_note", "order_items", "plain_"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s: arrays, nulls and invalid JSON are skipped", k)
		}
	}
	if got["request"] == "" {
		t.Error("the source field is kept")
	}
	// Only the named field when from is set.
	only := applyRow(t, []fieldRule{{Type: "json", From: "order"}}, logs[0])
	if _, ok := only["request_retry"]; ok || only["order_id"] != "TP1" {
		t.Errorf("from=order: %v", only)
	}
}

func TestCatalogJSONFields(t *testing.T) {
	index := &sls.Index{Keys: map[string]sls.IndexKey{
		"request": {Type: "json", JsonKeys: map[string]*sls.JsonKey{"method": {Type: "text", DocValue: true}}},
	}}
	sample := []map[string]string{{"msg": "a", "request": `{"method":"POST","path":"/v1"}`}}
	view := testView(append(append([]fieldRule{}, defaultFieldRules...), fieldRule{Type: "json"}), nil, true)
	c := buildCatalog(index, sample, view)
	if f, ok := c.byName("request_method"); !ok || f.SLSName != "request.method" || f.Rule != "json" || !*f.Indexed {
		t.Errorf("indexed JSON key: %+v", f)
	}
	if f, ok := c.byName("request_path"); !ok || f.SLSName != "" || f.Rule != "json" || f.Expr != `coalesce("request_path", json_extract_scalar("request", '$["path"]'))` {
		t.Errorf("JSON key found in samples: %+v", f)
	}
	if _, ok := c.byName("request.method"); ok {
		t.Error("the dotted index name is replaced by the flattened one")
	}
	cat := &catalog{Fields: c.Fields}
	q, skipped := applyAdhocFilters("*", []adhocFilter{{Key: "request_method", Operator: "=", Value: "POST"}, {Key: "request_path", Operator: "=", Value: "/v1"}}, view, cat, nil)
	if q != `request.method: "POST" | where coalesce("request_path", json_extract_scalar("request", '$["path"]')) = '/v1'` || len(skipped) != 0 {
		t.Errorf("filters: %q %v", q, skipped)
	}
}

func TestFlattenedFieldFilters(t *testing.T) {
	view := testView(append(append([]fieldRule{}, defaultFieldRules...),
		fieldRule{Type: "json"}, fieldRule{Type: "extract", From: "msg", To: "word", Pattern: `(\w+)`}), nil, true)
	f := buildLogsFrame([]map[string]string{{"__time__": "1700000000", "msg": "a",
		"http": `{"method":"POST","client":{"ip":"10.1.2.3"}}`, "labels": `{"app.kubernetes.io/name":"pay"}`}}, nil, view)
	custom := f.Meta.Custom.(map[string]any)
	if fmt.Sprint(custom[grafanaOnlyFieldsKey]) != "[word]" {
		t.Errorf("grafanaOnlyFields = %v", custom[grafanaOnlyFieldsKey])
	}
	exprs := custom[fieldExprsKey].(map[string]string)
	if exprs["http_client_ip"] != `coalesce("http_client_ip", json_extract_scalar("http", '$["client"]["ip"]'))` ||
		exprs["labels_app_kubernetes_io_name"] != `coalesce("labels_app_kubernetes_io_name", json_extract_scalar("labels", '$["app.kubernetes.io/name"]'))` {
		t.Errorf("exprs = %v", exprs)
	}
	ds := &datasource{}
	ds.learnFields(f)
	// Flattened fields the catalog's sample missed are filtered with SPL, like Loki's | json | k="v";
	// fields produced by other rules are skipped.
	q, skipped := applyAdhocFilters("level: ERROR", []adhocFilter{
		{Key: "http_method", Operator: "=", Value: "POST"},
		{Key: "http_client_ip", Operator: "!=", Value: "1.1.1.1"},
		{Key: "word", Operator: "=", Value: "a"},
	}, view, &catalog{}, ds.learnedField)
	want := `level: ERROR | where coalesce("http_method", json_extract_scalar("http", '$["method"]')) = 'POST' and coalesce(coalesce("http_client_ip", json_extract_scalar("http", '$["client"]["ip"]')), '') != '1.1.1.1'`
	if q != want || len(skipped) != 1 {
		t.Errorf("got  %s %v\nwant %s", q, skipped, want)
	}
}
