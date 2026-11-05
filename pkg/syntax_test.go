package main

import (
	"encoding/json"
	"os"
	"testing"
)

// The cases are shared with the frontend tests (src/queryModify.test.ts).
type syntaxCases struct {
	SplitStatement []struct{ In, Search, Rest string } `json:"splitStatement"`
	IsSQL          []struct {
		In   string
		Want bool
	} `json:"isSQL"`
	AddCondition []struct{ Query, Condition, Want string } `json:"addCondition"`
	SearchEquals []struct {
		Key, Value, Want string
		Negate           bool
	} `json:"searchEquals"`
	WhereEquals []struct {
		Expr, Value, Want string
		Negate            bool
	} `json:"whereEquals"`
}

func TestSharedFilterSyntax(t *testing.T) {
	raw, err := os.ReadFile("../testdata/filter-syntax.json")
	if err != nil {
		t.Fatal(err)
	}
	var c syntaxCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	op := func(negate bool) string {
		if negate {
			return "!="
		}
		return "="
	}
	for _, tc := range c.SplitStatement {
		if search, rest := splitStatement(tc.In); search != tc.Search || rest != tc.Rest {
			t.Errorf("splitStatement(%q) = %q, %q", tc.In, search, rest)
		}
	}
	for _, tc := range c.IsSQL {
		if got := isSQL(tc.In); got != tc.Want {
			t.Errorf("isSQL(%q) = %v", tc.In, got)
		}
	}
	for _, tc := range c.AddCondition {
		if got := addSearchCondition(tc.Query, tc.Condition); got != tc.Want {
			t.Errorf("addCondition(%q, %q) = %q, want %q", tc.Query, tc.Condition, got, tc.Want)
		}
	}
	for _, tc := range c.SearchEquals {
		if got := searchCondition(tc.Key, "", adhocFilter{Operator: op(tc.Negate), Value: tc.Value}); got != tc.Want {
			t.Errorf("searchEquals(%q, %q, %v) = %q, want %q", tc.Key, tc.Value, tc.Negate, got, tc.Want)
		}
	}
	for _, tc := range c.WhereEquals {
		if got := whereCondition(tc.Expr, adhocFilter{Operator: op(tc.Negate), Value: tc.Value}); got != tc.Want {
			t.Errorf("whereEquals(%q, %q, %v) = %q, want %q", tc.Expr, tc.Value, tc.Negate, got, tc.Want)
		}
	}
}
