package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// matrixColCount is the number of columns (every component, then the special
// targets) — the clamp bound for the column cursor.
func (m Model) matrixColCount() int {
	if m.rules == nil {
		return 0
	}
	return len(m.rules.Components) + len(specialTargets)
}

// matrixTargetAt maps a column index to its import target: the first
// len(Components) columns are components, the rest the special buckets.
func (m Model) matrixTargetAt(col int) string {
	comps := m.rules.Components
	switch {
	case col < 0:
		return ""
	case col < len(comps):
		return comps[col].Name
	case col-len(comps) < len(specialTargets):
		return specialTargets[col-len(comps)].target
	default:
		return ""
	}
}

// nextVerdict is the toggle cycle for a cell: default → allow → deny → default.
func nextVerdict(k cellKind) string {
	switch k {
	case cellAllow:
		return "deny"
	case cellDeny:
		return "default"
	default: // cellDefaultAllow / cellDefaultDeny
		return "allow"
	}
}

// verdictLabel describes a cell's current verdict for the focus pane.
func verdictLabel(k cellKind) string {
	switch k {
	case cellAllow:
		return "allow (explicit)"
	case cellDeny:
		return "deny (explicit)"
	case cellDefaultAllow:
		return "allow (default)"
	case cellDefaultDeny:
		return "deny (default)"
	default:
		return "self"
	}
}

// toggleCell cycles the verdict of the cursored (from → target) edge, writes it
// to depdog.yaml via the edit hook, and re-runs the check so every screen
// reflects the change. Self edges — and, without an edit hook, all edges — are
// inert.
func (m Model) toggleCell() (tea.Model, tea.Cmd) {
	if m.edit == nil {
		m.status = "editing not available (read-only session)"
		return m, nil
	}
	comps := m.rules.Components
	if len(comps) == 0 {
		return m, nil
	}
	from := comps[clamp(m.matrixSel, len(comps))].Name
	target := m.matrixTargetAt(clamp(m.matrixCol, m.matrixColCount()))
	if target == "" || target == from {
		m.status = "a component always imports itself — nothing to toggle"
		return m, nil
	}
	verdict := nextVerdict(cellVerdict(m.rules, from, target))
	if err := m.edit(from, target, verdict); err != nil {
		m.status = "edit failed: " + oneLine(err.Error())
		return m, nil
	}
	cmd := m.startRefresh()
	m.status = fmt.Sprintf("%s → %s: %s — re-running…", from, target, verdict)
	return m, cmd
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

// centerText centers a short ASCII header in a w-wide column, dimmed — bold and
// bright when active marks it as the cursor's column.
func centerText(s string, w int, active bool) string {
	if len(s) > w {
		s = s[:w]
	}
	style := styleDim
	if active {
		style = styleActive
	}
	left := (w - len(s)) / 2
	return strings.Repeat(" ", left) + style.Render(s) + strings.Repeat(" ", w-len(s)-left)
}

func padRight(s string, w int) string {
	if d := w - len(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// matrixRowCount is the number of selectable rows (one per component), the
// clamp bound for the Matrix tab's selection.
func (m Model) matrixRowCount() int {
	if m.rules == nil {
		return 0
	}
	return len(m.rules.Components)
}

// matrixGrid builds the fixed header lines and one line per component row. The
// sel row's label is a selection bar, the col column's header is highlighted,
// and the (sel,col) intersection is drawn as the edit cursor. The 1-based row
// index also heads that component's column, so the row labels double as the
// column key.
func (m Model) matrixGrid(sel, col int) (header, rows []string) {
	comps := m.rules.Components

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
	cell := func(from, target string, w, colIdx, rowIdx int) string {
		g, style := glyphFor(cellVerdict(m.rules, from, target))
		if viol[[2]string{from, target}] {
			style = styleSelectedBad // a live crossing pops out of the grid
		}
		if rowIdx == sel && colIdx == col {
			style = styleCursor // the edit cursor wins over everything
		}
		return centerGlyph(g, w, style)
	}

	var h strings.Builder
	h.WriteString(styleDim.Render(padRight(`from \ to`, labelW)))
	h.WriteString(sep)
	for i := range comps {
		h.WriteString(centerText(fmt.Sprintf("%d", i+1), matrixCompW, i == col))
	}
	h.WriteString(sep)
	for k, st := range specialTargets {
		h.WriteString(centerText(st.header, matrixSpecW, len(comps)+k == col))
	}
	divider := styleDim.Render(
		strings.Repeat("─", labelW) + "─┼" +
			strings.Repeat("─", matrixCompW*len(comps)) + "─┼" +
			strings.Repeat("─", matrixSpecW*len(specialTargets)))
	header = []string{
		styleTitle.Render("Rule matrix") + styleDim.Render("   rows import columns · ↑↓←→ move the cursor"),
		"", h.String(), divider,
	}

	for i, from := range comps {
		label := padRight(fmt.Sprintf("%d %s", i+1, from.Name), labelW)
		if i == sel {
			label = styleSelected.Render(label)
		}
		var r strings.Builder
		r.WriteString(label)
		r.WriteString(sep)
		for j, to := range comps {
			r.WriteString(cell(from.Name, to.Name, matrixCompW, j, i))
		}
		r.WriteString(sep)
		for k, st := range specialTargets {
			r.WriteString(cell(from.Name, st.target, matrixSpecW, len(comps)+k, i))
		}
		rows = append(rows, r.String())
	}
	return header, rows
}

// matrixFocus renders the per-component "arrows" pane for the selected row: its
// inferred stance, its explicit allow/deny refs as → / ⊗ arrows, and the
// targets it currently violates. This is the concept's focus view — the same
// data as `depdog explain <component>`, read off the compiled rule set.
func (m Model) matrixFocus(sel, col int) []string {
	from := m.rules.Components[sel].Name
	stance := stanceShort(m.rules.Stance(from))

	var crossings []string
	seen := map[string]bool{}
	nViol := 0
	for _, v := range m.res.Violations {
		if v.FromComponent != from {
			continue
		}
		nViol++
		if v.Target != "" && !seen[v.Target] {
			seen[v.Target] = true
			crossings = append(crossings, v.Target)
		}
	}

	headline := styleDim.Render("── focus: ") + styleTitle.Render(from) + styleDim.Render(" ── "+stance)
	if nViol > 0 {
		headline += "   " + styleBad.Render(plural(nViol, "violation")+" from here")
	}
	lines := []string{"", headline}

	rule, hasRule := m.rules.Rules[from]
	switch {
	case hasRule && (len(rule.Allow) > 0 || len(rule.Deny) > 0):
		if len(rule.Allow) > 0 {
			lines = append(lines, "  "+styleGood.Render("allow →")+"  "+refsText(rule.Allow))
		}
		if len(rule.Deny) > 0 {
			lines = append(lines, "  "+styleBad.Render("deny  ⊗")+"  "+refsText(rule.Deny))
		}
	default:
		def := "allow"
		if m.rules.Stance(from) == core.PolicyDeny {
			def = "deny"
		}
		lines = append(lines, "  "+styleDim.Render("no rule — falls to default: "+def))
	}
	if len(crossings) > 0 {
		lines = append(lines, "  "+styleBad.Render("crossing ✗")+"  "+strings.Join(crossings, ", "))
	}

	// The cursor line: the exact edge a toggle acts on, and what it would become.
	if target := m.matrixTargetAt(col); target != "" && target != from {
		k := cellVerdict(m.rules, from, target)
		cursor := "  " + styleDim.Render("cursor") + " " + from + " → " + styleTitle.Render(target) +
			styleDim.Render("  = "+verdictLabel(k))
		if m.edit != nil {
			cursor += styleDim.Render("   (space → " + nextVerdict(k) + ")")
		}
		lines = append(lines, cursor)
	}
	return lines
}

// matrixView renders the Matrix tab: the fixed header, a height-aware window of
// component rows kept centered on the selection (with ▲/▼ markers), the focus
// pane for the selected component, and the legend. Rows wider than the terminal
// are truncated (ANSI-aware) so a large matrix can't push the header off screen;
// horizontal scrolling is a later refinement.
func (m Model) matrixView() string {
	title := styleTitle.Render("Rule matrix")
	if m.rules == nil {
		return title + "\n\n" + styleDim.Render("no compiled rule set available — restart with `depdog tui`")
	}
	if len(m.rules.Components) == 0 {
		return title + "\n\n" + styleDim.Render("no components defined")
	}

	sel := clamp(m.matrixSel, len(m.rules.Components))
	col := clamp(m.matrixCol, m.matrixColCount())
	header, rows := m.matrixGrid(sel, col)
	focus := m.matrixFocus(sel, col)
	legend := []string{"",
		styleGood.Render("  ✓") + styleDim.Render(" allow  ") + styleBad.Render("✗") +
			styleDim.Render(" deny   faint = stance/default   ") + styleCursor.Render("▮") + styleDim.Render(" cursor   — self")}

	var out []string
	if budget := m.bodyRows(); budget > 0 {
		gridBudget := budget - len(header) - len(focus) - len(legend)
		if gridBudget < 3 {
			gridBudget = 3
		}
		max := gridBudget
		if len(rows) > max {
			if max -= 2; max < 1 { // room for the ▲/▼ markers
				max = 1
			}
		}
		start, end, above, below := window(len(rows), sel, max)
		out = append(out, header...)
		if above > 0 {
			out = append(out, moreLine("▲", above))
		}
		out = append(out, rows[start:end]...)
		if below > 0 {
			out = append(out, moreLine("▼", below))
		}
		out = append(out, focus...)
		out = append(out, legend...)
	} else {
		out = append(append(append(append([]string{}, header...), rows...), focus...), legend...)
	}

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
