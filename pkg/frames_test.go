package main

import (
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
	f := buildLogsFrame(logs, "level")

	if f.Meta.PreferredVisualization != data.VisTypeLogs {
		t.Errorf("preferred visualisation = %q", f.Meta.PreferredVisualization)
	}
	if got := field(t, f, "time").At(0).(time.Time); !got.Equal(time.Unix(1700000000, 123456789)) {
		t.Errorf("time = %v, want nanosecond precision", got)
	}
	want := []string{`code="12001" level="ERROR" msg="pay 'failed'"`, `channel="coda" error="" level="WARN" msg="slow"`}
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
	again := buildLogsFrame(logs, "level")
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
	f, err := buildSQLFrame([]string{"service", "c", "ratio", "ok"}, nil, rows, "time", time.UTC)
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
	f, err := buildSQLFrame([]string{"code"}, []string{"varchar"}, rows, "time", time.UTC)
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
	f, err := buildSQLFrame([]string{"time", "service", "c"}, []string{"bigint", "varchar", "bigint"}, rows, "time", time.UTC)
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
	f, err := buildSQLFrame([]string{"t", "c"}, nil, rows, "t", time.UTC)
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
	f := buildLogsFrame(logs, "severity")
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
	if got := field(t, f, "message").At(0).(string); got != `id="biz-1" msg="a" severity="ERROR"` {
		t.Errorf("message = %q", got)
	}
	// With the default level field no copy is added.
	if g := buildLogsFrame(logs[:1], "level"); len(fieldNames(g)) == len(fieldNames(f)) {
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
