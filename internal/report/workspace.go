package report

import (
	"fmt"
	"io"
	"path"
	"time"

	"github.com/matterpale/depdog/internal/core"
)

// Module is one analyzed workspace member handed to the aggregate reporters
// (TextWorkspace, JSONWorkspace, GitHubWorkspace, SARIFWorkspace).
type Module struct {
	Path   string // module path, e.g. "example.test/app"
	Rel    string // workspace-relative dir, slash-separated, e.g. "app"
	Result *core.Result
	Rules  *core.RuleSet
}

// Skipped is a workspace member that was not analyzed (it has no depdog.yaml).
type Skipped struct {
	Rel    string // workspace-relative dir, slash-separated
	Reason string
}

// TextWorkspace writes the aggregate human report: each analyzed member as its
// own section (the same body Text produces, under a module header), a skipped-
// members advisory, and a rolled-up summary line.
func TextWorkspace(w io.Writer, mods []Module, skipped []Skipped, elapsed time.Duration, color string) error {
	st := newStyles(w, color)
	var totalV, totalW, totalP, totalE int
	for i, m := range mods {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s ./%s\n", st.rule.Render("▸ module"), m.Rel)
		if err := Text(w, m.Result, elapsed, color); err != nil {
			return err
		}
		totalV += len(m.Result.Violations)
		totalW += len(m.Result.Warnings)
		totalP += m.Result.Stats.Packages
		totalE += m.Result.Stats.Edges
	}
	if len(skipped) > 0 {
		fmt.Fprintf(w, "\n%s %s skipped (no depdog.yaml):\n", st.warn.Render("!"), plural(len(skipped), "module"))
		for _, s := range skipped {
			fmt.Fprintf(w, "    ./%s\n", s.Rel)
		}
	}
	mark := st.good.Render("✓")
	if totalV > 0 {
		mark = st.bad.Render("✗")
	}
	fmt.Fprintf(w, "\n%s %s across %s", mark, plural(totalV, "violation"), plural(len(mods), "checked module"))
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

// joinPrefix prefixes a module-relative path with the member's workspace-
// relative directory, so machine-format file locations (GitHub annotations,
// SARIF) resolve from the workspace/repo root. An empty prefix (single-module
// runs) is a no-op, keeping that output byte-identical.
func joinPrefix(prefix, p string) string {
	if prefix == "" {
		return p
	}
	return path.Join(prefix, p)
}
