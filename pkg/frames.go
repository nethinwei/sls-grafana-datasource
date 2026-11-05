package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// ---------- logs ----------

// buildLogsFrame turns search and SPL results into a frame for the Logs panel: a nanosecond
// timestamp, a readable line, an id, and every field as its own column (so field overrides such as
// data links keep working). Grafana takes the severity from a field named "level"; when the
// configured level field has another name it is copied to "detected_level".
func buildLogsFrame(logs []map[string]string, levelField string) *data.Frame {
	var keys []string
	seen := map[string]bool{}
	for _, l := range logs {
		for k := range l {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	// Map iteration order is random; sort so columns and lines keep the same order across refreshes.
	sort.Strings(keys)

	times := make([]time.Time, len(logs))
	lines := make([]string, len(logs))
	ids := make([]string, len(logs))
	columns := make(map[string][]string, len(keys))
	for _, k := range keys {
		columns[k] = make([]string, len(logs))
	}
	for i, l := range logs {
		times[i] = logTime(l)
		lines[i] = logLine(l, keys)
		ids[i] = logID(l, times[i], lines[i], i)
		for k, v := range l {
			columns[k][i] = v
		}
	}

	frame := data.NewFrame("logs",
		data.NewField("time", nil, times),
		data.NewField("message", nil, lines),
		data.NewField("id", nil, ids),
	)
	for _, k := range keys {
		if k == "id" {
			continue // the log's own "id" field would collide with the row id; it is part of the line
		}
		frame.Fields = append(frame.Fields, data.NewField(k, nil, columns[k]))
	}
	if levelField != "level" && seen[levelField] {
		frame.Fields = append(frame.Fields, data.NewField("detected_level", nil, columns[levelField]))
	}
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeLogs}
	return frame
}

// logTime combines __time__ (seconds) with __time_ns_part__, which SLS returns when the collector
// sends nanosecond timestamps.
func logTime(l map[string]string) time.Time {
	sec, _ := strconv.ParseInt(l["__time__"], 10, 64)
	ns, _ := strconv.ParseInt(l["__time_ns_part__"], 10, 64)
	return time.Unix(sec, ns)
}

// logID identifies a log row for Grafana (deduplication, permalinks): the pack ID plus the position
// inside the pack, which SLS returns with `| with_pack_meta`. Without it, a hash of time and line
// suffixed with the row index, so identical logs are not merged.
func logID(l map[string]string, t time.Time, line string, row int) string {
	if pack, meta := l["__tag__:__pack_id__"], l["__pack_meta__"]; pack != "" && meta != "" {
		return pack + "/" + meta
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(strconv.FormatInt(t.UnixNano(), 10)))
	_, _ = h.Write([]byte(line))
	return strconv.FormatUint(h.Sum64(), 36) + "_" + strconv.Itoa(row)
}

// logLine renders `key="value"` pairs for the keys present in this log, leaving out SLS reserved and
// meta fields (__time__, __source__, __topic__, __tag__:*, ...). They remain separate columns.
func logLine(l map[string]string, keys []string) string {
	var b strings.Builder
	for _, k := range keys {
		v, ok := l[k]
		if !ok || strings.HasPrefix(k, "__") {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(strings.ReplaceAll(v, `"`, `'`))
		b.WriteByte('"')
	}
	return b.String()
}

// ---------- SQL ----------

type columnKind int

const (
	kindString columnKind = iota
	kindNumber
	kindBool
	kindTime
)

// buildSQLFrame turns SQL results into a typed frame. keys gives the column order and columnTypes
// (from the GetLogs meta) the SLS types; types that are absent or unknown are inferred from values.
//
// When the result has a time column it becomes a time series: rows are sorted by time and, if there
// are string columns, converted from long to wide so that every distinct combination of string
// values becomes its own series (what plugin 2.x expressed with ycol "group#:#value").
func buildSQLFrame(keys, columnTypes []string, rows []map[string]string, timeColumn string, loc *time.Location) (*data.Frame, error) {
	if len(keys) == 0 && len(rows) > 0 {
		for k := range rows[0] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}

	frame := data.NewFrame("")
	timeIdx := -1
	for i, k := range keys {
		values := make([]string, len(rows))
		for r, row := range rows {
			values[r] = row[k]
		}
		typeName := ""
		if i < len(columnTypes) {
			typeName = columnTypes[i]
		}
		kind := inferKind(typeName, values)
		if k == timeColumn && (kind == kindNumber || kind == kindTime || kind == kindString) {
			field, err := timeField(k, values, loc)
			if err != nil {
				return nil, err
			}
			frame.Fields = append(frame.Fields, field)
			timeIdx = len(frame.Fields) - 1
			continue
		}
		frame.Fields = append(frame.Fields, typedField(k, kind, values, loc))
	}

	if timeIdx < 0 {
		return frame, nil
	}
	return toTimeSeries(frame, timeIdx)
}

func toTimeSeries(frame *data.Frame, timeIdx int) (*data.Frame, error) {
	// Time first, then sort ascending: both are required by long-to-wide and by the time series panel.
	fields := make([]*data.Field, 0, len(frame.Fields))
	fields = append(fields, frame.Fields[timeIdx])
	for i, f := range frame.Fields {
		if i != timeIdx {
			fields = append(fields, f)
		}
	}
	frame.Fields = fields
	sortByTime(frame)

	hasString := false
	for _, f := range frame.Fields[1:] {
		switch f.Type() {
		case data.FieldTypeNullableString:
			hasString = true
		case data.FieldTypeNullableBool:
			// long-to-wide treats bool as a dimension too; keep it as a label value.
			hasString = true
		}
	}
	if !hasString {
		frame.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesWide}
		return frame, nil
	}
	wide, err := data.LongToWide(frame, nil)
	if err != nil {
		return nil, userError("cannot turn the result into time series (one row per time and string-column combination is required): %v", err)
	}
	wide.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesWide}
	return wide, nil
}

func sortByTime(frame *data.Frame) {
	n, _ := frame.RowLen()
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	tf := frame.Fields[0]
	sort.SliceStable(idx, func(a, b int) bool {
		ta, _ := tf.ConcreteAt(idx[a])
		tb, _ := tf.ConcreteAt(idx[b])
		return ta.(time.Time).Before(tb.(time.Time))
	})
	for _, f := range frame.Fields {
		vals := make([]interface{}, n)
		for i, j := range idx {
			vals[i] = f.At(j)
		}
		for i, v := range vals {
			f.Set(i, v)
		}
	}
}

// inferKind maps an SLS SQL column type to a field kind; when the type is missing or unknown it
// looks at the values: all non-null values numeric → number, all true/false → bool.
func inferKind(typeName string, values []string) columnKind {
	t := strings.ToLower(typeName)
	switch {
	case strings.Contains(t, "int"), strings.Contains(t, "double"), strings.Contains(t, "float"),
		strings.Contains(t, "real"), strings.Contains(t, "decimal"), t == "long":
		return kindNumber
	case t == "boolean" || t == "bool":
		return kindBool
	case strings.Contains(t, "timestamp"):
		return kindTime
	case t != "" && t != "unknown":
		return kindString
	}

	allNum, allBool, nonNull := true, true, false
	for _, v := range values {
		if isNull(v) {
			continue
		}
		nonNull = true
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			allNum = false
		}
		if v != "true" && v != "false" {
			allBool = false
		}
	}
	switch {
	case nonNull && allNum:
		return kindNumber
	case nonNull && allBool:
		return kindBool
	}
	return kindString
}

func isNull(v string) bool { return v == "" || v == "null" }

func typedField(name string, kind columnKind, values []string, loc *time.Location) *data.Field {
	switch kind {
	case kindNumber:
		out := make([]*float64, len(values))
		for i, v := range values {
			if f, err := strconv.ParseFloat(v, 64); err == nil && !isNull(v) && !math.IsNaN(f) {
				out[i] = &f
			}
		}
		return data.NewField(name, nil, out)
	case kindBool:
		out := make([]*bool, len(values))
		for i, v := range values {
			if !isNull(v) {
				b := v == "true"
				out[i] = &b
			}
		}
		return data.NewField(name, nil, out)
	case kindTime:
		out := make([]*time.Time, len(values))
		for i, v := range values {
			if t, err := parseTimeValue(v, loc); err == nil {
				out[i] = &t
			}
		}
		return data.NewField(name, nil, out)
	default:
		out := make([]*string, len(values))
		for i, v := range values {
			if !isNull(v) {
				s := v
				out[i] = &s
			}
		}
		return data.NewField(name, nil, out)
	}
}

func timeField(name string, values []string, loc *time.Location) (*data.Field, error) {
	out := make([]time.Time, len(values))
	for i, v := range values {
		t, err := parseTimeValue(v, loc)
		if err != nil {
			return nil, userError("time column %q: %v", name, err)
		}
		out[i] = t
	}
	return data.NewField(name, nil, out), nil
}

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}

// parseTimeValue accepts unix seconds, milliseconds or nanoseconds (decided by magnitude) and common
// date-time strings. Strings without an offset are read in loc, the SLS SQL time zone (UTC+8 by
// default). Numeric time such as __time__ or $__timeGroup(__time__) has no such ambiguity.
func parseTimeValue(v string, loc *time.Location) (time.Time, error) {
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		switch abs := math.Abs(f); {
		case abs < 1e11:
			sec, frac := math.Modf(f)
			return time.Unix(int64(sec), int64(frac*1e9)), nil
		case abs < 1e14:
			return time.UnixMilli(int64(f)), nil
		default:
			return time.Unix(0, int64(f)), nil
		}
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q as time", v)
}

// ---------- logs volume ----------

// buildVolumeFrames turns `time, level, count` SQL rows into one series per level, the shape the
// logs volume histogram expects: a time field and a number field labelled with the level.
func buildVolumeFrames(rows []map[string]string, loc *time.Location) (data.Frames, error) {
	type series struct {
		times  []time.Time
		counts []float64
	}
	byLevel := map[string]*series{}
	for _, r := range rows {
		t, err := parseTimeValue(r["time"], loc)
		if err != nil {
			return nil, err
		}
		c, _ := strconv.ParseFloat(r["count"], 64)
		level := strings.ToLower(r["level"])
		if isNull(level) {
			level = "unknown"
		}
		s := byLevel[level]
		if s == nil {
			s = &series{}
			byLevel[level] = s
		}
		s.times = append(s.times, t)
		s.counts = append(s.counts, c)
	}
	levels := make([]string, 0, len(byLevel))
	for l := range byLevel {
		levels = append(levels, l)
	}
	sort.Strings(levels)
	frames := make(data.Frames, 0, len(levels))
	for _, l := range levels {
		s := byLevel[l]
		f := data.NewFrame("",
			data.NewField("time", nil, s.times),
			data.NewField("count", data.Labels{"level": l}, s.counts),
		)
		sortByTime(f)
		f.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti}
		frames = append(frames, f)
	}
	return frames, nil
}
