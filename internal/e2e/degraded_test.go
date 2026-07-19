package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckGoDegradedFallback runs `check` on a Go module `go list` cannot fully
// resolve (an unresolved in-module import). depdog must NOT abort: it degrades to
// a best-effort graph, checks the edges it can see, and prints a human-actionable
// warning to stderr — while machine stdout (json) stays clean.
func TestCheckGoDegradedFallback(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/deg\n\ngo 1.21\n")
	write("a/a.go", "package a\n\nimport (\n\t\"strings\"\n\n\t\"example.com/deg/b\"\n\t\"example.com/deg/missing\"\n)\n\nvar _ = strings.TrimSpace\nvar _ = b.V\nvar _ = missing.X\n")
	write("b/b.go", "package b\n\nvar V = 1\n")
	write("depdog.yaml", "version: 2\n\ncomponents:\n  all: { path: \"**\", allow: [\"*\", std, external] }\n\ndefault: deny\n")

	// text: the warning lands on stderr and the check still runs (exit 0 — the
	// visible edges are all allowed).
	out, stderr, exit := run(t, dir, "check")
	if exit != 0 {
		t.Fatalf("degraded check exit %d, want 0 (should degrade, not abort)\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(stderr, "depdog: warning:") || !strings.Contains(stderr, "approximate") {
		t.Errorf("expected an approximate-classification warning on stderr, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "go mod download") {
		t.Errorf("warning should name the fix (go mod download); stderr:\n%s", stderr)
	}

	// json: the load warning must NOT pollute machine stdout. (Assert on the
	// warning's unique text — the JSON envelope legitimately has a "warnings" key.)
	jout, jerr, jexit := run(t, dir, "check", "--format", "json")
	if jexit != 0 {
		t.Fatalf("degraded json check exit %d, want 0\n%s", jexit, jout)
	}
	if strings.Contains(jout, "approximate") || strings.Contains(jout, "go list could not") {
		t.Errorf("load warning prose leaked into json stdout:\n%s", jout)
	}
	// But the machine-readable "degraded" signal IS present, so a CI consumer can
	// tell an approximate graph from an exact one without parsing stderr.
	if !strings.Contains(jout, `"degraded": true`) {
		t.Errorf("json should carry the degraded flag:\n%s", jout)
	}
	if !strings.Contains(jerr, "depdog: warning:") {
		t.Errorf("warning should still be on stderr under --format json; got:\n%s", jerr)
	}
}
