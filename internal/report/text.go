// Package report renders a core.Result for humans and machines. Everything
// the TUI will later display must be derivable from the same Result.
package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/matterpale/depdog/internal/core"
)

// Text writes the human-readable report. Violations are grouped by the rule
// that fired, in first-occurrence order, which is deterministic because the
// Result is.
func Text(w io.Writer, res *core.Result, elapsed time.Duration) error {
	var b strings.Builder
	fmt.Fprintf(&b, "depdog check — %s\n", res.ModulePath)

	var order []string
	groups := make(map[string][]core.Violation)
	for _, v := range res.Violations {
		if _, ok := groups[v.Rule]; !ok {
			order = append(order, v.Rule)
		}
		groups[v.Rule] = append(groups[v.Rule], v)
	}
	for _, rule := range order {
		vs := groups[rule]
		fmt.Fprintf(&b, "\n✗ %s  (%s)\n", rule, plural(len(vs), "violation"))
		width := 0
		for _, v := range vs {
			if len(v.ImportPath) > width {
				width = len(v.ImportPath)
			}
		}
		lastPkg := ""
		for _, v := range vs {
			if v.FromPackage != lastPkg {
				fmt.Fprintf(&b, "    %s\n", v.FromPackage)
				lastPkg = v.FromPackage
			}
			pos := ""
			if len(v.Positions) > 0 {
				pos = fmt.Sprintf("  %s:%d", v.Positions[0].File, v.Positions[0].Line)
				if len(v.Positions) > 1 {
					pos += fmt.Sprintf(" (+%d more)", len(v.Positions)-1)
				}
			}
			marker := ""
			if v.TestOnly {
				marker = " [test]"
			}
			fmt.Fprintf(&b, "      → %-*s%s%s\n", width, v.ImportPath, pos, marker)
		}
	}

	if len(res.Warnings) > 0 {
		fmt.Fprintf(&b, "\n! %s not covered by any component:\n", plural(len(res.Warnings), "package"))
		for _, wr := range res.Warnings {
			fmt.Fprintf(&b, "    %s  (%s)\n", wr.Package, wr.RelDir)
		}
	}

	b.WriteString("\n")
	if len(res.Violations) == 0 {
		b.WriteString("✓ no violations")
	} else {
		b.WriteString(plural(len(res.Violations), "violation"))
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintf(&b, " · %s", plural(len(res.Warnings), "warning"))
	}
	fmt.Fprintf(&b, " · %s · %s checked in %s\n",
		plural(res.Stats.Packages, "package"),
		plural(res.Stats.Edges, "edge"),
		elapsed.Round(time.Millisecond))

	_, err := io.WriteString(w, b.String())
	return err
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
