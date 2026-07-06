package tui

import (
	"fmt"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

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
	for i, v := range m.res.Violations {
		line := fmt.Sprintf("%s → %s", v.FromComponent, v.ImportPath)
		if i == m.selected {
			b.WriteString(styleSelected.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
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

	lastComp := "\x00"
	for i, p := range m.pkgs {
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
