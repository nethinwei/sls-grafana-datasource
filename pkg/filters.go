package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Dashboard ad hoc filters arrive with the query (adhocFilters) and are translated here, where the
// field catalog is at hand:
//
//   - a field renamed by the rules is filtered under its SLS name;
//   - an indexed field (or one whose index is unknown) becomes an SLS search condition, which uses
//     the index and is cheap;
//   - a field without an index becomes an SPL `| where` clause, which scans the logs but works;
//   - a field produced by other rules (extract, template, ...) only exists in Grafana and is skipped
//     with a notice, as is a non-indexed field in an SQL statement (SPL cannot precede SQL).
type adhocFilter struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Value    string   `json:"value"`
	Values   []string `json:"values"`
}

// applyAdhocFilters returns the statement with the filters added and a notice for each filter that
// could not be applied. cat may be nil (catalog unavailable): every field is then assumed indexed.
func applyAdhocFilters(query string, filters []adhocFilter, view *logsView, cat *catalog, learned learnedFields) (string, []string) {
	if len(filters) == 0 {
		return query, nil
	}
	sqlStatement := isSQL(query)
	var search, where, skipped []string
	for _, f := range filters {
		target, ok := filterTarget(f.Key, view, cat, learned)
		if !ok {
			skipped = append(skipped, fmt.Sprintf("%s is produced by a field rule and cannot be filtered in SLS", f.Key))
			continue
		}
		if target.indexed {
			if c := searchCondition(target.name, f); c != "" {
				search = append(search, c)
				continue
			}
			if sqlStatement {
				skipped = append(skipped, fmt.Sprintf("%s %s %s cannot be expressed in SLS search", f.Key, f.Operator, f.Value))
				continue
			}
		} else if sqlStatement {
			skipped = append(skipped, fmt.Sprintf("%s has no index and cannot be filtered in an SQL statement", f.Key))
			continue
		}
		if c := whereCondition(target.expr, f); c != "" {
			where = append(where, c)
		} else {
			skipped = append(skipped, fmt.Sprintf("operator %s is not supported", f.Operator))
		}
	}
	if len(search) > 0 {
		query = addSearchCondition(query, strings.Join(search, " and "))
	}
	if len(where) > 0 {
		query += " | where " + strings.Join(where, " and ")
	}
	return query, skipped
}

// filterField is how SLS reaches a field shown in Grafana.
type filterField struct {
	name    string // SLS field for search; empty for flattened JSON fields
	expr    string // SPL expression for where: the quoted field, or json_extract_scalar(...)
	indexed bool   // search works
}

// learnedFields reports what recent query results said about a field the catalog does not know:
// known with an SPL expression (flattened JSON), or known without one (exists only in Grafana).
type learnedFields func(name string) (expr string, known bool)

// filterTarget returns how SLS reaches a field shown in Grafana; ok is false for fields that exist
// only in Grafana. The catalog comes first (index and sample); then what recent results produced
// (flattened JSON keys outside the sample); then the rules' renames, with fields assumed indexed.
func filterTarget(key string, view *logsView, cat *catalog, learned learnedFields) (filterField, bool) {
	field := func(name string, indexed bool) (filterField, bool) {
		return filterField{name: name, expr: quoteIdentifier(name), indexed: indexed}, true
	}
	if cat != nil {
		if cf, found := cat.byName(key); found {
			switch {
			case cf.SLSName != "":
				return field(cf.SLSName, cf.Indexed == nil || *cf.Indexed)
			case cf.Expr != "":
				return filterField{expr: cf.Expr}, true
			}
			return filterField{}, false
		}
	}
	if learned != nil {
		if expr, known := learned(key); known {
			return filterField{expr: expr}, expr != ""
		}
	}
	if view.derived[key] {
		return filterField{}, false
	}
	if sls, renamed := view.slsNames[key]; renamed {
		return field(sls, true)
	}
	return field(key, true)
}

func quoteSearchValue(v string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
}

func filterValues(f adhocFilter) []string {
	if len(f.Values) > 0 {
		return f.Values
	}
	return []string{f.Value}
}

// searchCondition expresses a filter in SLS search syntax; "" when it cannot.
func searchCondition(key string, f adhocFilter) string {
	eq := func(v string) string { return key + ": " + quoteSearchValue(v) }
	switch f.Operator {
	case "=":
		return eq(f.Value)
	case "!=":
		return "not " + eq(f.Value)
	case "=|", "!=|":
		values := filterValues(f)
		parts := make([]string, len(values))
		for i, v := range values {
			parts[i] = eq(v)
		}
		if f.Operator == "!=|" {
			return "not " + strings.Join(parts, " and not ")
		}
		if len(parts) == 1 {
			return parts[0]
		}
		return "(" + strings.Join(parts, " or ") + ")"
	case "=~", "!~":
		w := regexToWildcard(f.Value)
		if w == "" {
			return "" // a where clause with regexp_like takes over
		}
		if f.Operator == "!~" {
			return "not " + key + ": " + w
		}
		return key + ": " + w
	case "<", ">", "<=", ">=":
		if _, err := strconv.ParseFloat(f.Value, 64); err == nil {
			return key + " " + f.Operator + " " + f.Value
		}
	}
	return ""
}

func splString(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

// whereCondition expresses a filter as an SPL where condition on col (a quoted field or an
// expression); "" when it cannot.
//
// A missing field is null in SPL, which no comparison matches; negative operators compare it as
// "" so that, as in Loki, logs without the field pass `!=`.
func whereCondition(col string, f adhocFilter) string {
	switch f.Operator {
	case "!=", "!=|", "!~":
		col = "coalesce(" + col + ", '')"
	}
	switch f.Operator {
	case "=", "!=":
		return col + " " + f.Operator + " " + splString(f.Value)
	case "=|", "!=|":
		values := filterValues(f)
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i] = splString(v)
		}
		op := " in "
		if f.Operator == "!=|" {
			op = " not in "
		}
		return col + op + "(" + strings.Join(quoted, ", ") + ")"
	case "=~", "!~":
		// Anchored, as Grafana's and Loki's regex matchers match the whole value.
		cond := "regexp_like(" + col + ", " + splString("^(?:"+f.Value+")$") + ")"
		if f.Operator == "!~" {
			return "not " + cond
		}
		return cond
	case "<", ">", "<=", ">=":
		if _, err := strconv.ParseFloat(f.Value, 64); err == nil {
			return "cast(" + col + " as double) " + f.Operator + " " + f.Value
		}
	}
	return ""
}

var wildcardSafe = regexp.MustCompile(`^[\w\-/:@*?]+$`)

// regexToWildcard converts a simple regex (^pay.*, timeout$, a.c) to an SLS search wildcard, which
// cannot start with * or ?; "" when the regex is not that simple.
func regexToWildcard(re string) string {
	body := strings.TrimSuffix(strings.TrimPrefix(re, "^"), "$")
	w := strings.ReplaceAll(strings.ReplaceAll(body, ".*", "*"), ".", "?")
	if !wildcardSafe.MatchString(w) || strings.HasPrefix(w, "*") || strings.HasPrefix(w, "?") {
		return ""
	}
	return w
}

// splitStatement splits "search | rest" at the first pipe outside quotes.
func splitStatement(q string) (search, rest string) {
	var quote byte
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '|':
			return strings.TrimSpace(q[:i]), q[i:]
		}
	}
	return strings.TrimSpace(q), ""
}

// addSearchCondition appends a condition to the search part with "and", wrapping a search that
// has a top-level "or" so the condition applies to all of it.
func addSearchCondition(query, cond string) string {
	search, rest := splitStatement(query)
	switch {
	case search == "" || search == "*":
		search = cond
	case hasTopLevelOr(search):
		search = "(" + search + ") and " + cond
	default:
		search = search + " and " + cond
	}
	if rest == "" {
		return search
	}
	return search + " " + rest
}

func hasTopLevelOr(s string) bool {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && i+4 <= len(s) && isSpace(c) && strings.EqualFold(s[i+1:i+3], "or") && isSpace(s[i+3]):
			return true
		}
	}
	return false
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' }
