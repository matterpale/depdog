package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// GitHub emits GitHub Actions workflow commands so a check surfaces as inline
// annotations on a pull request: one ::error:: per source position of each
// violation, and a ::warning:: for every unassigned package. A plain summary
// line closes the run log. Output is deterministic given a sorted Result.
func GitHub(w io.Writer, res *core.Result, rs *core.RuleSet) error {
	var b strings.Builder
	githubAnnotations(&b, res, rs, "")
	fmt.Fprintf(&b, "depdog check — %s · %s · %s\n",
		res.ModulePath, plural(len(res.Violations), "violation"), plural(res.Stats.Packages, "package"))
	_, err := io.WriteString(w, b.String())
	return err
}

// GitHubWorkspace emits annotations for every analyzed unit, each file path
// prefixed with the unit's walk-root-relative directory so annotations land
// on the right file from the repo root, then one aggregate summary line.
func GitHubWorkspace(w io.Writer, mods []Module) error {
	var b strings.Builder
	var totalV, totalP int
	for _, m := range mods {
		githubAnnotations(&b, m.Result, m.Rules, m.Rel)
		totalV += len(m.Result.Violations)
		totalP += m.Result.Stats.Packages
	}
	fmt.Fprintf(&b, "depdog check — monorepo · %s · %s across %s\n",
		plural(totalV, "violation"), plural(totalP, "package"), plural(len(mods), "checked unit"))
	_, err := io.WriteString(w, b.String())
	return err
}

// githubAnnotations writes the ::error::/::warning:: lines for one result.
// prefix (a unit's walk-root-relative dir, "" for a single unit) is joined
// onto each file location.
func githubAnnotations(b *strings.Builder, res *core.Result, rs *core.RuleSet, prefix string) {
	for _, v := range res.Violations {
		msg := fmt.Sprintf("%s imports %s (%s)", v.FromComponent, v.ImportPath, v.Rule)
		if v.TestOnly {
			msg += " [test]"
		}
		// Append the same plain-English explanation the JSON/text/SARIF surfaces
		// carry, so the terse annotation gains the WHY + fix (one source of
		// wording: core.Explanation). LF in it is escaped by ghData below.
		msg += " — " + core.Explanation(core.ExplainViolation(v, rs))
		if len(v.Positions) == 0 {
			fmt.Fprintf(b, "::error::%s\n", ghData(msg))
			continue
		}
		for _, p := range v.Positions {
			fmt.Fprintf(b, "::error file=%s,line=%d::%s\n", ghProp(joinPrefix(prefix, p.File)), p.Line, ghData(msg))
		}
	}
	for _, wr := range res.Warnings {
		fmt.Fprintf(b, "::warning::%s is not covered by any component (%s)\n", ghData(wr.Package), ghData(joinPrefix(prefix, wr.RelDir)))
	}
}

// ghData escapes a workflow-command message body: only %, CR and LF are
// special there.
func ghData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// ghProp escapes a workflow-command property value (file=…), where ':' and ','
// are additionally significant.
func ghProp(s string) string {
	return strings.NewReplacer(
		"%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C",
	).Replace(s)
}
