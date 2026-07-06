// Package report renders a core.Result for humans and machines. Everything
// the TUI will later display must be derivable from the same Result.
package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/matterpale/depdog/internal/core"
)

// styles carries the palette for the human report. It is built from a
// per-writer renderer, so styles are no-ops (identical bytes) whenever the
// destination is not a color terminal — CI logs and captured output stay
// plain, while a real terminal gets color.
type styles struct {
	bad, good, warn, rule, imp, pos lipgloss.Style
}

// newStyles builds the palette. color forces the profile: "always" emits ANSI
// even when the writer is not a terminal, "never" suppresses it, and "auto" (or
// "") uses the per-writer detection (which also honors NO_COLOR).
func newStyles(w io.Writer, color string) styles {
	r := lipgloss.NewRenderer(w)
	switch color {
	case "always":
		r.SetColorProfile(termenv.ANSI)
	case "never":
		r.SetColorProfile(termenv.Ascii)
	}
	return styles{
		bad:  r.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		good: r.NewStyle().Foreground(lipgloss.Color("2")).Bold(true),
		warn: r.NewStyle().Foreground(lipgloss.Color("3")),
		rule: r.NewStyle().Bold(true),
		imp:  r.NewStyle().Foreground(lipgloss.Color("1")),
		pos:  r.NewStyle().Faint(true),
	}
}

// Text writes the human-readable report. Violations are grouped by the rule
// that fired, in first-occurrence order, which is deterministic because the
// Result is.
func Text(w io.Writer, res *core.Result, elapsed time.Duration, color string) error {
	st := newStyles(w, color)
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
		fmt.Fprintf(&b, "\n%s %s  (%s)\n", st.bad.Render("✗"), st.rule.Render(rule), plural(len(vs), "violation"))
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
			// Pad before styling so ANSI codes never throw off the column.
			fmt.Fprintf(&b, "      → %s%s%s\n",
				st.imp.Render(fmt.Sprintf("%-*s", width, v.ImportPath)), st.pos.Render(pos), marker)
		}
	}

	var unassigned, empty []core.Warning
	for _, wr := range res.Warnings {
		if wr.Kind == core.WarnEmptyComponent {
			empty = append(empty, wr)
		} else {
			unassigned = append(unassigned, wr)
		}
	}
	if len(unassigned) > 0 {
		fmt.Fprintf(&b, "\n%s %s not covered by any component:\n", st.warn.Render("!"), plural(len(unassigned), "package"))
		for _, wr := range unassigned {
			fmt.Fprintf(&b, "    %s  (%s)\n", wr.Package, wr.RelDir)
		}
	}
	if len(empty) > 0 {
		fmt.Fprintf(&b, "\n%s %s with no packages (dead patterns?):\n", st.warn.Render("!"), plural(len(empty), "component"))
		for _, wr := range empty {
			fmt.Fprintf(&b, "    %s\n", wr.Component)
		}
	}
	if len(res.Cycles) > 0 {
		fmt.Fprintf(&b, "\n%s %s:\n", st.warn.Render("!"), plural(len(res.Cycles), "component cycle"))
		for _, c := range res.Cycles {
			fmt.Fprintf(&b, "    %s\n", strings.Join(c, " ↔ "))
		}
	}

	b.WriteString("\n")
	if len(res.Violations) == 0 {
		b.WriteString(st.good.Render("✓ no violations"))
	} else {
		b.WriteString(st.bad.Render(plural(len(res.Violations), "violation")))
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
