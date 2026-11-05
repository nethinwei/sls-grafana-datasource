package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestWideTableLimit(t *testing.T) {
	// 100 distinct seconds × 100 distinct request ids × 1 value column = 10000 cells.
	var rows []map[string]string
	for i := 0; i < 100; i++ {
		rows = append(rows, map[string]string{"time": fmt.Sprint(1700000000 + i), "request_id": fmt.Sprint("r", i), "latency": "1"})
	}
	keys, types := []string{"time", "request_id", "latency"}, []string{"long", "varchar", "long"}
	if _, err := buildSQLFrame(keys, types, rows, "time", time.UTC, 9999); err == nil || !strings.Contains(err.Error(), "10000 time series cells") {
		t.Fatalf("err = %v", err)
	}
	if _, err := buildSQLFrame(keys, types, rows, "time", time.UTC, 10000); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
}

func TestJSONFlattenLimit(t *testing.T) {
	logs := make([]map[string]string, 50)
	for i := range logs {
		// Objects keyed by IDs: 5 new keys per row.
		logs[i] = map[string]string{"__time__": "1700000000", "msg": "m",
			"counts": fmt.Sprintf(`{"id%d":1,"id%d":2,"id%d":3,"id%d":4,"id%d":5}`, i*5, i*5+1, i*5+2, i*5+3, i*5+4)}
	}
	v := compileLogsView(logsConfig{})
	v.maxFlattened = 20
	f := buildLogsFrame(logs, nil, &v)
	flattened := 0
	for _, fl := range f.Fields {
		if strings.HasPrefix(fl.Name, "counts_") {
			flattened++
		}
	}
	if flattened != 20 {
		t.Errorf("flattened %d fields, want 20", flattened)
	}
	if len(f.Meta.Notices) != 1 || f.Meta.Notices[0].Severity != data.NoticeSeverityInfo {
		t.Errorf("notices = %v", f.Meta.Notices)
	}
}

func TestJSONNameClashIsDeterministic(t *testing.T) {
	for i := 0; i < 20; i++ {
		got := applyRow(t, defaultFieldRules, map[string]string{
			"__time__": "1700000000", "labels": `{"x-env":"a","x_env":"b"}`})
		if got["labels_x_env"] != "a" { // "x-env" sorts first and owns the name
			t.Fatalf("labels_x_env = %q", got["labels_x_env"])
		}
	}
}

func TestLearnedFieldsPerLogstore(t *testing.T) {
	ds := &datasource{}
	f := data.NewFrame("logs")
	f.Meta = &data.FrameMeta{Custom: map[string]any{fieldExprsKey: map[string]string{"http_method": "e"}}}
	ds.learnFields("a", f)
	if e, ok := ds.learnedFields("a")("http_method"); !ok || e != "e" {
		t.Errorf("a: %q %v", e, ok)
	}
	if _, ok := ds.learnedFields("b")("http_method"); ok {
		t.Error("fields learned on logstore a must not apply to b")
	}
}

func TestBigIntegersKeepTheirDigits(t *testing.T) {
	rows := []map[string]string{{"order_id": "1234567890123456789", "small": "42", "ratio": "0.5"}, {"order_id": "7", "small": "9007199254740992", "ratio": "1"}}
	f, err := buildSQLFrame([]string{"order_id", "small", "ratio"}, []string{"bigint", "long", "double"}, rows, "", time.UTC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Fields[0].At(0); got == nil || *got.(*string) != "1234567890123456789" {
		t.Errorf("order_id = %v, want the exact digits as a string", got)
	}
	if v := f.Fields[1].At(1); v == nil || *v.(*float64) != 9007199254740992 { // 2^53 itself is exact
		t.Errorf("small = %v", v)
	}
	if f.Fields[2].Type() != data.FieldTypeNullableFloat64 {
		t.Errorf("ratio type = %s", f.Fields[2].Type())
	}
}

func TestTimeColumnFormatsAndBadRows(t *testing.T) {
	for _, v := range []string{"2024/01/02 03:04:05", "2024-01-02T03:04:05", "2024-01-02 03:04:05.123+08:00", "1704164645123456.0"} {
		if _, err := parseTimeValue(v, time.UTC); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
	if got, _ := parseTimeValue("1704164645123456.0", time.UTC); got.Year() != 2024 {
		t.Errorf("float microseconds read as %v", got)
	}
	rows := []map[string]string{{"time": "1700000000", "c": "1"}, {"time": "null", "c": "2"}, {"time": "1700000060", "c": "3"}}
	f, err := buildSQLFrame([]string{"time", "c"}, []string{"long", "long"}, rows, "time", time.UTC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := f.RowLen(); n != 2 || len(f.Meta.Notices) != 1 {
		t.Errorf("rows=%d notices=%v, want the bad row left out with a notice", n, f.Meta.Notices)
	}
}
