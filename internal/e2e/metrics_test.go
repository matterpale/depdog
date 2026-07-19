package e2e

import (
	"strings"
	"testing"
)

// The boundaries fixture exercises cross-component coupling and boundary
// crossings; metrics output has no timing line, so it is golden-stable as-is.
func TestMetricsBoundariesText(t *testing.T) {
	out, _, exit := run(t, fixture("boundaries"), "metrics")
	if exit != 0 {
		t.Fatalf("exit %d, want 0\n%s", exit, out)
	}
	golden(t, "metrics_boundaries_text.golden", out)
}

func TestMetricsBoundariesJSON(t *testing.T) {
	out, _, exit := run(t, fixture("boundaries"), "metrics", "--format", "json")
	if exit != 0 {
		t.Fatalf("exit %d, want 0\n%s", exit, out)
	}
	golden(t, "metrics_boundaries_json.golden", out)
}

// The cycle fixture exercises the component-cycle roll-up (and the singular
// "1 cycle").
func TestMetricsCycleText(t *testing.T) {
	out, _, exit := run(t, fixture("cycle"), "metrics")
	if exit != 0 {
		t.Fatalf("exit %d, want 0\n%s", exit, out)
	}
	golden(t, "metrics_cycle_text.golden", out)
}

func TestMetricsUnknownFormat(t *testing.T) {
	_, stderr, exit := run(t, fixture("clean"), "metrics", "--format", "yaml")
	if exit != 2 {
		t.Fatalf("exit %d, want 2 for an unknown --format", exit)
	}
	// fang capitalises the first letter when it renders the error, so match
	// case-insensitively (as the diff command's usage-error test does).
	if !strings.Contains(strings.ToLower(stderr), "unknown --format") {
		t.Errorf("stderr should explain the bad format: %s", stderr)
	}
}
