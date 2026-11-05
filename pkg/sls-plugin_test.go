package main

import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func buildLogsFrame(t *testing.T, logs []map[string]string, ycols []string) *data.Frame {
	t.Helper()
	var frames data.Frames
	(&SlsDatasource{}).BuildLogs(logs, ycols, &frames)
	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}
	return frames[0]
}

func fieldValues(t *testing.T, frame *data.Frame, name string) []string {
	t.Helper()
	var field *data.Field
	for _, f := range frame.Fields {
		if f.Name == name {
			field = f
		}
	}
	if field == nil {
		t.Fatalf("field %q missing", name)
	}
	values := make([]string, field.Len())
	for i := range values {
		values[i] = field.At(i).(string)
	}
	return values
}

func TestBuildLogsMessageOnlyHasOwnNonMetaFields(t *testing.T) {
	logs := []map[string]string{
		{
			"__time__": "1700000000", "__source__": "10.0.0.1", "__topic__": "",
			"__tag__:__hostname__": "host-a", "level": "ERROR", "msg": "pay failed", "code": "12001",
		},
		{"__time__": "1700000001", "level": "WARN", "msg": "slow", "channel": "coda", "error": ""},
	}
	frame := buildLogsFrame(t, logs, []string{""})

	got := fieldValues(t, frame, "message")
	want := []string{
		`code="12001" level="ERROR" msg="pay failed" `,
		`channel="coda" error="" level="WARN" msg="slow" `,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Meta fields stay available as separate fields for log details.
	if host := fieldValues(t, frame, "__tag__:__hostname__"); host[0] != "host-a" || host[1] != "" {
		t.Errorf("__tag__:__hostname__ = %q", host)
	}
	if _, err := frame.RowLen(); err != nil {
		t.Errorf("frame has mismatched field lengths: %v", err)
	}
}

func TestBuildLogsFieldOrderIsStable(t *testing.T) {
	logs := []map[string]string{{"__time__": "1700000000", "z": "1", "a": "2", "m": "3"}}
	for i := 0; i < 20; i++ {
		frame := buildLogsFrame(t, logs, []string{""})
		var names []string
		for _, f := range frame.Fields[2:] { // skip time, message
			names = append(names, f.Name)
		}
		want := []string{"__time__", "a", "m", "z"}
		for j := range want {
			if names[j] != want[j] {
				t.Fatalf("field order = %v, want %v", names, want)
			}
		}
	}
}

func TestBuildLogsWithYcolsKeepsChosenColumns(t *testing.T) {
	logs := []map[string]string{
		{"__time__": "1700000000", "__tag__:__path__": "/logs/a.log", "msg": "hello"},
		{"__time__": "1700000001", "msg": "world"},
	}
	frame := buildLogsFrame(t, logs, []string{"__tag__:__path__", "msg"})

	got := fieldValues(t, frame, "message")
	want := []string{`__tag__:__path__="/logs/a.log" msg="hello" `, `msg="world" `}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
