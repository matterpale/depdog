package e2e

import (
	"strings"
	"testing"
)

// The crossunit fixture is a four-unit polyglot monorepo governed by a root
// depdog.work.yaml: web (ts, own depdog.yaml), shared (ts, config-less —
// scanned for cross-unit governance only), api (go, own depdog.yaml) and
// billing (go, config-less). It exercises every cross-unit verdict kind:
//
//   - api → billing: both members of the `services` boundary (identity
//     channel — api's import of example.com/billing resolves via billing's
//     go.mod module path) → cross-unit-boundary.
//   - shared → web: a relative import back into the web tree (path channel)
//     under shared's deny ["*"] rule → cross-unit.
//   - web → shared/internal: the unit edge is allowed (web: allow [shared])
//     but the relative import lands in shared's internal/** surface →
//     cross-unit-surface. web's imports of @acme/shared and
//     @acme/shared/src/util (identity channel) pass the surface check.
//
// Both intra-unit checks are clean, so every violation in the report is the
// cross pass's. A bare `depdog check` at the root triggers work mode — no
// --all required.

func TestCheckCrossUnitText(t *testing.T) {
	out, stderr, exit := run(t, fixture("crossunit"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "crossunit_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckCrossUnitJSON(t *testing.T) {
	// The envelope gains the additive cross_unit key: declared units (with
	// their detected identities), bespoke cross violations with explanations,
	// boundaries, unit cycles and unit/edge stats.
	out, _, exit := run(t, fixture("crossunit"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "crossunit_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestCheckCrossUnitGitHub(t *testing.T) {
	// Cross-unit annotations carry repo-root-relative positions already (the
	// engine prefixes each source unit's dir), so no joinPrefix is applied.
	out, _, exit := run(t, fixture("crossunit"), "check", "--format", "github")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "crossunit_github.golden", out)
}

func TestCheckCrossUnitSARIF(t *testing.T) {
	// One extra SARIF run carries the cross-unit verdicts beside the per-unit
	// runs.
	out, _, exit := run(t, fixture("crossunit"), "check", "--format", "sarif")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "crossunit_sarif.golden", out)
}

func TestCheckCrossUnitAllFlagIdentical(t *testing.T) {
	// Work mode outranks --all's plain fan-out: an explicit --all at a work
	// root produces the identical report.
	bare, _, bexit := run(t, fixture("crossunit"), "check")
	all, _, aexit := run(t, fixture("crossunit"), "check", "--all")
	if bexit != aexit {
		t.Fatalf("exit codes differ: bare %d vs --all %d", bexit, aexit)
	}
	norm := func(s string) string { return reTextDur.ReplaceAllString(s, "checked in X") }
	if norm(bare) != norm(all) {
		t.Errorf("--all at a work root must match the bare run\n--- bare ---\n%s\n--- --all ---\n%s", bare, all)
	}
}

func TestCheckCrossUnitNarrowSkipsCross(t *testing.T) {
	// --unit needs no --all under a work file, narrows to intra-unit checking,
	// and skips the cross pass (cross edges only mean something over the full
	// unit set). web alone is clean, so the run collapses to the classic
	// single-project output and exits 0.
	out, stderr, exit := run(t, fixture("crossunit"), "check", "--unit", "web")
	if exit != 0 {
		t.Fatalf("exit %d, want 0\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if strings.Contains(out, "cross-unit") {
		t.Errorf("--unit narrowing must skip the cross pass:\n%s", out)
	}
	if !strings.HasPrefix(out, "depdog check — @acme/web") {
		t.Errorf("narrowed run should render classic single-project output:\n%s", out)
	}
}
