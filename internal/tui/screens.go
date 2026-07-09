package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/report"
)

// listRows is how many rows a scrollable list may occupy given the window
// height, leaving room for the header, detail pane and footer. Zero means the
// window is unsized (or huge) — render the whole list.
func (m Model) listRows() int {
	if m.height == 0 {
		return 0
	}
	r := m.height - 14 // header (3) + spacing + detail pane + footer
	if r < 3 {
		r = 3
	}
	return r
}

// bodyRows is how many terminal rows a screen body may fill, after the header,
// its two surrounding blank lines and the footer. Zero means unsized — the
// caller renders everything and lets the terminal scroll. The Packages screen
// caps its side-by-side list and detail columns to this budget so neither
// overflows the screen height.
func (m Model) bodyRows() int {
	if m.height == 0 {
		return 0
	}
	r := m.height - 6 // header(3) + two blank lines + footer(1)
	if r < 4 {
		r = 4
	}
	return r
}

// window returns the visible half-open range [start,end) of n items that keeps
// sel in view within at most max rows, and how many items are hidden above and
// below. max <= 0 or a list that already fits shows everything.
func window(n, sel, max int) (start, end, above, below int) {
	if max <= 0 || n <= max {
		return 0, n, 0, 0
	}
	start = sel - max/2
	if start < 0 {
		start = 0
	}
	if start+max > n {
		start = n - max
	}
	end = start + max
	return start, end, start, n - end
}

func moreLine(prefix string, count int) string {
	return styleDim.Render(fmt.Sprintf("  %s %d more", prefix, count))
}

func (m Model) dashboardView() string {
	var b strings.Builder

	if len(m.res.Violations) == 0 {
		b.WriteString(styleGood.Render("✓ no violations"))
	} else {
		b.WriteString(styleBad.Render("✗ " + plural(len(m.res.Violations), "violation")))
	}
	b.WriteString(styleDim.Render(fmt.Sprintf("  ·  %s · %s",
		plural(m.res.Stats.Packages, "package"), plural(m.res.Stats.Edges, "edge"))))
	if len(m.res.Warnings) > 0 {
		b.WriteString(styleWarn.Render(fmt.Sprintf("  ·  %s", plural(len(m.res.Warnings), "unassigned package"))))
	}
	b.WriteString("\n\n")
	b.WriteString(m.componentTable())
	return b.String()
}

func (m Model) componentTable() string {
	nameW := len("Component")
	for _, c := range m.res.Components {
		if len(c.Name) > nameW {
			nameW = len(c.Name)
		}
	}
	var b strings.Builder
	b.WriteString(styleDim.Render(fmt.Sprintf("  %-*s  %8s  %6s  %10s",
		nameW, "Component", "Packages", "Edges", "Violations")))
	b.WriteString("\n")
	for _, c := range m.res.Components {
		row := fmt.Sprintf("  %-*s  %8d  %6d  %10d", nameW, c.Name, c.Packages, c.Edges, c.Violations)
		if c.Violations > 0 {
			row = styleBad.Render(row)
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) violationsView() string {
	if len(m.res.Violations) == 0 {
		return styleGood.Render("✓ no violations")
	}
	var b strings.Builder
	if m.filtering || m.filter != "" {
		hint := "  (esc clears)"
		if m.filtering {
			hint = "▊"
		}
		b.WriteString(styleWarn.Render("filter: "+m.filter) + styleDim.Render(hint) + "\n\n")
	}

	vs := m.filteredViolations()
	if len(vs) == 0 {
		b.WriteString(styleDim.Render("no violations match the filter"))
		return b.String()
	}
	sel := clamp(m.selected, len(vs))

	start, end, above, below := window(len(vs), sel, m.listRows())
	if above > 0 {
		b.WriteString(moreLine("▲", above) + "\n")
	}
	for i := start; i < end; i++ {
		v := vs[i]
		line := fmt.Sprintf("%s → %s", v.FromComponent, v.ImportPath)
		if i == sel {
			b.WriteString(styleSelected.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	if below > 0 {
		b.WriteString(moreLine("▼", below) + "\n")
	}

	v := vs[sel]
	b.WriteString("\n")
	b.WriteString(styleDim.Render("── detail ──"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s imports %s\n", v.FromPackage, styleBad.Render(v.ImportPath))
	b.WriteString(styleDim.Render("rule: ") + v.Rule + "\n")
	if v.TestOnly {
		b.WriteString(styleWarn.Render("test-only import") + "\n")
	}
	for _, p := range v.Positions {
		b.WriteString(styleDim.Render(fmt.Sprintf("  %s:%d", p.File, p.Line)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) packagesView() string {
	if len(m.pkgs) == 0 {
		return styleDim.Render("no packages")
	}

	var head []string
	if m.filtering || m.filter != "" {
		hint := "  (esc clears)"
		if m.filtering {
			hint = "▊"
		}
		head = append(head, styleWarn.Render("filter: "+m.filter)+styleDim.Render(hint), "")
	}

	pkgs := m.filteredPackages()
	if len(pkgs) == 0 {
		return strings.Join(append(head, styleDim.Render("no packages match the filter")), "\n")
	}
	sel := clamp(m.selPkg, len(pkgs))

	legend := styleDim.Render("[std] std-lib · [external] third-party · [name] component · ✗ violates a rule")

	// The list and the detail pane sit side by side, so both get the full body
	// height, less the filter head and the full-width legend (a blank + one line)
	// that spans under both columns. paneRows == 0 means unsized: render everything.
	paneRows := 0
	if body := m.bodyRows(); body > 0 {
		if paneRows = body - len(head) - 2; paneRows < 4 {
			paneRows = 4
		}
	}
	list := m.packageList(pkgs, sel, paneRows)
	detail := m.packageDetail(pkgs[sel], paneRows)

	out := head
	if m.width > 0 {
		out = append(out, m.twoColumns(list, detail)...)
	} else {
		// Unsized (the first frame before a resize): there is no width to split
		// into columns, so stack the panes and let the terminal scroll.
		out = append(out, list...)
		out = append(out, "")
		out = append(out, detail...)
	}
	out = append(out, "", legend)
	// Safety net: never exceed the body budget, so the alt-screen header can't
	// scroll off even when a tiny terminal squeezes the panes.
	if body := m.bodyRows(); body > 0 && len(out) > body {
		out = out[:body]
	}
	return strings.Join(out, "\n")
}

// twoColumns lays the package list and the detail pane side by side — the list
// on the left, a vertical divider, the detail on the right — joining them row by
// row. Each column is fitted to its width so a long import path can never wrap
// and push the header off screen, and the divider runs the full height of the
// taller column.
func (m Model) twoColumns(left, right []string) []string {
	const gutter = 3 // the width of the " │ " divider
	avail := m.width - gutter
	if avail < 2 {
		avail = 2
	}
	leftW := 0
	for _, l := range left {
		if w := lipgloss.Width(l); w > leftW {
			leftW = w
		}
	}
	if maxLeft := avail * 2 / 5; leftW > maxLeft { // the list never takes over 40%
		leftW = maxLeft
	}
	if leftW < 1 {
		leftW = 1
	}
	rightW := avail - leftW

	div := styleDim.Render(" │ ")
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = padCell(l, leftW) + div + fitCell(r, rightW)
	}
	return out
}

// fitCell truncates s to at most w visible cells with an ellipsis, ANSI-aware so
// styled text keeps its escape codes intact.
func fitCell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return s
}

// padCell fits s into exactly w cells: truncated when too wide, space-padded when
// too narrow, so the divider to its right stays vertically aligned.
func padCell(s string, w int) string {
	s = fitCell(s, w)
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// packageList renders the grouped, scrollable package list into at most budget
// rows (0 == unbounded), inserting a group header at each boundary and keeping
// the selected package in view. Offending packages are pulled into a leading
// red "▸ violations" group (their order is set in setData); the rest keep their
// component grouping. Headers are flattened into the same line stream as the
// rows, so they count against the budget and the block's height stays fixed —
// that is what keeps the selection from skidding as it moves.
func (m Model) packageList(pkgs []core.PackageView, sel, budget int) []string {
	var lines []string
	selLine := 0
	lastGroup := "\x00"
	for i, p := range pkgs {
		viol := m.violPkgs[p.ImportPath]
		// "\x00" prefixes the violations sentinel so it can never collide with a
		// real component that happens to be named "violations".
		group := p.Component
		if group == "" {
			group = "unassigned"
		}
		if viol {
			group = "\x00violations"
		}
		if group != lastGroup {
			header := "▸ " + strings.TrimPrefix(group, "\x00")
			if viol {
				lines = append(lines, styleBad.Render(header))
			} else {
				lines = append(lines, styleDim.Render(header))
			}
			lastGroup = group
		}
		label := "  " + m.short(p.ImportPath)
		switch {
		case i == sel && viol:
			label = styleSelectedBad.Render(label)
		case i == sel:
			label = styleSelected.Render(label)
		case viol:
			label = styleBad.Render(label)
		}
		if i == sel {
			selLine = len(lines)
		}
		lines = append(lines, label)
	}

	max := budget
	if max > 0 && len(lines) > max {
		if max -= 2; max < 1 { // leave room for the ▲/▼ markers
			max = 1
		}
	}
	start, end, above, below := window(len(lines), selLine, max)

	var out []string
	if above > 0 {
		out = append(out, moreLine("▲", above))
	}
	out = append(out, lines[start:end]...)
	if below > 0 {
		out = append(out, moreLine("▼", below))
	}
	return out
}

// packageDetail renders the selected package's detail pane: a path header over
// its outgoing imports and incoming importers. When max > 0 the import/importer
// list is truncated to fit with a "… N more" summary, so the pane keeps a bounded
// height instead of growing with the package's fan-out. The class legend is drawn
// separately, full-width, under both columns.
func (m Model) packageDetail(p core.PackageView, max int) []string {
	var content []string
	if len(p.Imports) == 0 {
		content = append(content, styleDim.Render("  (no imports)"))
	} else {
		content = append(content, styleDim.Render("imports:"))
		for _, iv := range p.Imports {
			content = append(content, m.renderImport(p.ImportPath, iv))
		}
	}
	if len(p.Importers) > 0 {
		content = append(content, styleDim.Render("imported by:"))
		for _, imp := range p.Importers {
			content = append(content, "    "+m.short(imp))
		}
	}

	// Reserve one row for the path header; the rest is for content.
	if max > 0 {
		room := max - 1
		if room < 1 {
			room = 1
		}
		if len(content) > room {
			keep := room - 1
			if keep < 0 {
				keep = 0
			}
			content = append(content[:keep:keep], moreLine("…", len(content)-keep))
		}
	}

	out := []string{styleDim.Render("── " + p.ImportPath + " ──")}
	return append(out, content...)
}

// renderImport shows one outgoing edge: the import path, a [class] or
// [component] tag, a test marker, and a red ✗ prefix when the edge violates a
// rule.
func (m Model) renderImport(from string, iv core.ImportView) string {
	tag := iv.Class.String()
	if iv.Class == core.ClassInModule {
		if iv.Component != "" {
			tag = iv.Component
		} else {
			tag = "unassigned"
		}
	}
	line := iv.Path + "  " + styleDim.Render("["+tag+"]")
	if iv.TestOnly {
		line += styleDim.Render(" [test]")
	}
	if m.violEdges[[2]string{from, iv.Path}] {
		return styleBad.Render("  ✗ ") + line
	}
	return "    " + line
}

// configLines renders the Config tab's document: the active config path header
// followed by the compiled rule set (report.RuleSet — the same content as
// `depdog config`). It is a static block of lines the view then windows; there
// is no selection here, only a scroll offset.
func (m Model) configLines() []string {
	pathLabel := m.configRel
	if pathLabel == "" {
		pathLabel = "(config path unknown)"
	}
	lines := []string{styleDim.Render("config: ") + pathLabel, ""}
	if m.rules == nil {
		return append(lines, styleDim.Render("no compiled rule set available — restart with `depdog tui`"))
	}
	var buf strings.Builder
	if err := report.RuleSet(&buf, m.rules); err != nil {
		return append(lines, styleBad.Render("failed to render the rule set: "+err.Error()))
	}
	dump := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	return append(lines, dump...)
}

// configLineCount is how many lines the Config document occupies — the clamp
// bound for its scroll offset.
func (m Model) configLineCount() int { return len(m.configLines()) }

// clampScroll bounds a document scroll offset to [0, maxOffset], where maxOffset
// is the deepest offset that still fills the window. budget <= 0 (unsized) or a
// document that fits pins the offset at 0.
func clampScroll(off, n, budget int) int {
	if off < 0 {
		return 0
	}
	if budget <= 0 || n <= budget {
		return 0
	}
	// Reserve two rows for the ▲/▼ markers, matching what configView renders.
	visible := budget - 2
	if visible < 1 {
		visible = 1
	}
	if max := n - visible; off > max {
		return max
	}
	return off
}

// configView renders the Config document into the height-aware window, with the
// existing `▲/▼ N more` markers when it is taller than the screen. It is a
// document (scroll offset), not a list (no selection).
func (m Model) configView() string {
	lines := m.configLines()
	budget := m.bodyRows()
	if budget <= 0 || len(lines) <= budget {
		return strings.Join(lines, "\n")
	}

	visible := budget - 2 // leave room for the ▲/▼ markers
	if visible < 1 {
		visible = 1
	}
	off := clampScroll(m.configScroll, len(lines), budget)
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

// short trims the module path prefix for readable package labels.
func (m Model) short(path string) string {
	mod := m.res.ModulePath
	switch {
	case path == mod:
		return "."
	case strings.HasPrefix(path, mod+"/"):
		return strings.TrimPrefix(path, mod+"/")
	default:
		return path
	}
}
