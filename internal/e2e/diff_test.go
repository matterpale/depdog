package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInit turns dir into a git repo with a deterministic identity, so commits
// succeed regardless of the machine's global git config.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "depdog@example.test")
	git(t, dir, "config", "user.name", "depdog test")
	// Avoid signing/hook surprises on developer machines.
	git(t, dir, "config", "commit.gpgsign", "false")
}

// git runs a git subcommand in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", // ignore the developer's ~/.gitconfig
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDiffSinceReportsAddedEdge builds a throwaway git repo with two commits: a
// clean two-component module, then a change adding a cross-component import
// (handler → repository). `depdog diff --since <first>` must report that one
// added component edge and exit 0 (informational, not a gate).
func TestDiffSinceReportsAddedEdge(t *testing.T) {
	dir := t.TempDir()

	const cfg = `version: 2
components:
  handler:    { path: "internal/handler/**",    allow: ["*"] }
  repository: { path: "internal/repository/**", allow: [std] }
default: allow
`
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.test/diff\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "depdog.yaml"), cfg)
	// First commit: handler does NOT yet import repository.
	writeFile(t, filepath.Join(dir, "internal/handler/handler.go"), "package handler\n")
	writeFile(t, filepath.Join(dir, "internal/repository/repo.go"), "package repository\n")

	gitInit(t, dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "initial: no cross-component edge")
	first := git(t, dir, "rev-parse", "HEAD")

	// Second commit (working tree): handler now imports repository.
	writeFile(t, filepath.Join(dir, "internal/handler/handler.go"),
		"package handler\n\nimport _ \"example.test/diff/internal/repository\"\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "handler now imports repository")

	out, stderr, exit := run(t, dir, "diff", "--since", first)
	if exit != 0 {
		t.Fatalf("diff exit %d, want 0 (informational)\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "1 cross-component edge added") {
		t.Errorf("diff should report one added edge:\n%s", out)
	}
	if !strings.Contains(out, "+ handler → repository") {
		t.Errorf("diff should name the added handler → repository edge:\n%s", out)
	}
	// The working tree is the "after" graph; leftover worktrees must be gone.
	if wt := git(t, dir, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Errorf("temp worktree not cleaned up:\n%s", wt)
	}

	// The same diff in --format json must be a well-formed, deterministic delta
	// naming the one added edge with snake_case keys and [] (not null) arrays.
	jsonOut, stderr, exit := run(t, dir, "diff", "--since", first, "--format", "json")
	if exit != 0 {
		t.Fatalf("diff --format json exit %d, want 0\nstdout:\n%s\nstderr:\n%s", exit, jsonOut, stderr)
	}
	var delta struct {
		Since string `json:"since"`
		Added []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"added"`
		Removed []json.RawMessage `json:"removed"`
		Stats   struct {
			Added int `json:"added"`
		} `json:"stats"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &delta); err != nil {
		t.Fatalf("diff --format json is not valid JSON: %v\n%s", err, jsonOut)
	}
	if delta.Since != first {
		t.Errorf("since = %q, want %q", delta.Since, first)
	}
	if delta.Stats.Added != 1 || len(delta.Added) != 1 {
		t.Fatalf("json should report one added edge, got %+v", delta)
	}
	if delta.Added[0].From != "handler" || delta.Added[0].To != "repository" {
		t.Errorf("added edge = %s → %s, want handler → repository", delta.Added[0].From, delta.Added[0].To)
	}
	// removed must be [] (not null) even when empty.
	if delta.Removed == nil || strings.Contains(jsonOut, "null") {
		t.Errorf("empty removed must encode as [], not null:\n%s", jsonOut)
	}

	// A second json run is byte-identical (deterministic).
	jsonOut2, _, _ := run(t, dir, "diff", "--since", first, "--format", "json")
	if jsonOut2 != jsonOut {
		t.Errorf("diff --format json not deterministic:\n%s\n---\n%s", jsonOut, jsonOut2)
	}
}

// TestDiffCleansUpWorktreeOnScanError locks in the safety invariant that the
// temporary worktree is torn down even when the "before" scan fails AFTER the
// worktree is created: the first commit has no go.mod, so scanning that ref
// errors, and the deferred cleanup must still leave no worktree behind.
func TestDiffCleansUpWorktreeOnScanError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "depdog.yaml"),
		"version: 2\ncomponents:\n  a: { path: \"**\", allow: [\"*\"] }\ndefault: allow\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")
	gitInit(t, dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "no go.mod yet")
	first := git(t, dir, "rev-parse", "HEAD")

	// Current tree adds go.mod so the "after" scan resolves; the "before" ref
	// still lacks it, so scanning the materialized worktree fails after add.
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.test/diff\n\ngo 1.26\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "add go.mod")

	_, stderr, exit := run(t, dir, "diff", "--since", first)
	if exit != 2 {
		t.Fatalf("exit %d, want 2 (before-scan fails: no go.mod at the ref)\nstderr:\n%s", exit, stderr)
	}
	// Despite the scan error, the deferred cleanup must have removed the worktree.
	if wt := git(t, dir, "worktree", "list"); strings.Count(wt, "\n") != 0 {
		t.Errorf("temp worktree leaked after a scan error:\n%s", wt)
	}
}

// TestDiffUnknownFormat is a usage error (exit 2): an invalid --format value is
// rejected with an actionable message before any git or scan work.
func TestDiffUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.test/diff\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "depdog.yaml"),
		"version: 2\ncomponents:\n  a: { path: \"**\", allow: [\"*\"] }\ndefault: allow\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")

	_, stderr, exit := run(t, dir, "diff", "--since", "HEAD", "--format", "toml")
	if exit != 2 {
		t.Fatalf("exit %d, want 2 (unknown --format)", exit)
	}
	if !strings.Contains(strings.ToLower(stderr), "format") {
		t.Errorf("stderr should point at --format:\n%s", stderr)
	}
}

// TestDiffMissingSince is a usage error (exit 2): --since is required.
func TestDiffMissingSince(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.test/diff\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "depdog.yaml"),
		"version: 2\ncomponents:\n  a: { path: \"**\", allow: [\"*\"] }\ndefault: allow\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")

	_, stderr, exit := run(t, dir, "diff")
	if exit != 2 {
		t.Fatalf("exit %d, want 2 (missing --since)", exit)
	}
	// fang capitalizes the first letter, so match case-insensitively.
	if !strings.Contains(strings.ToLower(stderr), "since") {
		t.Errorf("stderr should point at --since:\n%s", stderr)
	}
}

// TestDiffNotAGitRepo is a git error (exit 2): diff needs git to materialize the
// ref, so a non-repo directory fails actionably.
func TestDiffNotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.test/diff\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "depdog.yaml"),
		"version: 2\ncomponents:\n  a: { path: \"**\", allow: [\"*\"] }\ndefault: allow\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")

	_, stderr, exit := run(t, dir, "diff", "--since", "HEAD")
	if exit != 2 {
		t.Fatalf("exit %d, want 2 (not a git repo)", exit)
	}
	if !strings.Contains(stderr, "git") {
		t.Errorf("stderr should mention git:\n%s", stderr)
	}
}

// TestDiffUnknownRef is a git error (exit 2) with an actionable message.
func TestDiffUnknownRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.test/diff\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "depdog.yaml"),
		"version: 2\ncomponents:\n  a: { path: \"**\", allow: [\"*\"] }\ndefault: allow\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")
	gitInit(t, dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "initial")

	_, stderr, exit := run(t, dir, "diff", "--since", "no-such-ref")
	if exit != 2 {
		t.Fatalf("exit %d, want 2 (unknown ref)", exit)
	}
	if !strings.Contains(stderr, "no-such-ref") {
		t.Errorf("stderr should name the bad ref:\n%s", stderr)
	}
}
