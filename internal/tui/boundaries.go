package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/matterpale/depdog/internal/core"
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

// currentBoundary is the boundary under the ↑/↓ cursor, or nil.
func (m Model) currentBoundary() *core.Boundary {
	if m.rules == nil || len(m.rules.Boundaries) == 0 {
		return nil
	}
	return &m.rules.Boundaries[clamp(m.matrixBoundSel, len(m.rules.Boundaries))]
}

// currentMemberCount / currentMember report the ←/→ member cursor within the
// selected boundary.
func (m Model) currentMemberCount() int {
	if b := m.currentBoundary(); b != nil {
		return len(b.Members)
	}
	return 0
}

func (m Model) currentMember() (string, bool) {
	b := m.currentBoundary()
	if b == nil || len(b.Members) == 0 {
		return "", false
	}
	return b.Members[clamp(m.matrixMemberSel, len(b.Members))].Label, true
}

// removeSelectedMember drops the cursored member from the selected boundary via
// the remove hook, then re-runs the check.
func (m Model) removeSelectedMember() (tea.Model, tea.Cmd) {
	b := m.currentBoundary()
	member, ok := m.currentMember()
	if b == nil || !ok || m.removeMember == nil {
		return m, nil
	}
	if err := m.removeMember(b.Name, member); err != nil {
		m.status = "remove failed: " + oneLine(err.Error())
		return m, nil
	}
	m.status = fmt.Sprintf("removed %q from %q — re-running…", member, b.Name)
	return m, m.startRefresh()
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

	// The detail pane for the selected boundary: its members as a ←/→-cursored
	// list, then the isolation/sealed notes.
	b := bs[sel]
	head := styleDim.Render("── ") + styleTitle.Render(b.Name) + styleDim.Render(" ──")
	if b.Sealed {
		head += "  " + styleWarn.Render("sealed")
	}
	detail := []string{"", head, styleDim.Render("  members")}
	msel := clamp(m.matrixMemberSel, len(b.Members))
	for i, mem := range b.Members {
		if i == msel {
			detail = append(detail, "  "+styleSelected.Render("▸ "+mem.Label))
		} else {
			detail = append(detail, "    "+mem.Label)
		}
	}
	detail = append(detail,
		"  "+styleBad.Render("⊗")+styleDim.Render(" no member may import another (all cross-member edges denied)"))
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
