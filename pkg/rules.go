package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Field rules reshape log rows for display. They run in order on each query result, column by
// column, and only change what Grafana shows; SLS data is untouched.
//
//	time      __time__ + __time_ns_part__ → timestamp   (the row timestamp, see defaultFieldRules)
//	drop      __time__
//	rename    __tag__:__hostname__ → host.name
//	extract   __tag__:__path__ /logs/(.+)\.log → service.name ($1)
//	default   deployment.environment = prod (when absent or empty)
//	template  ${service}/${env} → app
//
// Adding a rule type: write a constructor returning a rule and register it in ruleTypes. apply
// changes the rows; describe tells the field catalog what the rule removes, renames or produces,
// from which the line, completion, field browser and filters follow without further changes.
type fieldRule struct {
	Type  string `json:"type"`
	From  string `json:"from"`  // source field (time, rename, extract)
	To    string `json:"to"`    // target field (time, rename, extract, default, template)
	Field string `json:"field"` // field to hide (drop)
	// Pattern is a regular expression (extract).
	Pattern string `json:"pattern"`
	// Template builds the value: from the match for extract ($1 by default, ${name} for named
	// groups), from fields for template (${field}; $$ is a literal $).
	Template string `json:"template"`
	Value    string `json:"value"` // value (default)
	// Nanos holds the nanosecond part of a seconds timestamp, e.g. __time_ns_part__ (time). When a
	// log has no such field its nanoseconds are 0.
	Nanos string `json:"nanos"`
	// Format of a produced time field: rfc3339 (default, nanoseconds), unix_ms or unix_ns (time).
	// Ignored when the target is the time field, which becomes the row timestamp.
	Format string `json:"format"`
}

// rule is one compiled field rule.
type rule interface {
	apply(t *table, ctx *applyContext)
	describe(c *catalogBuilder)
}

// applyContext is shared by the rules of one run.
type applyContext struct {
	timeField string
	times     rowTimes // written by a time rule whose target is timeField
}

var ruleTypes = map[string]func(r fieldRule) (rule, error){
	"time":     newTimeRule,
	"rename":   newRenameRule,
	"extract":  newExtractRule,
	"drop":     newDropRule,
	"default":  newDefaultRule,
	"template": newTemplateRule,
}

// defaultTimeField is the field that becomes the row timestamp unless configured otherwise.
const defaultTimeField = "timestamp"

// defaultFieldRules apply when no rules are configured: build the row timestamp from SLS's seconds
// and (when the collector sends them) nanoseconds, and hide the raw fields.
var defaultFieldRules = []fieldRule{
	{Type: "time", From: "__time__", Nanos: "__time_ns_part__", To: defaultTimeField},
	{Type: "drop", Field: "__time__"},
	{Type: "drop", Field: "__time_ns_part__"},
}

// contextFields are needed by log context and must keep their SLS names.
var contextFields = map[string]bool{"__tag__:__pack_id__": true, "__pack_meta__": true}

// compileRules validates and compiles rules. Invalid rules are skipped and reported (health
// check), so one bad rule does not break queries.
func compileRules(rules []fieldRule) ([]rule, []string) {
	var out []rule
	var problems []string
	for i, r := range rules {
		r.From, r.To, r.Field, r.Nanos = strings.TrimSpace(r.From), strings.TrimSpace(r.To), strings.TrimSpace(r.Field), strings.TrimSpace(r.Nanos)
		ctor, ok := ruleTypes[r.Type]
		var compiled rule
		err := fmt.Errorf("unknown type")
		if ok {
			compiled, err = ctor(r)
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("rule %d (%s): %v", i+1, r.Type, err))
			continue
		}
		out = append(out, compiled)
	}
	return out, problems
}

func require(fields ...string) error {
	for _, f := range fields {
		if f == "" {
			return fmt.Errorf("missing required parameters")
		}
	}
	return nil
}

// ---------- rename ----------

type renameRule struct{ from, to string }

func newRenameRule(r fieldRule) (rule, error) {
	switch {
	case require(r.From, r.To) != nil || r.From == r.To:
		return nil, fmt.Errorf("from and a different to are required")
	case contextFields[r.From]:
		return nil, fmt.Errorf("%s is needed by log context and cannot be renamed", r.From)
	}
	return renameRule{r.From, r.To}, nil
}

func (r renameRule) apply(t *table, _ *applyContext) {
	src := t.cols[r.from]
	if src == nil {
		return
	}
	delete(t.cols, r.from)
	dst := t.cols[r.to]
	if dst == nil {
		t.cols[r.to] = src // O(1): the column changes its name
		return
	}
	// The log's own field under the target name wins (an application's own host.name is more
	// precise than the collector's).
	for i, ok := range src.present {
		if ok && !dst.present[i] {
			dst.values[i], dst.present[i] = src.values[i], true
		}
	}
}

func (r renameRule) describe(c *catalogBuilder) { c.rename(r.from, r.to) }

// ---------- drop ----------

type dropRule struct{ field string }

func newDropRule(r fieldRule) (rule, error) {
	switch {
	case r.Field == "":
		return nil, fmt.Errorf("field is required")
	case contextFields[r.Field]:
		return nil, fmt.Errorf("%s is needed by log context and cannot be dropped", r.Field)
	}
	return dropRule{r.Field}, nil
}

func (r dropRule) apply(t *table, _ *applyContext) { delete(t.cols, r.field) }
func (r dropRule) describe(c *catalogBuilder)      { c.remove(r.field) }

// ---------- default ----------

type defaultRule struct{ to, value string }

func newDefaultRule(r fieldRule) (rule, error) {
	if r.To == "" {
		return nil, fmt.Errorf("to is required")
	}
	return defaultRule{r.To, r.Value}, nil
}

func (r defaultRule) apply(t *table, _ *applyContext) {
	dst := t.column(r.to)
	for i := range dst.values {
		if dst.values[i] == "" {
			dst.values[i], dst.present[i] = r.value, true
		}
	}
}

func (r defaultRule) describe(c *catalogBuilder) { c.derive(r.to, "default") }

// ---------- extract ----------

type extractRule struct {
	from, to, template string
	re                 *regexp.Regexp
}

// maxExtractCache bounds the per-run cache of extract results (distinct source values).
const maxExtractCache = 1024

func newExtractRule(r fieldRule) (rule, error) {
	if err := require(r.From, r.To, r.Pattern); err != nil {
		return nil, fmt.Errorf("from, to and pattern are required")
	}
	re, err := regexp.Compile(r.Pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %v", err)
	}
	tmpl := r.Template
	if tmpl == "" {
		tmpl = "$1"
	}
	return extractRule{r.From, r.To, tmpl, re}, nil
}

func (r extractRule) apply(t *table, _ *applyContext) {
	src := t.cols[r.from]
	if src == nil {
		return
	}
	// Meta fields (paths, hosts) repeat a lot within one result: run the regex once per distinct
	// value. The cache is bounded so high-cardinality fields cost no extra memory.
	type result struct {
		value string
		ok    bool
	}
	cache := make(map[string]result, 16)
	var dst *column
	var buf []byte
	for i, ok := range src.present {
		if !ok {
			continue
		}
		v := src.values[i]
		res, hit := cache[v]
		if !hit {
			if m := r.re.FindStringSubmatchIndex(v); m != nil {
				buf = r.re.ExpandString(buf[:0], r.template, v, m)
				res = result{string(buf), true}
			}
			if len(cache) < maxExtractCache {
				cache[v] = res
			}
		}
		if !res.ok {
			continue
		}
		if dst == nil {
			dst = t.column(r.to)
		}
		dst.values[i], dst.present[i] = res.value, true
	}
}

func (r extractRule) describe(c *catalogBuilder) { c.derive(r.to, "extract") }

// ---------- time ----------

type timeRule struct{ from, nanos, to, format string }

func newTimeRule(r fieldRule) (rule, error) {
	if err := require(r.From, r.To); err != nil {
		return nil, fmt.Errorf("from and to are required")
	}
	switch r.Format {
	case "", "rfc3339", "unix_ms", "unix_ns":
	default:
		return nil, fmt.Errorf("format must be rfc3339, unix_ms or unix_ns")
	}
	return timeRule{r.From, r.Nanos, r.To, r.Format}, nil
}

func (r timeRule) apply(t *table, ctx *applyContext) {
	src := t.cols[r.from]
	if src == nil {
		return
	}
	nanos := t.cols[r.nanos] // nil when the collector sends no nanoseconds
	toRowTime := r.to == ctx.timeField
	var dst *column
	if toRowTime {
		if ctx.times.t == nil {
			ctx.times = rowTimes{t: make([]time.Time, t.n), set: make([]bool, t.n)}
		}
	} else {
		dst = t.column(r.to)
	}
	for i, ok := range src.present {
		if !ok {
			continue
		}
		ts, err := parseTimeValue(src.values[i], time.UTC)
		if err != nil {
			continue
		}
		if nanos != nil && nanos.present[i] {
			if ns, err := strconv.ParseInt(nanos.values[i], 10, 64); err == nil && ns > 0 && ns < 1e9 {
				ts = time.Unix(ts.Unix(), ns)
			}
		}
		if toRowTime {
			// Written directly as time.Time: no formatting and re-parsing.
			ctx.times.t[i], ctx.times.set[i] = ts, true
		} else {
			dst.values[i], dst.present[i] = formatTime(ts, r.format), true
		}
	}
}

func (r timeRule) describe(c *catalogBuilder) {
	if r.to != c.timeField { // the time field is the row timestamp, not a column
		c.derive(r.to, "time")
	}
}

func formatTime(t time.Time, format string) string {
	switch format {
	case "unix_ms":
		return strconv.FormatInt(t.UnixMilli(), 10)
	case "unix_ns":
		return strconv.FormatInt(t.UnixNano(), 10)
	default:
		return t.UTC().Format(time.RFC3339Nano)
	}
}

// ---------- template ----------

type templateRule struct {
	to    string
	parts []templatePart
}

// templatePart is literal text or, when field is set, the value of a field.
type templatePart struct {
	text  string
	field string
}

func newTemplateRule(r fieldRule) (rule, error) {
	if err := require(r.To, r.Template); err != nil {
		return nil, fmt.Errorf("to and template are required")
	}
	parts, err := parseTemplate(r.Template)
	if err != nil {
		return nil, fmt.Errorf("invalid template: %v", err)
	}
	return templateRule{r.To, parts}, nil
}

// parseTemplate splits "${a}-${b}" into parts. Field names may contain ":" and "." (e.g.
// ${__tag__:__path__}); "$$" is a literal "$".
func parseTemplate(t string) ([]templatePart, error) {
	var parts []templatePart
	var lit strings.Builder
	for i := 0; i < len(t); i++ {
		switch {
		case strings.HasPrefix(t[i:], "$$"):
			lit.WriteByte('$')
			i++
		case strings.HasPrefix(t[i:], "${"):
			end := strings.IndexByte(t[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("unclosed ${ at %d", i)
			}
			name := strings.TrimSpace(t[i+2 : i+end])
			if name == "" {
				return nil, fmt.Errorf("empty field name at %d", i)
			}
			if lit.Len() > 0 {
				parts = append(parts, templatePart{text: lit.String()})
				lit.Reset()
			}
			parts = append(parts, templatePart{field: name})
			i += end
		default:
			lit.WriteByte(t[i])
		}
	}
	if lit.Len() > 0 {
		parts = append(parts, templatePart{text: lit.String()})
	}
	return parts, nil
}

func (r templateRule) apply(t *table, _ *applyContext) {
	cols := make([]*column, len(r.parts))
	for i, p := range r.parts {
		if p.field != "" {
			cols[i] = t.cols[p.field]
		}
	}
	var dst *column
	buf := make([]byte, 0, 64)
	for i := 0; i < t.n; i++ {
		buf = buf[:0]
		found := false
		for pi, p := range r.parts {
			if p.field == "" {
				buf = append(buf, p.text...)
			} else if c := cols[pi]; c != nil && c.present[i] {
				found = true
				buf = append(buf, c.values[i]...)
			}
		}
		if found {
			if dst == nil {
				dst = t.column(r.to)
			}
			dst.values[i], dst.present[i] = string(buf), true
		}
	}
}

func (r templateRule) describe(c *catalogBuilder) { c.derive(r.to, "template") }

// ---------- columnar rows ----------

// table is a columnar view of log rows: one column per field, with a presence flag per row so an
// absent field differs from an empty one.
type table struct {
	n    int
	cols map[string]*column
}

type column struct {
	values  []string
	present []bool
}

func newTable(logs []map[string]string) *table {
	t := &table{n: len(logs), cols: make(map[string]*column, 32)}
	for i, l := range logs {
		for k, v := range l {
			c := t.cols[k]
			if c == nil {
				c = t.newColumn()
				t.cols[k] = c
			}
			c.values[i] = v
			c.present[i] = true
		}
	}
	return t
}

func (t *table) newColumn() *column {
	return &column{values: make([]string, t.n), present: make([]bool, t.n)}
}

// column returns the named column, creating an empty one when needed.
func (t *table) column(name string) *column {
	c := t.cols[name]
	if c == nil {
		c = t.newColumn()
		t.cols[name] = c
	}
	return c
}

// rowTimes holds row timestamps written by a time rule whose target is the time field.
type rowTimes struct {
	t   []time.Time
	set []bool
}
