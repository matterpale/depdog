package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/matterpale/depdog/internal/core"
)

// The Matrix tab is a read-only visualization of the compiled rule set as an
// adjacency matrix: rows import columns, each cell showing whether that edge is
// allowed, denied, or left to the component's stance — with live violations
// highlighted. It is the first, non-editing phase of the visual rule editor
// concept (docs/tui-visual-editor.md); like the Config tab it adds a view over
// core's types, not new data, so every glyph is derivable from `depdog config`
// plus `depdog check`.

// specialTargets are the non-component import targets that get their own
// columns: the abstract buckets every adapter fills. A specific external-module
// allow/deny ref has no column here, so such an edge reads as a stance cell —
// module columns are a later refinement.
var specialTargets = []struct{ target, header string }{
	{"std", "std"},
	{"external", "ext"},
	{"unassigned", "una"},
}

// cellKind classifies one from→target edge for the matrix.
type cellKind int

const (
	cellSelf         cellKind = iota // the diagonal: a component and itself
	cellAllow                        // an explicit allow rule permits it
	cellDeny                         // an explicit deny rule forbids it
	cellDefaultAllow                 // no matching rule; the stance/default permits it
	cellDefaultDeny                  // no matching rule; the stance/default forbids it
)

// refMatchesTarget mirrors core's unexported matcher over the exported Ref
// fields, so the matrix can tell an explicit rule from a stance fallback without
// re-deriving the whole decision. A RefExternalModule (a specific module) never
// matches a bucket target here, so such an edge reads as a stance cell.
func refMatchesTarget(r core.Ref, target string) bool {
	switch r.Kind {
	case core.RefAny:
		return true
	case core.RefStd:
		return target == "std"
	case core.RefExternal:
		return target == "external"
	case core.RefUnassigned:
		return target == "unassigned"
	case core.RefComponent:
		return r.Name == target
	}
	return false
}

func anyRefMatches(refs []core.Ref, target string) bool {
	for _, r := range refs {
		if refMatchesTarget(r, target) {
			return true
		}
	}
	return false
}

// cellVerdict decides how the from→target cell reads. An explicit deny wins over
// an explicit allow (mirroring core.Decide); an edge no rule mentions falls to
// the component's inferred stance, resolved through core.Decide.
func cellVerdict(rs *core.RuleSet, from, target string) cellKind {
	if from == target {
		return cellSelf
	}
	rule := rs.Rules[from]
	switch {
	case anyRefMatches(rule.Deny, target):
		return cellDeny
	case anyRefMatches(rule.Allow, target):
		return cellAllow
	}
	if allowed, _ := rs.Decide(from, target); allowed {
		return cellDefaultAllow
	}
	return cellDefaultDeny
}

func glyphFor(k cellKind) (glyph string, style lipgloss.Style) {
	switch k {
	case cellAllow:
		return "✓", styleGood
	case cellDeny:
		return "✗", styleBad
	case cellDefaultAllow:
		return "✓", styleDim
	case cellDefaultDeny:
		return "✗", styleDim
	default: // cellSelf
		return "—", styleDim
	}
}

const (
	matrixCompW = 3 // width of a component column (holds a 1-glyph cell / a 1–2 digit index)
	matrixSpecW = 4 // width of a special-target column (holds "std"/"ext"/"una")
)

// centerGlyph places a single-width glyph in a w-wide column, styled.
func centerGlyph(glyph string, w int, style lipgloss.Style) string {
	left := (w - 1) / 2
	return strings.Repeat(" ", left) + style.Render(glyph) + strings.Repeat(" ", w-1-left)
}

// centerText centers a short ASCII header in a w-wide column, dimmed.
func centerText(s string, w int) string {
	if len(s) > w {
		s = s[:w]
	}
	left := (w - len(s)) / 2
	return strings.Repeat(" ", left) + styleDim.Render(s) + strings.Repeat(" ", w-len(s)-left)
}

func padRight(s string, w int) string {
	if d := w - len(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// matrixLines renders the rule matrix as a slice of lines the view windows. The
// left column names each component (prefixed by the 1-based index that also
// heads its column), then the component columns, then the std/external/
// unassigned buckets.
func (m Model) matrixLines() []string {
	head := []string{styleTitle.Render("Rule matrix") + styleDim.Render("   rows import columns")}
	if m.rules == nil {
		return append(head, "", styleDim.Render("no compiled rule set available — restart with `depdog tui`"))
	}
	comps := m.rules.Components
	if len(comps) == 0 {
		return append(head, "", styleDim.Render("no components defined"))
	}

	// Live violations, keyed by the (from component, target) cell they cross.
	viol := make(map[[2]string]bool, len(m.res.Violations))
	for _, v := range m.res.Violations {
		viol[[2]string{v.FromComponent, v.Target}] = true
	}

	labelW := len(`from \ to`)
	for i, c := range comps {
		if w := len(fmt.Sprintf("%d %s", i+1, c.Name)); w > labelW {
			labelW = w
		}
	}

	sep := styleDim.Render(" │")
	cell := func(from, target string, w int) string {
		g, style := glyphFor(cellVerdict(m.rules, from, target))
		if viol[[2]string{from, target}] {
			style = styleSelectedBad // a live crossing pops out of the grid
		}
		return centerGlyph(g, w, style)
	}

	// Header: blank label cell, then the component indices, then bucket headers.
	var h strings.Builder
	h.WriteString(styleDim.Render(padRight(`from \ to`, labelW)))
	h.WriteString(sep)
	for i := range comps {
		h.WriteString(centerText(fmt.Sprintf("%d", i+1), matrixCompW))
	}
	h.WriteString(sep)
	for _, st := range specialTargets {
		h.WriteString(centerText(st.header, matrixSpecW))
	}

	divider := styleDim.Render(
		strings.Repeat("─", labelW) + "─┼" +
			strings.Repeat("─", matrixCompW*len(comps)) + "─┼" +
			strings.Repeat("─", matrixSpecW*len(specialTargets)))

	lines := append(head, "", h.String(), divider)

	for i, from := range comps {
		var r strings.Builder
		r.WriteString(padRight(fmt.Sprintf("%d %s", i+1, from.Name), labelW))
		r.WriteString(sep)
		for _, to := range comps {
			r.WriteString(cell(from.Name, to.Name, matrixCompW))
		}
		r.WriteString(sep)
		for _, st := range specialTargets {
			r.WriteString(cell(from.Name, st.target, matrixSpecW))
		}
		lines = append(lines, r.String())
	}

	lines = append(lines, "",
		styleGood.Render("  ✓")+styleDim.Render(" allow   ")+styleBad.Render("✗")+
			styleDim.Render(" deny   faint ✓/✗ = via stance/default   — self"),
		styleDim.Render("  an inverse cell marks a live violation crossing that edge"))
	return lines
}

func (m Model) matrixLineCount() int { return len(m.matrixLines()) }

// matrixView renders the matrix into the height-aware window, with the same
// ▲/▼ markers the Config document uses. It is a document (a scroll offset), not
// a selection. Lines wider than the terminal are truncated (ANSI-aware) so a
// large matrix can never push the header off screen; horizontal scrolling is a
// later refinement.
func (m Model) matrixView() string {
	lines := m.matrixLines()
	if m.width > 0 {
		for i, ln := range lines {
			if lipgloss.Width(ln) > m.width {
				lines[i] = ansi.Truncate(ln, m.width, "…")
			}
		}
	}

	budget := m.bodyRows()
	if budget <= 0 || len(lines) <= budget {
		return strings.Join(lines, "\n")
	}
	visible := budget - 2 // leave room for the ▲/▼ markers
	if visible < 1 {
		visible = 1
	}
	off := clampScroll(m.matrixScroll, len(lines), budget)
	end := off + visible
	if end > len(lines) {
		end = len(lines)
	}
	var out []string
	if off > 0 {
		out = append(out, moreLine("▲", off))
	}
	out = append(out, lines[off:end]...)
	if below := len(lines) - end; below > 0 {
		out = append(out, moreLine("▼", below))
	}
	return strings.Join(out, "\n")
}
