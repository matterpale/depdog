package tui

import (
	"fmt"
	"sort"
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
// columns: the abstract buckets every adapter fills. Specific external modules
// referenced by any rule (RefExternalModule) get their own columns after
// these, so a depguard-style module rule reads as an explicit cell rather
// than disappearing into the stance.
var specialTargets = []struct{ target, header string }{
	{"std", "std"},
	{"external", "ext"},
	{"unassigned", "una"},
}

// matrixModules lists the distinct external-module refs across every rule,
// sorted — each gets its own column after the special buckets. Derived from
// the (possibly staged) rule set on every call, so columns appear and vanish
// with the rules that mention them.
func (m Model) matrixModules() []string {
	if m.rules == nil {
		return nil
	}
	seen := map[string]bool{}
	var mods []string
	collect := func(refs []core.Ref) {
		for _, r := range refs {
			if r.Kind == core.RefExternalModule && !seen[r.Name] {
				seen[r.Name] = true
				mods = append(mods, r.Name)
			}
		}
	}
	for _, rule := range m.rules.Rules {
		collect(rule.Allow)
		collect(rule.Deny)
	}
	// Modules named only in the top-level deny still get a column, so a
	// module-wide ban is visible in the grid (every cell reads as a hard deny).
	collect(m.rules.GlobalDeny)
	sort.Strings(mods)
	return mods
}

// moduleHeader is the short column label for a module path: its last segment.
// The focus pane's cursor line names the full path. Trailing slashes are
// trimmed first, so a ref like "example.com/mod/" still yields a labelled
// column rather than a blank one; a ref that trims to nothing falls back to the
// raw path.
func moduleHeader(mod string) string {
	seg := strings.TrimRight(mod, "/")
	if i := strings.LastIndexByte(seg, '/'); i >= 0 {
		seg = seg[i+1:]
	}
	if seg == "" {
		return mod
	}
	return seg
}

// moduleHeaders returns a column header per module in mods: normally the last
// path segment (moduleHeader), but any last segment shared by two or more
// modules is widened to the shortest trailing path suffix that tells them apart
// — e.g. "a/util" vs "b/util" — falling back to the full path. Distinct module
// paths always yield distinct headers, so no two columns read the same.
func moduleHeaders(mods []string) []string {
	heads := make([]string, len(mods))
	collide := map[string]int{}
	for i, mod := range mods {
		heads[i] = moduleHeader(mod)
		collide[heads[i]]++
	}
	for i, mod := range mods {
		if collide[heads[i]] > 1 { // the bare last segment is ambiguous — widen it
			heads[i] = uniqueModuleSuffix(mod, mods)
		}
	}
	return heads
}

// uniqueModuleSuffix returns the shortest trailing path suffix of mod (whole
// segments) that no other module in mods shares at the same segment length, or
// mod's full path if none is unique.
func uniqueModuleSuffix(mod string, mods []string) string {
	segs := moduleSegs(mod)
	for k := 1; k < len(segs); k++ {
		suf := strings.Join(segs[len(segs)-k:], "/")
		unique := true
		for _, other := range mods {
			if other == mod {
				continue
			}
			osegs := moduleSegs(other)
			if len(osegs) >= k && strings.Join(osegs[len(osegs)-k:], "/") == suf {
				unique = false
				break
			}
		}
		if unique {
			return suf
		}
	}
	if full := strings.Join(segs, "/"); full != "" {
		return full
	}
	return mod
}

// moduleSegs splits a module path into its slash-separated segments, ignoring a
// trailing slash (matching moduleHeader's normalization).
func moduleSegs(mod string) []string {
	return strings.Split(strings.TrimRight(mod, "/"), "/")
}

// moduleColWidth sizes the module columns to their longest header label,
// bounded so one long name cannot flood the grid.
func moduleColWidth(heads []string) int {
	w := 0
	for _, h := range heads {
		if l := len(h); l > w {
			w = l
		}
	}
	if w > 9 {
		w = 9
	}
	if w < 3 {
		w = 3
	}
	return w + 2
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
	// A module-wide deny wins over the source component's own allow, so it is
	// checked first — otherwise the cell would read allow while `check` flags the
	// edge (both consult the same rs.GloballyDenied that Decide does).
	if _, ok := rs.GloballyDenied(target, false, ""); ok {
		return cellDeny
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

// matrixCol is one column of the rule matrix: its import target, header label,
// whether that target is an external module (resolved through the module
// decision path rather than the bucket one), its display width, its x offset
// within the scrollable region, and whether a group separator precedes it.
type matrixCol struct {
	target     string // component name, a special bucket, or a full module path
	head       string // the short column-header label
	isModule   bool
	groupStart bool // first column of the specials / modules group — a separator precedes it
	w, x       int
}

// matrixCols is the single source of truth for the components│specials│modules
// column layout: the ordered columns with their targets, headers, widths, and
// geometry. Every consumer — the grid render, the column-cursor bound, the
// target/verdict lookups, and the horizontal window — reads it, so the layout
// is encoded once. Derived from the (possibly staged) rule set, so columns
// appear and vanish with the rules that mention each module.
func (m Model) matrixCols() []matrixCol {
	if m.rules == nil {
		return nil
	}
	comps := m.rules.Components
	mods := m.matrixModules()
	heads := moduleHeaders(mods)
	modW := moduleColWidth(heads)
	cols := make([]matrixCol, 0, len(comps)+len(specialTargets)+len(mods))
	x := 0
	add := func(c matrixCol) {
		c.x = x
		x += c.w
		cols = append(cols, c)
	}
	for i, c := range comps {
		add(matrixCol{target: c.Name, head: fmt.Sprintf("%d", i+1), w: matrixCompW})
	}
	x += matrixSepW
	for i, st := range specialTargets {
		add(matrixCol{target: st.target, head: st.header, w: matrixSpecW, groupStart: i == 0})
	}
	if len(mods) > 0 {
		x += matrixSepW
		for i, mod := range mods {
			add(matrixCol{target: mod, head: heads[i], isModule: true, w: modW, groupStart: i == 0})
		}
	}
	return cols
}

// matrixColCount is the number of columns — the clamp bound for the column cursor.
func (m Model) matrixColCount() int { return len(m.matrixCols()) }

// matrixTargetAt maps a column index to its import target (component name,
// special bucket, or full module path), "" when out of range.
func (m Model) matrixTargetAt(col int) string {
	cols := m.matrixCols()
	if col < 0 || col >= len(cols) {
		return ""
	}
	return cols[col].target
}

// verdictFor is the cell verdict for the from component against a resolved
// column, dispatching module columns to the module decision path. The grid's
// per-cell path uses it against precomputed columns so a full render never
// re-derives the module list.
func (m Model) verdictFor(from string, c matrixCol) cellKind {
	if c.isModule {
		return moduleCellVerdict(m.rules, from, c.target)
	}
	return cellVerdict(m.rules, from, c.target)
}

// verdictAt is verdictFor addressed by column index, for the focus pane and the
// toggle — both cold paths resolving a single cursored cell.
func (m Model) verdictAt(from string, col int) cellKind {
	cols := m.matrixCols()
	if col < 0 || col >= len(cols) {
		return cellSelf
	}
	return m.verdictFor(from, cols[col])
}

// moduleCellVerdict decides how the from→module cell reads: an exact
// allow/deny ref for that module path is explicit; anything else (including a
// broader prefix ref or the external bucket) falls to DecideModule, the same
// path `explain <from> <module>` reports.
func moduleCellVerdict(rs *core.RuleSet, from, mod string) cellKind {
	// A module-wide deny wins over any component allow (even an exact module
	// allow), so it is checked before the component rule — keeping the cell in
	// step with `check`/`explain`, which decide the global ban first.
	if _, ok := rs.GloballyDenied("", true, mod); ok {
		return cellDeny
	}
	rule := rs.Rules[from]
	switch {
	case hasExactModuleRef(rule.Deny, mod):
		return cellDeny
	case hasExactModuleRef(rule.Allow, mod):
		return cellAllow
	}
	if allowed, _ := rs.DecideModule(from, mod); allowed {
		return cellDefaultAllow
	}
	return cellDefaultDeny
}

func hasExactModuleRef(refs []core.Ref, mod string) bool {
	for _, r := range refs {
		if r.Kind == core.RefExternalModule && r.Name == mod {
			return true
		}
	}
	return false
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

// toggleCell cycles the verdict of the cursored (from → target) edge and stages
// the change (in memory, via applyEdit). Self edges — and, without an editor —
// are inert.
func (m Model) toggleCell() (tea.Model, tea.Cmd) {
	if m.editor == nil || m.rules == nil {
		m.status = "editing not available (read-only session)"
		return m, nil
	}
	comps := m.rules.Components
	if len(comps) == 0 {
		return m, nil
	}
	from := comps[clamp(m.matrixSel, len(comps))].Name
	col := clamp(m.matrixCol, m.matrixColCount())
	target := m.matrixTargetAt(col)
	if target == "" || target == from {
		m.status = "a component always imports itself — nothing to toggle"
		return m, nil
	}
	verdict := nextVerdict(m.verdictAt(from, col))
	return m.applyEdit(
		func(d []byte) ([]byte, error) { return m.editor.SetRule(d, from, target, verdict) },
		fmt.Sprintf("%s → %s: %s", from, target, verdict))
}

const (
	matrixCompW = 3 // width of a component column (holds a 1-glyph cell / a 1–2 digit index)
	matrixSpecW = 4 // width of a special-target column (holds "std"/"ext"/"una")
	matrixSepW  = 2 // width of a group separator (" │" between headers/cells, "─┼" on the divider)
	hMarkerW    = 1 // width of the dim ‹ the horizontal window prepends for hidden-left columns
	hTailW      = 1 // width of the … matrixView's shared right-edge truncation appends
)

// centerGlyph places a single-width glyph in a w-wide column, styled.
func centerGlyph(glyph string, w int, style lipgloss.Style) string {
	left := (w - 1) / 2
	return strings.Repeat(" ", left) + style.Render(glyph) + strings.Repeat(" ", w-1-left)
}

// centerText centers a short header in a w-wide column, dimmed — bold and
// bright when active marks it as the cursor's column. Module headers may hold
// non-ASCII runes, so it measures and clips by display width (never mid-rune)
// rather than by byte count.
func centerText(s string, w int, active bool) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "")
	}
	style := styleDim
	if active {
		style = styleActive
	}
	sw := lipgloss.Width(s)
	left := (w - sw) / 2
	return strings.Repeat(" ", left) + style.Render(s) + strings.Repeat(" ", w-sw-left)
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

// selectedComponent is the component under the row cursor, or nil when there is
// no compiled rule set / no components.
func (m Model) selectedComponent() *core.Component {
	if m.rules == nil || len(m.rules.Components) == 0 {
		return nil
	}
	return &m.rules.Components[clamp(m.matrixSel, len(m.rules.Components))]
}

// matrixGrid builds the fixed header lines and one line per component row. The
// sel row's label is a selection bar, the col column's header is highlighted,
// and the (sel,col) intersection is drawn as the edit cursor. The 1-based row
// index also heads that component's column, so the row labels double as the
// column key. Grids wider than the terminal are windowed horizontally: the
// row-label gutter stays fixed and the column region scrolls to keep the
// cursor visible, a dim ‹ marking columns hidden on the left (the shared
// right-edge truncation marks the right).
func (m Model) matrixGrid(sel, col int) (header, rows []string) {
	comps := m.rules.Components
	cols := m.matrixCols()

	// Live violations, keyed by the (from component, column target) cell they
	// cross. A module import records Target "external", so besides the external
	// bucket it also lights every module column its import path falls under —
	// otherwise the module columns, the whole point of the feature, never
	// highlight.
	viol := make(map[[2]string]bool, len(m.res.Violations))
	for _, v := range m.res.Violations {
		viol[[2]string{v.FromComponent, v.Target}] = true
		if v.Target == "external" {
			for _, c := range cols {
				if c.isModule && (v.ImportPath == c.target || strings.HasPrefix(v.ImportPath, c.target+"/")) {
					viol[[2]string{v.FromComponent, c.target}] = true
				}
			}
		}
	}

	labelW := len(`from \ to`)
	for i, c := range comps {
		if w := len(fmt.Sprintf("%d %s", i+1, c.Name)); w > labelW {
			labelW = w
		}
	}

	hslice := m.matrixHSlice(cols, col, labelW+matrixSepW)
	sep := styleDim.Render(" │")

	// The header, the divider, and every cell row share one walk over the
	// columns: a group separator before each group-start column, then the
	// column's content at its own width.
	writeCols := func(b *strings.Builder, groupSep string, content func(b *strings.Builder, i int, c matrixCol)) {
		for i, c := range cols {
			if c.groupStart {
				b.WriteString(groupSep)
			}
			content(b, i, c)
		}
	}

	var h strings.Builder
	writeCols(&h, sep, func(b *strings.Builder, i int, c matrixCol) {
		b.WriteString(centerText(c.head, c.w, i == col))
	})

	var d strings.Builder
	writeCols(&d, "─┼", func(b *strings.Builder, _ int, c matrixCol) {
		b.WriteString(strings.Repeat("─", c.w))
	})

	header = []string{
		styleTitle.Render("Rule matrix") + styleWarn.Render("  experimental") + styleDim.Render("   rows import columns · ↑↓←→ move the cursor"),
		"",
		styleDim.Render(padRight(`from \ to`, labelW)) + sep + hslice(h.String()),
		styleDim.Render(strings.Repeat("─", labelW)+"─┼") + hslice(styleDim.Render(d.String())),
	}

	for i, from := range comps {
		label := padRight(fmt.Sprintf("%d %s", i+1, from.Name), labelW)
		if i == sel {
			label = styleSelected.Render(label)
		}
		var r strings.Builder
		writeCols(&r, sep, func(b *strings.Builder, j int, c matrixCol) {
			g, style := glyphFor(m.verdictFor(from.Name, c))
			if viol[[2]string{from.Name, c.target}] {
				style = styleSelectedBad // a live crossing pops out of the grid
			}
			if i == sel && j == col {
				style = styleCursor // the edit cursor wins over everything
			}
			b.WriteString(centerGlyph(g, c.w, style))
		})
		rows = append(rows, label+sep+hslice(r.String()))
	}
	return header, rows
}

// matrixHSlice returns the transform the grid applies to each line's column
// region: the identity when everything fits (or no size is known yet), else a
// left-trim that keeps the cursor's column in view, replacing the hidden
// prefix with a dim ‹. The right edge is clipped by the view's shared
// ANSI-aware truncation, so only the left side is handled here. Derived
// purely from the cursor, like the vertical window — no scroll state.
func (m Model) matrixHSlice(cols []matrixCol, col, gutterW int) func(string) string {
	identity := func(s string) string { return s }
	if m.width <= 0 || len(cols) == 0 {
		return identity
	}
	last := cols[len(cols)-1]
	restW := last.x + last.w
	avail := m.width - gutterW
	// After a left trim the region reads ‹ marker · visible columns · … tail.
	// Reserve the marker and tail, plus one column so the cursor never sits
	// flush against the ellipsis; don't trim when the region already fits or the
	// window is too narrow to hold a marker and a tail on both sides.
	margin := hMarkerW + hTailW + 1
	if avail < 2*margin || restW <= avail {
		return identity
	}
	c := cols[clamp(col, len(cols))]
	hoff := c.x + c.w - (avail - margin) // keep the cursor clear of the … tail
	if hoff > c.x {
		hoff = c.x // never hide the cursor's own left edge
	}
	if hoff <= 0 {
		return identity
	}
	return func(s string) string { return ansi.TruncateLeft(s, hoff, styleDim.Render("‹")) }
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
		k := m.verdictAt(from, col)
		cursor := "  " + styleDim.Render("cursor") + " " + from + " → " + styleTitle.Render(target) +
			styleDim.Render("  = "+verdictLabel(k))
		if m.editor != nil {
			cursor += styleDim.Render("   (space → " + nextVerdict(k) + ")")
		}
		lines = append(lines, cursor)
	}
	return lines
}

// matrixView renders the Matrix tab: the fixed header, a height-aware window of
// component rows kept centered on the selection (with ▲/▼ markers), the focus
// pane for the selected component, and the legend. Wide grids scroll
// horizontally with the cursor (matrixHSlice); the final ANSI-aware truncation
// clips the right edge so a large matrix can't push the header off screen.
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
