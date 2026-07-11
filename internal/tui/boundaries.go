package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The boundaries overlay is the Matrix tab's second, read-only view (key `b`):
// the orthogonal mutual-exclusion axis. Components answer "who may import whom";
// boundaries answer "which members must stay isolated". It is a pure view over
// core's rule set, like the grid — the same data as `depdog config`'s boundaries
// block, with live boundary violations surfaced. Editing boundaries is a later
// phase (docs/tui-visual-editor.md).

func (m Model) boundaryCount() int {
	if m.rules == nil {
		return 0
	}
	return len(m.rules.Boundaries)
}

// boundaryViolationCounts tallies live violations per boundary name.
func (m Model) boundaryViolationCounts() map[string]int {
	counts := map[string]int{}
	for _, v := range m.res.Violations {
		if v.Boundary != "" {
			counts[v.Boundary]++
		}
	}
	return counts
}

func (m Model) boundariesView() string {
	title := styleTitle.Render("Boundaries") + styleDim.Render("   mutual-exclusion sets · b: back to rules")
	if m.rules == nil {
		return title + "\n\n" + styleDim.Render("no compiled rule set available — restart with `depdog tui`")
	}
	bs := m.rules.Boundaries
	if len(bs) == 0 {
		return title + "\n\n" + styleDim.Render("no boundaries defined — add a `boundaries:` block to depdog.yaml")
	}
	sel := clamp(m.matrixBoundSel, len(bs))
	counts := m.boundaryViolationCounts()

	// The selectable list of boundaries.
	var list []string
	for i, b := range bs {
		label := b.Name
		if b.Sealed {
			label += "  " + styleWarn.Render("sealed")
		}
		if c := counts[b.Name]; c > 0 {
			label += "  " + styleBad.Render(fmt.Sprintf("✗%d", c))
		}
		if i == sel {
			list = append(list, styleSelected.Render("▸ "+b.Name)+strings.TrimPrefix(label, b.Name))
		} else {
			list = append(list, "  "+label)
		}
	}

	// The detail pane for the selected boundary.
	b := bs[sel]
	members := make([]string, len(b.Members))
	for i, mem := range b.Members {
		members[i] = mem.Label
	}
	head := styleDim.Render("── ") + styleTitle.Render(b.Name) + styleDim.Render(" ──")
	if b.Sealed {
		head += "  " + styleWarn.Render("sealed")
	}
	detail := []string{
		"",
		head,
		styleDim.Render("  members  ") + strings.Join(members, ", "),
		"  " + styleBad.Render("⊗") + styleDim.Render(" no member may import another (all cross-member edges denied)"),
	}
	if b.Sealed {
		detail = append(detail, "  "+styleWarn.Render("▉")+styleDim.Render(" sealed: nothing outside all members may import in"))
	}
	if c := counts[b.Name]; c > 0 {
		detail = append(detail, "  "+styleBad.Render(plural(c, "live boundary violation")))
	} else {
		detail = append(detail, "  "+styleGood.Render("✓")+styleDim.Render(" no live boundary violations"))
	}

	out := append([]string{title, ""}, list...)
	out = append(out, detail...)

	if m.width > 0 {
		for i, ln := range out {
			if lipgloss.Width(ln) > m.width {
				out[i] = ansi.Truncate(ln, m.width, "…")
			}
		}
	}
	if budget := m.bodyRows(); budget > 0 && len(out) > budget {
		out = out[:budget]
	}
	return strings.Join(out, "\n")
}
