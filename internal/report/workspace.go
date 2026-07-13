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
// section (the same body Text produces, under a `▸ ./<dir> (<lang>)` header), a
// skipped-units advisory, and a rolled-up summary line counting checked units.
func TextWorkspace(w io.Writer, mods []Module, skipped []Skipped, elapsed time.Duration, color string) error {
	st := newStyles(w, color)
	var totalV, totalW, totalP, totalE int
	for i, m := range mods {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s\n", st.rule.Render(fmt.Sprintf("▸ ./%s (%s)", m.Rel, m.Lang)))
		if err := Text(w, m.Result, elapsed, color); err != nil {
			return err
		}
		totalV += len(m.Result.Violations)
		totalW += len(m.Result.Warnings)
		totalP += m.Result.Stats.Packages
		totalE += m.Result.Stats.Edges
	}
	if len(skipped) > 0 {
		fmt.Fprintf(w, "\n%s %s skipped (no depdog.yaml):\n", st.warn.Render("!"), plural(len(skipped), "unit"))
		for _, s := range skipped {
			fmt.Fprintf(w, "    ./%s\n", s.Rel)
		}
	}
	mark := st.good.Render("✓")
	if totalV > 0 {
		mark = st.bad.Render("✗")
	}
	fmt.Fprintf(w, "\n%s %s across %s", mark, plural(totalV, "violation"), plural(len(mods), "checked unit"))
	if len(skipped) > 0 {
		fmt.Fprintf(w, " (%d skipped)", len(skipped))
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
