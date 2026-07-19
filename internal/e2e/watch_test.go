package e2e

import (
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
