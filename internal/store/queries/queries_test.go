package queries

import (
	"strings"
	"testing"
)

// Loading happens in init; this pins the failure modes that protect it.
func TestEveryFieldIsFilled(t *testing.T) {
	set, err := load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for name, field := range set.bindings() {
		if strings.TrimSpace(*field) == "" {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestStatementsAreTrimmedAndUnterminated(t *testing.T) {
	stmts, err := parseAll()
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range stmts {
		if text != strings.TrimSpace(text) {
			t.Errorf("%s: not trimmed", name)
		}
		if strings.HasSuffix(text, ";") {
			t.Errorf("%s: trailing semicolon should be stripped", name)
		}
	}
}

func TestParseRejectsBadFiles(t *testing.T) {
	cases := map[string]string{
		"duplicate key": "-- name: a\nSELECT 1;\n-- name: a\nSELECT 2;",
		"empty body":    "-- name: a\n-- name: b\nSELECT 1;",
		"empty name":    "-- name:\nSELECT 1;",
	}
	for title, body := range cases {
		t.Run(title, func(t *testing.T) {
			out := map[string]string{}
			if err := parseFile("t.sql", body, out); err == nil {
				t.Errorf("expected an error, got %v", out)
			}
		})
	}
}

// The two list statements take their id placeholders from the caller.
func TestOnlyIDListsAreFormatted(t *testing.T) {
	for name, field := range Q.bindings() {
		wantVerb := name == "push.list_by_sub_ids" || name == "push.list_by_role_and_sub_ids"
		if strings.Contains(*field, "%s") != wantVerb {
			t.Errorf("%s: unexpected format verb", name)
		}
	}
}
