package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func field(t *testing.T, f *data.Frame, name string) *data.Field {
	t.Helper()
	for _, fl := range f.Fields {
		if fl.Name == name {
			return fl
		}
	}
	t.Fatalf("field %q missing; have %v", name, fieldNames(f))
	return nil
}

func fieldNames(f *data.Frame) []string {
	var n []string
	for _, fl := range f.Fields {
		n = append(n, fl.Name)
	}
	return n
}

func TestBuildLogsFrame(t *testing.T) {
	logs := []map[string]string{
		{"__time__": "1700000000", "__time_ns_part__": "123456789", "__source__": "10.0.0.1", "__topic__": "",
			"__tag__:__hostname__": "host-a", "level": "ERROR", "msg": `pay "failed"`, "code": "12001"},
		{"__time__": "1700000001", "level": "WARN", "msg": "slow", "channel": "coda", "error": ""},
	}
	f := buildLogsFrame(logs, nil, testView(nil, nil, true))

	if f.Meta.PreferredVisualization != data.VisTypeLogs {
		t.Errorf("preferred visualisation = %q", f.Meta.PreferredVisualization)
	}
	if got := field(t, f, "time").At(0).(time.Time); !got.Equal(time.Unix(1700000000, 123456789)) {
		t.Errorf("time = %v, want nanosecond precision", got)
	}
	// Without the SLS key order the line is sorted; the level is shown by Grafana, not in the line.
	want := []string{`code="12001" msg="pay \"failed\""`, `channel="coda" error="" msg="slow"`}
	for i, w := range want {
		if got := field(t, f, "message").At(i).(string); got != w {
			t.Errorf("message[%d] = %q, want %q", i, got, w)
		}
	}
	if host := field(t, f, "__tag__:__hostname__"); host.At(0) != "host-a" || host.At(1) != "" {
		t.Errorf("meta fields must stay available as columns")
	}
	if _, err := f.RowLen(); err != nil {
		t.Errorf("mismatched field lengths: %v", err)
	}
	// Stable column order across calls.
	again := buildLogsFrame(logs, nil, testView(nil, nil, true))
	if a, b := fieldNames(f), fieldNames(again); len(a) != len(b) {
		t.Fatal("column count changed")
	} else {
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("column order changed: %v vs %v", a, b)
			}
		}
	}
}

func TestBuildSQLFrameTable(t *testing.T) {
	rows := []map[string]string{
		{"service": "topup", "c": "10", "ratio": "0.5", "ok": "true"},
		{"service": "cashier", "c": "9", "ratio": "null", "ok": "false"},
	}
	f, err := buildSQLFrame([]string{"service", "c", "ratio", "ok"}, nil, rows, "time", time.UTC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if f.Meta != nil && f.Meta.Type != "" {
		t.Errorf("table should not be typed as time series, got %q", f.Meta.Type)
	}
	if got := field(t, f, "c").Type(); got != data.FieldTypeNullableFloat64 {
		t.Errorf("c type = %v, want number (so 10 sorts after 9)", got)
	}
	if v := field(t, f, "ratio").At(1).(*float64); v != nil {
		t.Errorf("null should become a null value, got %v", *v)
	}
	if got := field(t, f, "ok").Type(); got != data.FieldTypeNullableBool {
		t.Errorf("ok type = %v, want bool", got)
	}
	if got := field(t, f, "service").Type(); got != data.FieldTypeNullableString {
		t.Errorf("service type = %v, want string", got)
	}
}

func TestBuildSQLFrameColumnTypesWin(t *testing.T) {
	rows := []map[string]string{{"code": "200"}, {"code": "404"}}
	f, err := buildSQLFrame([]string{"code"}, []string{"varchar"}, rows, "time", time.UTC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := field(t, f, "code").Type(); got != data.FieldTypeNullableString {
		t.Errorf("declared varchar must stay string, got %v", got)
	}
}

// SQL grouped by time and a string column becomes one series per string value.
func TestBuildSQLFrameLongToWide(t *testing.T) {
	rows := []map[string]string{
		{"time": "1700000060", "service": "topup", "c": "3"},
		{"time": "1700000000", "service": "topup", "c": "1"},
		{"time": "1700000000", "service": "cashier", "c": "2"},
		{"time": "1700000060", "service": "cashier", "c": "4"},
	}
	f, err := buildSQLFrame([]string{"time", "service", "c"}, []string{"bigint", "varchar", "bigint"}, rows, "time", time.UTC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if f.Meta == nil || f.Meta.Type != data.FrameTypeTimeSeriesWide {
		t.Fatalf("want timeseries-wide, got %+v", f.Meta)
	}
	if len(f.Fields) != 3 { // time, c{service=cashier}, c{service=topup}
		t.Fatalf("fields = %v", fieldNames(f))
	}
	tf := f.Fields[0]
	if !tf.At(0).(time.Time).Before(tf.At(1).(time.Time)) {
		t.Error("time must be ascending")
	}
	for _, fl := range f.Fields[1:] {
		svc := fl.Labels["service"]
		first := *fl.At(0).(*float64)
		if (svc == "topup" && first != 1) || (svc == "cashier" && first != 2) {
			t.Errorf("series %s first value = %v", svc, first)
		}
	}
}

func TestBuildSQLFrameTimeSeriesWithoutStrings(t *testing.T) {
	rows := []map[string]string{{"t": "1700000060", "c": "2"}, {"t": "1700000000", "c": "1"}}
	f, err := buildSQLFrame([]string{"t", "c"}, nil, rows, "t", time.UTC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if f.Meta == nil || f.Meta.Type != data.FrameTypeTimeSeriesWide || f.Fields[0].Name != "t" {
		t.Fatalf("want wide series with time first, got %+v %v", f.Meta, fieldNames(f))
	}
	if v := *f.Fields[1].At(0).(*float64); v != 1 {
		t.Errorf("rows not sorted by time, first value %v", v)
	}
}

func TestParseTimeValue(t *testing.T) {
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	want := time.Unix(1700000000, 0)
	// "2023-11-15 06:13:20.000" is what SLS SQL returns for that instant (UTC+8, no offset).
	for _, v := range []string{"1700000000", "1700000000000", "1700000000000000000", "2023-11-14T22:13:20Z", "2023-11-15 06:13:20.000", "2023-11-15 06:13:20"} {
		got, err := parseTimeValue(v, shanghai)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseTimeValue(%q) = %v, %v", v, got, err)
		}
	}
	if _, err := parseTimeValue("yesterday", shanghai); err == nil {
		t.Error("expected error")
	}
}

func TestLogsFrameIDAndLevel(t *testing.T) {
	logs := []map[string]string{
		{"__time__": "1700000000", "__tag__:__pack_id__": "P-1", "__pack_meta__": "0|abc|9|8", "severity": "ERROR", "msg": "a", "id": "biz-1"},
		{"__time__": "1700000000", "severity": "WARN", "msg": "same"},
		{"__time__": "1700000000", "severity": "WARN", "msg": "same"},
	}
	f := buildLogsFrame(logs, nil, testViewLevel("severity"))
	ids := field(t, f, "id")
	if ids.At(0) != "P-1/0|abc|9|8" {
		t.Errorf("id from pack = %v", ids.At(0))
	}
	if ids.At(1) == ids.At(2) {
		t.Error("identical logs must keep distinct ids")
	}
	if got := field(t, f, "detected_level").At(0); got != "ERROR" {
		t.Errorf("detected_level = %v", got)
	}
	// The log's own "id" field stays in the line and does not clash with the row id.
	if got := field(t, f, "message").At(0).(string); got != `id="biz-1" msg="a"` {
		t.Errorf("message = %q", got)
	}
	// With the default level field no copy is added.
	if g := buildLogsFrame(logs[:1], nil, testView(nil, nil, true)); len(fieldNames(g)) == len(fieldNames(f)) {
		for _, n := range fieldNames(g) {
			if n == "detected_level" {
				t.Error("detected_level must only be added for a non-default level field")
			}
		}
	}
}

func TestBuildVolumeFrames(t *testing.T) {
	rows := []map[string]string{
		{"time": "1700000060", "level": "ERROR", "count": "2"},
		{"time": "1700000000", "level": "ERROR", "count": "1"},
		{"time": "1700000000", "level": "info", "count": "9"},
		{"time": "1700000000", "level": "null", "count": "3"},
	}
	frames, err := buildVolumeFrames(rows, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]float64{}
	for _, f := range frames {
		vf := f.Fields[1]
		var vals []float64
		for i := 0; i < vf.Len(); i++ {
			vals = append(vals, vf.At(i).(float64))
		}
		got[vf.Labels["level"]] = vals
	}
	want := map[string][]float64{"error": {1, 2}, "info": {9}, "unknown": {3}}
	for l, w := range want {
		if fmt.Sprint(got[l]) != fmt.Sprint(w) {
			t.Errorf("level %s = %v, want %v (sorted by time)", l, got[l], w)
		}
	}
}

// testView compiles a logs config: rules nil means the defaults.
func testView(rules []fieldRule, lineFields []string, lineRest bool) *logsView {
	cfg := logsConfig{}
	if rules != nil {
		cfg.Rules = &rules
	}
	cfg.Line.Fields, cfg.Line.Rest = lineFields, &lineRest
	v := compileLogsView(cfg)
	if len(v.problems) > 0 {
		panic(v.problems)
	}
	return &v
}

func testViewLevel(level string) *logsView {
	v := compileLogsView(logsConfig{LevelField: level})
	return &v
}

func TestLogsFrameFieldRules(t *testing.T) {
	logs := []map[string]string{
		{"__time__": "1700000000", "__tag__:__hostname__": "web-1", "__tag__:__host_ip__": "10.0.0.1",
			"__tag__:__path__": "/logs/topup.log", "__tag__:__pack_id__": "P", "__pack_meta__": "0|x|1|1", "__topic__": "", "msg": "a"},
		// The application's own host.name wins over the collector tag.
		{"__time__": "1700000001", "__tag__:__hostname__": "web-2", "host.name": "app-host", "msg": "b"},
	}
	rules := append(append([]fieldRule{}, defaultFieldRules...),
		fieldRule{Type: "rename", From: "__tag__:__hostname__", To: "host.name"},
		fieldRule{Type: "rename", From: "__tag__:__host_ip__", To: "host.ip"},
		fieldRule{Type: "extract", From: "__tag__:__path__", To: "service.name", Pattern: `/logs/(.+)\.log`},
		fieldRule{Type: "drop", Field: "__topic__"},
	)
	f := buildLogsFrame(logs, nil, testView(rules, []string{"host.name", "host.ip", "missing"}, true))

	names := map[string]bool{}
	for _, n := range fieldNames(f) {
		names[n] = true
	}
	for _, n := range []string{"host.name", "host.ip", "service.name", "__tag__:__path__", "__tag__:__pack_id__"} {
		if !names[n] {
			t.Errorf("missing column %q in %v", n, fieldNames(f))
		}
	}
	for _, n := range []string{"__tag__:__hostname__", "__tag__:__host_ip__", "__topic__", "__time__"} {
		if names[n] {
			t.Errorf("column %q should not exist", n)
		}
	}
	if got := field(t, f, "host.name"); got.At(0) != "web-1" || got.At(1) != "app-host" {
		t.Errorf("host.name = %v, %v", got.At(0), got.At(1))
	}
	// lineFields open the line in order (missing ones skipped); service.name is a rule output that
	// is not listed, so it stays in details only.
	if got := field(t, f, "message").At(0); got != `host.name="web-1" host.ip="10.0.0.1" msg="a"` {
		t.Errorf("line = %q", got)
	}
	if got := field(t, f, "id").At(0); got != "P/0|x|1|1" {
		t.Errorf("id = %q", got)
	}

	// lineRest off: only the listed fields.
	only := buildLogsFrame(logs, nil, testView(rules, []string{"msg", "host.name"}, false))
	if got := field(t, only, "message").At(0); got != `msg="a" host.name="web-1"` {
		t.Errorf("line without rest = %q", got)
	}
}

func TestDefaultRulesBuildTheTimestamp(t *testing.T) {
	logs := []map[string]string{
		{"__time__": "1700000000", "__time_ns_part__": "123456789", "msg": "a"},
		{"__time__": "1700000001", "msg": "no nanoseconds from this collector"},
	}
	f := buildLogsFrame(logs, nil, testView(nil, nil, true))
	for _, n := range fieldNames(f) {
		if n == "__time__" || n == "__time_ns_part__" || n == "timestamp" {
			t.Errorf("%s should not be a column with the default rules", n)
		}
	}
	tf := field(t, f, "time")
	if !tf.At(0).(time.Time).Equal(time.Unix(1700000000, 123456789)) || !tf.At(1).(time.Time).Equal(time.Unix(1700000001, 0)) {
		t.Errorf("times = %v, %v", tf.At(0), tf.At(1))
	}

	// No rules at all: the raw fields stay and the row time falls back to __time__ (seconds).
	none := buildLogsFrame(logs, nil, testView([]fieldRule{}, nil, true))
	if got := field(t, none, "time").At(0).(time.Time); !got.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("fallback time = %v", got)
	}
	field(t, none, "__time_ns_part__")

	// A time rule can also write a visible field, e.g. a normalised timestamp for the line.
	visible := buildLogsFrame(logs, nil, testView(append(append([]fieldRule{}, defaultFieldRules[0]),
		fieldRule{Type: "time", From: "__time__", Nanos: "__time_ns_part__", To: "log.time"}), []string{"log.time"}, false))
	if got := field(t, visible, "message").At(0); got != `log.time="2023-11-14T22:13:20.123456789Z"` {
		t.Errorf("line = %q", got)
	}
}

func TestLogsLineKeepsSLSOrder(t *testing.T) {
	logs := []map[string]string{{
		"__time__": "1700000000", "level": "INFO", "msg": "paid", "service_name": "pay", "order_id": "TP1",
		"__tag__:__hostname__": "web-1", "svc_host": "h", "host.name": "app-host",
	}}
	keys := [][]string{{"level", "msg", "service_name", "order_id", "svc_host", "host.name", "__tag__:__hostname__", "__time__"}}
	rules := append(append([]fieldRule{}, defaultFieldRules...),
		fieldRule{Type: "rename", From: "__tag__:__hostname__", To: "host.name"},
		fieldRule{Type: "rename", From: "svc_host", To: "service.host"},
		fieldRule{Type: "rename", From: "order_id", To: "service_name"}, // onto an own field: shown once
	)
	f := buildLogsFrame(logs, keys, testView(rules, []string{"host.name"}, true))
	want := `host.name="app-host" msg="paid" service_name="pay" service.host="h"`
	if got := field(t, f, "message").At(0).(string); got != want {
		t.Errorf("line = %q\nwant   %q", got, want)
	}
	// Listing the level field puts it back.
	f = buildLogsFrame(logs, keys, testView(rules, []string{"level"}, false))
	if got := field(t, f, "message").At(0).(string); got != `level="INFO"` {
		t.Errorf("line = %q", got)
	}
}

func TestDecodeObject(t *testing.T) {
	m, keys, err := decodeObject([]byte(` { "msg" : "a \"q\" \u4e2d" ,"b":"x","n":12,"z":null, "b":"y"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(keys) != "[msg b n z]" || m["msg"] != `a "q" 中` || m["b"] != "y" || m["n"] != "12" || m["z"] != "" {
		t.Errorf("keys %v map %v", keys, m)
	}
	if _, _, err := decodeObject([]byte(`{"a":"b"`), nil); err == nil {
		t.Error("truncated object must fail")
	}
	if m, keys, err := decodeObject([]byte(`{}`), nil); err != nil || len(m) != 0 || len(keys) != 0 {
		t.Errorf("empty: %v %v %v", m, keys, err)
	}
}

func TestDecodeRowsSharesKeys(t *testing.T) {
	raw := []json.RawMessage{[]byte(`{"a":"1","b":"2"}`), []byte(`{"a":"3","b":"4"}`), []byte(`{"a":"5","c":"6"}`)}
	logs, keys, err := decodeRows(raw)
	if err != nil {
		t.Fatal(err)
	}
	if &keys[0][0] != &keys[1][0] || &keys[1][0] == &keys[2][0] {
		t.Error("identical key lists must share one slice, different ones must not")
	}
	if fmt.Sprint(keys[2]) != "[a c]" || logs[2]["c"] != "6" || logs[1]["b"] != "4" {
		t.Errorf("%v %v", keys, logs)
	}
}

func TestLogsLineExclude(t *testing.T) {
	logs := []map[string]string{{"__time__": "1700000000", "msg": "paid", "source": "a.go:1", "order_id": "TP1"}}
	keys := [][]string{{"msg", "source", "order_id", "__time__"}}
	rest := true
	cfg := logsConfig{}
	cfg.Line.Fields, cfg.Line.Rest, cfg.Line.Exclude = []string{"msg", "source"}, &rest, []string{"source"}
	v := compileLogsView(cfg)
	f := buildLogsFrame(logs, keys, &v)
	if got := field(t, f, "message").At(0).(string); got != `msg="paid" order_id="TP1"` {
		t.Errorf("line = %q", got)
	}
	if field(t, f, "source").At(0) != "a.go:1" {
		t.Error("excluded fields stay as columns")
	}
}

func TestLineValues(t *testing.T) {
	cases := map[string]string{
		`plain`:                                `"plain"`,
		`say "hi" \ bye`:                       `"say \"hi\" \\ bye"`,
		`{"method":"POST","header":{"a":"b"}}`: `{"method":"POST","header":{"a":"b"}}`,
		`["a","b"]`:                            `["a","b"]`,
		`{not json}`:                           `"{not json}"`,
	}
	for in, want := range cases {
		if got := string(appendValue(nil, in)); got != want {
			t.Errorf("appendValue(%s) = %s, want %s", in, got, want)
		}
	}
}
