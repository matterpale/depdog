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
func GitHub(w io.Writer, res *core.Result) error {
	var b strings.Builder
	for _, v := range res.Violations {
		msg := fmt.Sprintf("%s imports %s (%s)", v.FromComponent, v.ImportPath, v.Rule)
		if v.TestOnly {
			msg += " [test]"
		}
		if len(v.Positions) == 0 {
			fmt.Fprintf(&b, "::error::%s\n", ghData(msg))
			continue
		}
		for _, p := range v.Positions {
			fmt.Fprintf(&b, "::error file=%s,line=%d::%s\n", ghProp(p.File), p.Line, ghData(msg))
		}
	}
	for _, wr := range res.Warnings {
		fmt.Fprintf(&b, "::warning::%s is not covered by any component (%s)\n", ghData(wr.Package), ghData(wr.RelDir))
	}
	fmt.Fprintf(&b, "depdog check — %s · %s · %s\n",
		res.ModulePath, plural(len(res.Violations), "violation"), plural(res.Stats.Packages, "package"))
	_, err := io.WriteString(w, b.String())
	return err
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
