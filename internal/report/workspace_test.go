package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/matterpale/depdog/internal/core"
)

func sevViolation(s core.Severity) core.Violation {
	return core.Violation{
		FromPackage: "m/a", FromComponent: "a", ImportPath: "m/b", Target: "b",
		Rule: "a: allow [std]", Severity: s, Positions: []core.Position{{File: "a/a.go", Line: 1}},
	}
}

// TestTextWorkspaceWarnOnly is the regression test for the aggregate reporter
// lagging the single-module one: a multi-unit run whose only violations are
// warnings must NOT print a red ✗ failure summary — it exits 0, so the summary
// must agree.
func TestTextWorkspaceWarnOnly(t *testing.T) {
	mods := []Module{
		{Path: "m", Rel: "unitA", Lang: "go",
			Result: &core.Result{ModulePath: "m", Violations: []core.Violation{sevViolation(core.SeverityWarn)}},
			Rules:  &core.RuleSet{}},
		{Path: "n", Rel: "unitB", Lang: "go",
			Result: &core.Result{ModulePath: "n"}, Rules: &core.RuleSet{}},
	}
	var buf bytes.Buffer
	if err := TextWorkspace(&buf, mods, nil, nil, time.Second, "never"); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if strings.Contains(s, "✗") {
		t.Errorf("warn-only aggregate run shows a ✗ failure mark (contradicts exit 0):\n%s", s)
	}
	if !strings.Contains(s, "0 violations across 2 checked units") {
		t.Errorf("summary should report 0 error-violations:\n%s", s)
	}
	if !strings.Contains(s, "1 warning") {
		t.Errorf("summary should surface the warn-severity violation:\n%s", s)
	}
}

// TestTextWorkspaceErrorStillFails: an error-severity violation still marks ✗
// (the skipped sibling forces the aggregate path).
func TestTextWorkspaceErrorStillFails(t *testing.T) {
	mods := []Module{
		{Path: "m", Rel: "unitA", Lang: "go",
			Result: &core.Result{ModulePath: "m", Violations: []core.Violation{sevViolation(core.SeverityError)}},
			Rules:  &core.RuleSet{}},
	}
	var buf bytes.Buffer
	if err := TextWorkspace(&buf, mods, []Skipped{{Rel: "unitB"}}, nil, time.Second, "never"); err != nil {
		t.Fatal(err)
	}
	if s := buf.String(); !strings.Contains(s, "✗") || !strings.Contains(s, "1 violation across 1 checked unit") {
		t.Errorf("error-severity aggregate run should mark ✗ with 1 violation:\n%s", s)
	}
}
