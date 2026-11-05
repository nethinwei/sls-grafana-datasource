package main

import (
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
	if f := get("attrs.code"); f.Type != "long" || !*f.Stats {
		t.Errorf("attrs.code = %+v", f)
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
	cat := &catalog{Fields: []catalogField{{Name: "msg", Indexed: &no}}}
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
			`* | where regexp_like("level", '(a|b)')`, 0},
		{"no index uses where", "x", []adhocFilter{{Key: "msg", Operator: "=", Value: "it's"}, {Key: "level", Operator: ">", Value: "3"}},
			`x and level > 3 | where "msg" = 'it''s'`, 0},
		{"no index in SQL is skipped", "* | select 1", []adhocFilter{{Key: "msg", Operator: "=", Value: "a"}}, "* | select 1", 1},
		{"derived field is skipped", "*", []adhocFilter{{Key: "service.name", Operator: "=", Value: "pay"}}, "*", 1},
	}
	for _, c := range cases {
		got, skipped := applyAdhocFilters(c.query, c.filters, v, cat)
		if got != c.want || len(skipped) != c.skipped {
			t.Errorf("%s:\n got %q %v\nwant %q (%d skipped)", c.name, got, skipped, c.want, c.skipped)
		}
	}
}
