package core

import (
	"fmt"
	"strings"
)

// Violation is one import edge that breaks a rule.
type Violation struct {
	FromPackage   string // import path of the offending package
	FromComponent string
	ImportPath    string // the offending import
	Target        string // component name, or "std" / "external" / "unassigned"
	Rule          string // human-readable rule that fired, e.g. `domain: allow [std]`
	TestOnly      bool
	Positions     []Position
}

// Warning flags an in-module package no component claims. Warnings never
// fail a check by themselves.
type Warning struct {
	Package string
	RelDir  string
}

type Stats struct {
	Packages int
	Edges    int
}

// ComponentStat is the per-component rollup the dashboard and JSON report
// present: how many packages map to the component, how many import edges leave
// them, and how many of those break a rule.
type ComponentStat struct {
	Name       string
	Packages   int
	Edges      int
	Violations int
}

// Result is everything a check run produces; reporters and the TUI consume
// this and nothing else.
type Result struct {
	ModulePath string
	Violations []Violation
	Warnings   []Warning
	Components []ComponentStat // one per declared component, sorted by name
	Stats      Stats
}

// Evaluate checks every import edge of the graph against the rule set.
// Ordering is deterministic given a sorted graph.
func Evaluate(g *Graph, rs *RuleSet) (*Result, error) {
	res := &Result{ModulePath: g.ModulePath}

	// One stat bucket per declared component (in rs.Components' sorted order),
	// so even components with no matching packages still appear.
	compStats := make(map[string]*ComponentStat, len(rs.Components))
	for _, c := range rs.Components {
		compStats[c.Name] = &ComponentStat{Name: c.Name}
	}

	// Components are resolved per directory, not per graph node, so import
	// targets outside the loaded package set still classify correctly.
	cache := make(map[string]string, len(g.Packages))
	assign := func(relDir string) (string, error) {
		if c, ok := cache[relDir]; ok {
			return c, nil
		}
		c, err := rs.AssignComponent(relDir)
		if err != nil {
			return "", err
		}
		cache[relDir] = c
		return c, nil
	}

	for _, p := range g.Packages {
		if rs.Skipped(p.RelDir) {
			continue
		}
		res.Stats.Packages++
		comp, err := assign(p.RelDir)
		if err != nil {
			return nil, err
		}
		if comp == "" {
			// No component means no rule to judge outgoing edges by; the
			// package is reported once instead of flooding the output.
			res.Warnings = append(res.Warnings, Warning{Package: p.ImportPath, RelDir: p.RelDir})
			continue
		}
		cs := compStats[comp]
		cs.Packages++
		rule, hasRule := rs.Rules[comp]
		for _, imp := range p.Imports {
			res.Stats.Edges++
			cs.Edges++

			targetComp := ""
			if imp.Class == ClassInModule {
				if rs.Skipped(imp.RelDir) {
					continue
				}
				if targetComp, err = assign(imp.RelDir); err != nil {
					return nil, err
				}
				if targetComp == comp {
					continue // imports within a component are always fine
				}
			}

			if imp.TestOnly {
				if rs.TestFiles == TestRelaxed {
					continue
				}
				if rs.TestFiles == TestHybrid && imp.Class == ClassExternal {
					continue
				}
			}

			if hasRule && matchAny(rule.Deny, imp, targetComp) {
				res.Violations = append(res.Violations,
					violation(p, comp, imp, targetComp, ruleText(comp, "deny", rule.Deny)))
				cs.Violations++
				continue
			}
			if hasRule && matchAny(rule.Allow, imp, targetComp) {
				continue
			}
			if rs.Policy == PolicyAllow {
				continue
			}
			text := "policy: deny"
			if hasRule && len(rule.Allow) > 0 {
				text = ruleText(comp, "allow", rule.Allow)
			}
			res.Violations = append(res.Violations, violation(p, comp, imp, targetComp, text))
			cs.Violations++
		}
	}

	for _, c := range rs.Components {
		res.Components = append(res.Components, *compStats[c.Name])
	}
	return res, nil
}

func violation(p Package, comp string, imp Import, targetComp, rule string) Violation {
	target := targetComp
	if imp.Class != ClassInModule {
		target = imp.Class.String()
	} else if target == "" {
		target = "unassigned"
	}
	return Violation{
		FromPackage:   p.ImportPath,
		FromComponent: comp,
		ImportPath:    imp.Path,
		Target:        target,
		Rule:          rule,
		TestOnly:      imp.TestOnly,
		Positions:     imp.Positions,
	}
}

func matchAny(refs []Ref, imp Import, targetComp string) bool {
	for _, r := range refs {
		if refMatches(r, imp, targetComp) {
			return true
		}
	}
	return false
}

func refMatches(r Ref, imp Import, targetComp string) bool {
	switch r.Kind {
	case RefAny:
		return true
	case RefStd:
		return imp.Class == ClassStd
	case RefExternal:
		return imp.Class == ClassExternal
	case RefUnassigned:
		return imp.Class == ClassInModule && targetComp == ""
	case RefComponent:
		return imp.Class == ClassInModule && targetComp == r.Name
	}
	return false
}

func ruleText(comp, verb string, refs []Ref) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.String()
	}
	return fmt.Sprintf("%s: %s [%s]", comp, verb, strings.Join(names, ", "))
}
