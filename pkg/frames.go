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

// buildLogsFrame turns search and SPL results into a frame for the Logs panel: the row timestamp,
// a readable line, an id, and every field as its own column (so field overrides such as data
// links keep working). The field rules run first, column by column.
//
// keys gives each row's fields in SLS order (nil when unknown: sorted), which the line follows.
//
// Grafana takes the severity from a field named "level"; when the configured level field has
// another name it is copied to "detected_level".
func buildLogsFrame(logs []map[string]string, keys [][]string, f *logsView) *data.Frame {
	n := len(logs)
	t, times := f.table(logs)

	names := make([]string, 0, len(t.cols))
	for k := range t.cols {
		names = append(names, k)
	}
	// Map iteration order is random; sort so columns keep the same order across refreshes.
	sort.Strings(names)

	// The line: the configured line fields first, then (rest) the log's own fields in the order
	// they were written. Meta fields (__*) and fields produced by rules join only when listed; the
	// level field is shown by Grafana as the level, so it is left out unless listed.
	listed := make(map[string]bool, len(f.lineFields))
	var fixed []lineColumn
	for _, k := range f.lineFields {
		if c := t.cols[k]; c != nil && !listed[k] {
			listed[k] = true
			fixed = append(fixed, lineColumn{name: k, col: c})
		}
	}
	// rest resolves an SLS key to the column shown for it, once per key; nil when it stays out.
	// Keys showing the same column (a rename onto an own field) share one entry.
	rest := map[string]*lineColumn{}
	byName := map[string]*lineColumn{}
	resolve := func(key string) *lineColumn {
		lc, ok := rest[key]
		if ok {
			return lc
		}
		name := key
		if shown, renamed := f.shownNames[key]; renamed {
			name = shown
		}
		if c := t.cols[name]; c != nil && !strings.HasPrefix(key, "__") && !listed[name] && name != f.levelField {
			if lc = byName[name]; lc == nil {
				lc = &lineColumn{name: name, col: c, row: -1}
				byName[name] = lc
			}
		}
		rest[key] = lc
		return lc
	}

	stamps := rowTimestamps(logs, t, times, f.timeField)
	lines := make([]string, n)
	ids := make([]string, n)
	pack, meta := t.cols["__tag__:__pack_id__"], t.cols["__pack_meta__"]
	// One scratch buffer for all rows: each line costs exactly one allocation (the string).
	buf := make([]byte, 0, 512)
	var lastKeys []string
	var lastCols []*lineColumn
	for i := 0; i < n; i++ {
		buf = buf[:0]
		for _, lc := range fixed {
			buf = lc.append(buf, i)
		}
		if f.lineRest {
			var rowKeys []string
			if keys != nil && keys[i] != nil {
				rowKeys = keys[i]
			} else {
				rowKeys = sortedKeys(logs[i])
			}
			// Rows sharing a key slice (see decodeObject) resolve it once.
			if len(rowKeys) == 0 || len(lastKeys) != len(rowKeys) || &lastKeys[0] != &rowKeys[0] {
				lastKeys, lastCols = rowKeys, lastCols[:0]
				for _, k := range rowKeys {
					if lc := resolve(k); lc != nil {
						lastCols = append(lastCols, lc)
					}
				}
			}
			for _, lc := range lastCols {
				if lc.row != i {
					lc.row = i
					buf = lc.append(buf, i)
				}
			}
		}
		lines[i] = string(buf)
		if pack != nil && meta != nil && pack.present[i] && meta.present[i] {
			ids[i] = pack.values[i] + "/" + meta.values[i]
		} else {
			ids[i] = hashID(stamps[i], lines[i], i)
		}
	}

	frame := data.NewFrame("logs",
		data.NewField("time", nil, stamps),
		data.NewField("message", nil, lines),
		data.NewField("id", nil, ids),
	)
	for _, k := range names {
		if k == "id" {
			continue // the log's own "id" field would collide with the row id; it is part of the line
		}
		frame.Fields = append(frame.Fields, data.NewField(k, nil, t.cols[k].values))
	}
	if f.levelField != "level" {
		if c := t.cols[f.levelField]; c != nil {
			frame.Fields = append(frame.Fields, data.NewField("detected_level", nil, c.values))
		}
	}
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeLogs}
	return frame
}

type lineColumn struct {
	name string
	col  *column
	row  int // last row this column was written to
}

// append writes ` name="value"` (without the leading space on the first pair) when the row has a
// value.
func (lc *lineColumn) append(buf []byte, i int) []byte {
	if !lc.col.present[i] {
		return buf
	}
	if len(buf) > 0 {
		buf = append(buf, ' ')
	}
	buf = append(buf, lc.name...)
	buf = append(buf, '=', '"')
	buf = appendQuoted(buf, lc.col.values[i])
	return append(buf, '"')
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// rowTimestamps returns the timestamp of each row: from the time rule that wrote the time field,
// else from a time field the row has, else (no rule configured it) SLS's __time__ in seconds.
func rowTimestamps(logs []map[string]string, t *table, times rowTimes, timeField string) []time.Time {
	out := make([]time.Time, t.n)
	col := t.cols[timeField]
	for i := range out {
		switch {
		case times.set != nil && times.set[i]:
			out[i] = times.t[i]
			continue
		case col != nil && col.present[i]:
			if ts, err := parseTimeValue(col.values[i], time.UTC); err == nil {
				out[i] = ts
				continue
			}
		}
		if sec, err := strconv.ParseInt(logs[i]["__time__"], 10, 64); err == nil {
			out[i] = time.Unix(sec, 0)
		}
	}
	return out
}

// appendQuoted appends v with double quotes replaced, so the line stays parseable as key="value".
func appendQuoted(buf []byte, v string) []byte {
	if strings.IndexByte(v, '"') < 0 {
		return append(buf, v...)
	}
	for i := 0; i < len(v); i++ {
		if v[i] == '"' {
			buf = append(buf, '\'')
		} else {
			buf = append(buf, v[i])
		}
	}
	return buf
}

// hashID identifies a row without pack data (deduplication, permalinks): a hash of time and line
// suffixed with the row index, so identical logs are not merged.
func hashID(t time.Time, line string, row int) string {
	h := fnv.New64a()
	var buf [8]byte
	ns := t.UnixNano()
	for i := range buf {
		buf[i] = byte(ns >> (8 * i))
	}
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte(line))
	return strconv.FormatUint(h.Sum64(), 36) + "_" + strconv.Itoa(row)
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
	// Integers first: fast, and exact for 19-digit nanosecond values that float64 cannot hold.
	if i, err := strconv.ParseInt(v, 10, 64); err == nil {
		switch abs := max(i, -i); {
		case abs < 1e11:
			return time.Unix(i, 0), nil
		case abs < 1e14:
			return time.UnixMilli(i), nil
		case abs < 1e17:
			return time.UnixMicro(i), nil
		default:
			return time.Unix(0, i), nil
		}
	}
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
