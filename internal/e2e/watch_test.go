package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckWatchRejectsJSON: --watch is text-only; combining it with a machine
// format is a usage error (exit 2), reported before any watching begins (so this
// returns immediately rather than hanging).
func TestCheckWatchRejectsJSON(t *testing.T) {
	_, stderr, exit := run(t, fixture("clean"), "check", "--watch", "--format", "json")
	if exit != 2 {
		t.Fatalf("exit %d, want 2 for --watch with a non-text format", exit)
	}
	low := strings.ToLower(stderr)
	if !strings.Contains(low, "watch") || !strings.Contains(low, "text") {
		t.Errorf("stderr should explain watch is text-only: %s", stderr)
	}
}

// TestCheckWatchFirstRunConfigErrorIsFatal: a config that fails to load makes the
// initial --watch run exit 2 (matching the one-shot contract) rather than
// silently entering the watch loop. It returns immediately — no hang.
func TestCheckWatchFirstRunConfigErrorIsFatal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.test/broken\n\ngo 1.21\n")
	// An unknown top-level key trips the strict decoder → config load error.
	writeFile(t, filepath.Join(dir, "depdog.yaml"), "version: 2\nboguskey: true\n")

	_, stderr, exit := run(t, dir, "check", "--watch")
	if exit != 2 {
		t.Fatalf("exit %d, want 2 when the initial watched check can't load its config\nstderr: %s", exit, stderr)
	}
}
