package e2e

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestTrendReportsDrift builds a two-commit repo where the second commit adds a
// cross-component edge, and asserts `trend --since <first>` shows the edge count
// rising 0 → 1 — the drift signal.
func TestTrendReportsDrift(t *testing.T) {
	dir := t.TempDir()
	const cfg = `version: 2
components:
  handler:    { path: "internal/handler/**",    allow: ["*"] }
  repository: { path: "internal/repository/**", allow: [std] }
default: allow
`
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.test/trend\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "depdog.yaml"), cfg)
	writeFile(t, filepath.Join(dir, "internal/handler/handler.go"), "package handler\n")
	writeFile(t, filepath.Join(dir, "internal/repository/repo.go"), "package repository\n")

	gitInit(t, dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "initial: no cross-component edge")
	first := git(t, dir, "rev-parse", "HEAD")

	// Second commit: handler now imports repository (a new cross-component edge).
	writeFile(t, filepath.Join(dir, "internal/handler/handler.go"),
		"package handler\n\nimport _ \"example.test/trend/internal/repository\"\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "handler now imports repository")

	// text
	out, stderr, exit := run(t, dir, "trend", "--since", first)
	if exit != 0 {
		t.Fatalf("trend exit %d, want 0\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "+1 cross-component edge") {
		t.Errorf("trend should report the added cross-component edge:\n%s", out)
	}
	if wt := git(t, dir, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Errorf("temp worktrees not cleaned up:\n%s", wt)
	}

	// json — the edge count rises 0 → 1 across the two sampled points.
	jout, _, jexit := run(t, dir, "trend", "--since", first, "--format", "json")
	if jexit != 0 {
		t.Fatalf("trend json exit %d, want 0\n%s", jexit, jout)
	}
	var tr struct {
		Points []struct {
			Edges int `json:"edges"`
		} `json:"points"`
		Delta struct {
			Edges int `json:"edges"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(jout), &tr); err != nil {
		t.Fatalf("trend json invalid: %v\n%s", err, jout)
	}
	if len(tr.Points) != 2 {
		t.Fatalf("want 2 sampled points, got %d: %s", len(tr.Points), jout)
	}
	if tr.Points[0].Edges != 0 || tr.Points[1].Edges != 1 || tr.Delta.Edges != 1 {
		t.Errorf("edges should rise 0→1 (delta +1): points=%+v delta=%d", tr.Points, tr.Delta.Edges)
	}
}
