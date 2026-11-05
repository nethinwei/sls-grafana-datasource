package main

import (
	"errors"
	"strings"
)

var errDerivedField = errors.New("the field is produced by a field rule")

// logsView is logsConfig compiled once per data source instance: everything needed to turn SLS
// rows into a Logs frame and to describe fields to the editor.
type logsView struct {
	rules      []rule
	problems   []string // invalid rules, reported by the health check
	timeField  string
	levelField string
	// slsNames maps a field renamed by the rules back to its SLS name, for filters; shownNames is
	// the reverse, for the log line.
	slsNames   map[string]string
	shownNames map[string]string
	// derived are fields produced by rules other than rename; SLS cannot filter on them.
	derived     map[string]bool
	lineFields  []string
	lineRest    bool
	lineExclude map[string]bool
	// maxFlattened bounds the fields the json rule creates per result (0: no limit).
	maxFlattened int
}

func compileLogsView(cfg logsConfig) logsView {
	v := logsView{timeField: cfg.TimeField, levelField: cfg.LevelField, lineRest: true}
	if v.timeField == "" {
		v.timeField = defaultTimeField
	}
	if v.levelField == "" {
		v.levelField = "level"
	}
	if cfg.Line.Rest != nil {
		v.lineRest = *cfg.Line.Rest
	}
	v.lineExclude = map[string]bool{}
	for _, f := range cfg.Line.Exclude {
		v.lineExclude[f] = true
	}
	for _, f := range cfg.Line.Fields {
		if f != "" && !v.lineExclude[f] {
			v.lineFields = append(v.lineFields, f)
		}
	}
	rules := defaultFieldRules
	if cfg.Rules != nil {
		rules = *cfg.Rules
	}
	v.rules, v.problems = compileRules(rules)

	// Describing the rules on an empty catalog tells what they produce, independent of any logstore.
	c := newCatalogBuilder(v.timeField)
	for _, r := range v.rules {
		r.describe(c)
	}
	v.slsNames, v.shownNames, v.derived = map[string]string{}, map[string]string{}, map[string]bool{}
	for name, f := range c.fields {
		if f.SLSName != "" {
			v.slsNames[name] = f.SLSName
			v.shownNames[f.SLSName] = name
		} else {
			v.derived[name] = true
		}
	}
	return v
}

// slsName is the SLS field behind a field shown in Grafana (itself unless renamed).
func (v *logsView) slsName(name string) string {
	if sls, ok := v.slsNames[name]; ok {
		return sls
	}
	return name
}

// table runs the rules on logs and returns the resulting columns and the run's context (row times
// written by a time rule, fields flattened by a json rule).
func (v *logsView) table(logs []map[string]string) (*table, *applyContext) {
	t := newTable(logs)
	ctx := &applyContext{timeField: v.timeField, maxFlattened: v.maxFlattened}
	for _, r := range v.rules {
		r.apply(t, ctx)
	}
	return t, ctx
}

// fieldExpr returns the SPL expression reading a flattened JSON field, "" for other fields. The
// same field may also be stored flat when the collector expands JSON (processor_json), as during
// a migration, so both forms are read:
//
//	coalesce("request_method", json_extract_scalar("request", '$["method"]'))
func (v *logsView) fieldExpr(name string, ctx *applyContext) string {
	p, ok := ctx.flattened[name]
	if !ok {
		return ""
	}
	src := p.field
	if sls, renamed := v.slsNames[src]; renamed {
		src = sls
	}
	var path strings.Builder
	path.WriteString("$")
	for _, k := range p.keys {
		path.WriteString(`["` + strings.ReplaceAll(strings.ReplaceAll(k, `\`, `\\`), `"`, `\"`) + `"]`)
	}
	return "coalesce(" + quoteIdentifier(name) + ", json_extract_scalar(" + quoteIdentifier(src) + ", " + splString(path.String()) + "))"
}
