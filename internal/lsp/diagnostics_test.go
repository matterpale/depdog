package lsp

import (
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

// TestDiagnosticsSeverity checks that a warn-severity violation maps to the LSP
// Warning severity (2) while an error-severity one maps to Error (1).
func TestDiagnosticsSeverity(t *testing.T) {
	res := &core.Result{
		Violations: []core.Violation{
			{FromComponent: "a", ImportPath: "m/b", Target: "b", Rule: "a: allow [std]",
				Severity: core.SeverityError, Positions: []core.Position{{File: "a/a.go", Line: 3}}},
			{FromComponent: "c", ImportPath: "m/d", Target: "d", Rule: "c: allow [std]",
				Severity: core.SeverityWarn, Positions: []core.Position{{File: "c/c.go", Line: 5}}},
		},
	}
	bySuffix := map[string]int{}
	for _, p := range diagnosticsFor(res, "/root", "") {
		for _, d := range p.Diagnostics {
			switch {
			case strings.HasSuffix(p.URI, "a/a.go"):
				bySuffix["a"] = d.Severity
			case strings.HasSuffix(p.URI, "c/c.go"):
				bySuffix["c"] = d.Severity
			}
		}
	}
	if bySuffix["a"] != severityError {
		t.Errorf("error violation diagnostic severity = %d, want %d (Error)", bySuffix["a"], severityError)
	}
	if bySuffix["c"] != severityWarning {
		t.Errorf("warn violation diagnostic severity = %d, want %d (Warning)", bySuffix["c"], severityWarning)
	}
}
