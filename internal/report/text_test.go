package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func dirtyResult() *core.Result {
	return &core.Result{
		ModulePath: "example.test/m",
		Violations: []core.Violation{{
			FromPackage:   "example.test/m/a",
			FromComponent: "a",
			ImportPath:    "example.test/m/b",
			Target:        "b",
			Rule:          "a: allow [std]",
			Positions:     []core.Position{{File: "a/a.go", Line: 3}},
		}},
		Warnings: []core.Warning{{Package: "example.test/m/w", RelDir: "w"}},
		Stats:    core.Stats{Packages: 2, Edges: 4},
	}
}

// TestTextNoANSIWhenNotTerminal locks in the invariant the goldens rely on:
// writing to a plain buffer (as CI and captured output do) produces no escape
// codes, so styling can never destabilize machine-read text.
func TestTextNoANSIWhenNotTerminal(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, dirtyResult(), 0); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Errorf("styled output leaked ANSI to a non-terminal writer:\n%q", out)
	}
	for _, want := range []string{"✗", "a: allow [std]", "example.test/m/b", "a/a.go:3", "1 violation", "not covered"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestTextEmptyComponentWarning(t *testing.T) {
	res := &core.Result{
		ModulePath: "example.test/m",
		Warnings:   []core.Warning{{Kind: core.WarnEmptyComponent, Component: "ghost"}},
		Stats:      core.Stats{Packages: 1},
	}
	var buf bytes.Buffer
	if err := Text(&buf, res, 0); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "ghost") || !strings.Contains(out, "no packages") {
		t.Errorf("empty-component warning not rendered:\n%s", out)
	}
	// It's a warning, not a violation.
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("empty component should not read as a violation:\n%s", out)
	}
}

func TestTextCleanIsPlain(t *testing.T) {
	var buf bytes.Buffer
	res := &core.Result{ModulePath: "example.test/m", Stats: core.Stats{Packages: 3, Edges: 5}}
	if err := Text(&buf, res, 0); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Errorf("clean output leaked ANSI:\n%q", out)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("clean output should confirm no violations:\n%s", out)
	}
}
