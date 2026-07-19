package report

import (
	"fmt"
	"io"
	"path"
	"time"

	"github.com/matterpale/depdog/internal/core"
)

// Module is one analyzed unit handed to the aggregate reporters (TextWorkspace,
// JSONWorkspace, GitHubWorkspace, SARIFWorkspace). "Unit" is the public noun for
// a subtree rooted by a depdog.yaml and checked with its own adapter; the type
// keeps the Module name to contain churn, but its meaning is widened — a unit is
// a go.work member (all "go") or any discovered polyglot unit (Lang carries the
// per-unit adapter). Rel doubles as the unit's "dir" key in the JSON envelope.
type Module struct {
	Path   string // module path, e.g. "example.test/app"
	Rel    string // walk-root-relative dir, slash-separated, e.g. "web" (the envelope's "dir")
	Lang   string // the adapter that checked this unit, e.g. "go" / "ts" (the envelope's "lang")
	Result *core.Result
	Rules  *core.RuleSet
}

// Skipped is a unit-shaped directory that was not analyzed (it holds a
// language marker but no depdog.yaml).
type Skipped struct {
	Rel    string // walk-root-relative dir, slash-separated
	Reason string
}

// TextWorkspace writes the aggregate human report: each analyzed unit as its own
// section (the same body Text produces, under a `▸ ./<dir> (<lang>)` header), the
// cross-unit section on a work-file run (cross may be nil), a skipped-units
// advisory, and a rolled-up summary line counting checked units.
func TextWorkspace(w io.Writer, mods []Module, skipped []Skipped, cross *CrossUnit, elapsed time.Duration, color string) error {
	st := newStyles(w, color)
	var totalErr, totalWarnV, totalW, totalP, totalE int
	for i, m := range mods {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s\n", st.rule.Render(fmt.Sprintf("▸ ./%s (%s)", m.Rel, m.Lang)))
		if err := Text(w, m.Result, elapsed, color); err != nil {
			return err
		}
		ec := m.Result.ErrorCount()
		totalErr += ec
		totalWarnV += len(m.Result.Violations) - ec // warn-severity violations
		totalW += len(m.Result.Warnings)
		totalP += m.Result.Stats.Packages
		totalE += m.Result.Stats.Edges
	}
	if cross != nil {
		if len(mods) > 0 {
			fmt.Fprintln(w)
		}
		textCrossUnit(w, st, cross)
	}
	if len(skipped) > 0 {
		fmt.Fprintf(w, "\n%s %s skipped (no depdog.yaml):\n", st.warn.Render("!"), plural(len(skipped), "unit"))
		for _, s := range skipped {
			// The standard reason is implied by the header; only a richer one
			// (a work unit scanned for cross-unit governance) earns a note.
			if s.Reason != "" && s.Reason != "no depdog.yaml" {
				fmt.Fprintf(w, "    ./%s  (%s)\n", s.Rel, s.Reason)
			} else {
				fmt.Fprintf(w, "    ./%s\n", s.Rel)
			}
		}
	}
	// The exit code counts only error-severity violations (cross-unit violations
	// are always error-severity), so the summary mark and count must too: a
	// warn-only run is not a failure. The error-violation count is phrased as
	// "violations" (so the clean "0 violations" and all-error cases are unchanged);
	// warn-severity violations get their own segment.
	crossV := cross.violationCount()
	mark := st.good.Render("✓")
	switch {
	case totalErr+crossV > 0:
		mark = st.bad.Render("✗")
	case totalWarnV > 0:
		mark = st.warn.Render("⚠")
	}
	fmt.Fprintf(w, "\n%s %s across %s", mark, plural(totalErr, "violation"), plural(len(mods), "checked unit"))
	if len(skipped) > 0 {
		fmt.Fprintf(w, " (%d skipped)", len(skipped))
	}
	if cross != nil {
		fmt.Fprintf(w, " · %s", plural(crossV, "cross-unit violation"))
	}
	if totalWarnV > 0 {
		fmt.Fprintf(w, " · %s", st.warn.Render(plural(totalWarnV, "warning")))
	}
	if totalW > 0 {
		fmt.Fprintf(w, " · %s", plural(totalW, "warning"))
	}
	fmt.Fprintf(w, " · %s · %s · checked in %s\n",
		plural(totalP, "package"), plural(totalE, "edge"), elapsed.Round(time.Millisecond))
	return nil
}

// joinPrefix prefixes a unit-relative path with the unit's walk-root-relative
// directory, so machine-format file locations (GitHub annotations, SARIF)
// resolve from the repo root. An empty prefix (single-unit runs) is a no-op,
// keeping that output byte-identical.
func joinPrefix(prefix, p string) string {
	if prefix == "" {
		return p
	}
	return path.Join(prefix, p)
}
