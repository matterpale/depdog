package tui

import (
	"fmt"
	"strings"

	"github.com/matterpale/depdog/internal/core"
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
	start, end, above, below := window(len(m.res.Violations), m.selected, m.listRows())
	if above > 0 {
		b.WriteString(moreLine("▲", above) + "\n")
	}
	for i := start; i < end; i++ {
		v := m.res.Violations[i]
		line := fmt.Sprintf("%s → %s", v.FromComponent, v.ImportPath)
		if i == m.selected {
			b.WriteString(styleSelected.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	if below > 0 {
		b.WriteString(moreLine("▼", below) + "\n")
	}

	v := m.res.Violations[m.selected]
	b.WriteString("\n")
	b.WriteString(styleDim.Render("── detail ──"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s imports %s\n", v.FromPackage, styleBad.Render(v.ImportPath)))
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
	var b strings.Builder

	start, end, above, below := window(len(m.pkgs), m.selPkg, m.listRows())
	if above > 0 {
		b.WriteString(moreLine("▲", above) + "\n")
	}
	lastComp := "\x00"
	for i := start; i < end; i++ {
		p := m.pkgs[i]
		comp := p.Component
		if comp == "" {
			comp = "unassigned"
		}
		if comp != lastComp {
			b.WriteString(styleDim.Render("▸ "+comp) + "\n")
			lastComp = comp
		}
		name := "  " + m.short(p.ImportPath)
		if i == m.selPkg {
			b.WriteString(styleSelected.Render(name))
		} else {
			b.WriteString(name)
		}
		b.WriteString("\n")
	}
	if below > 0 {
		b.WriteString(moreLine("▼", below) + "\n")
	}

	p := m.pkgs[m.selPkg]
	b.WriteString("\n")
	b.WriteString(styleDim.Render("── " + p.ImportPath + " ──"))
	b.WriteString("\n")
	if len(p.Imports) == 0 {
		b.WriteString(styleDim.Render("  (no imports)") + "\n")
	} else {
		b.WriteString(styleDim.Render("imports:") + "\n")
		for _, iv := range p.Imports {
			b.WriteString(m.renderImport(p.ImportPath, iv) + "\n")
		}
	}
	if len(p.Importers) > 0 {
		b.WriteString(styleDim.Render("imported by:") + "\n")
		for _, imp := range p.Importers {
			b.WriteString("    " + m.short(imp) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
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
