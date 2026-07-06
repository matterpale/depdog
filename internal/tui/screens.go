package tui

import (
	"fmt"
	"strings"
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
