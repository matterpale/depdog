package core

import (
	"fmt"
	"strings"
)

// ReasonKind classifies why an edge was flagged, so a boundary verdict and its
// sealed variant survive into JSON without string-parsing the Rule field. The
// empty kind is an ordinary component allow/deny/stance violation and is omitted
// from JSON.
type ReasonKind string

const (
	ReasonRule           ReasonKind = ""                // ordinary component allow/deny/stance
	ReasonBoundary       ReasonKind = "boundary"        // a cross-member boundary crossing
	ReasonBoundarySealed ReasonKind = "boundary-sealed" // ungrouped source → in-member target under sealed
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
	Reason        ReasonKind // "" for ordinary rule violations; a boundary kind otherwise
	Boundary      string     // boundary name when Reason is a boundary kind; "" otherwise
	Severity      Severity   // SeverityError (fails the build) unless the source component/boundary opted into warn
}

// WarningKind distinguishes the advisory notes a check surfaces.
const (
	WarnUnassigned          = "unassigned"            // an in-module package no component claims
	WarnEmptyComponent      = "empty-component"       // a component whose patterns match no package
	WarnEmptyBoundaryMember = "empty-boundary-member" // a glob boundary member matching no package
)

// Warning is an advisory note that never fails a check by itself. Its fields
// depend on Kind: WarnUnassigned carries Package and RelDir; WarnEmptyComponent
// carries Component; WarnEmptyBoundaryMember carries Boundary and Component (the
// member label).
type Warning struct {
	Kind      string
	Package   string
	RelDir    string
	Component string
	Boundary  string
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
	Cycles     [][]string      // components forming an import cycle; each sorted
	Stats      Stats
}

// ErrorCount returns how many violations are error-severity — the count that
// decides the exit code. A tree whose only violations are warnings has
// ErrorCount() == 0 and so exits clean.
func (r *Result) ErrorCount() int {
	n := 0
	for _, v := range r.Violations {
		if v.Severity == SeverityError {
			n++
		}
	}
	return n
}

// Evaluate checks every import edge of the graph against the rule set.
// Ordering is deterministic given a sorted graph.
func Evaluate(g *Graph, rs *RuleSet) (*Result, error) {
	res := &Result{ModulePath: g.ModulePath}

	// One stat bucket per declared component (in rs.Components' sorted order),
	// so even components with no matching packages still appear.
	compStats := make(map[string]*ComponentStat, len(rs.Components))
	sevByComp := make(map[string]Severity, len(rs.Components))
	for _, c := range rs.Components {
		compStats[c.Name] = &ComponentStat{Name: c.Name}
		sevByComp[c.Name] = c.Severity
	}

	// Component adjacency, for detecting architecture-level import cycles that
	// go vet cannot (a package cycle is illegal Go, but a component cycle can
	// span several acyclic package edges).
	compEdges := make(map[string]map[string]bool, len(rs.Components))

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

	// Boundary membership is resolved per directory too, independently of
	// component assignment (the two axes are orthogonal). A nil slice means the
	// config declares no boundaries.
	memCache := make(map[string][]int, len(g.Packages))
	membership := func(relDir string) ([]int, error) {
		if m, ok := memCache[relDir]; ok {
			return m, nil
		}
		m, err := rs.BoundaryMembership(relDir)
		if err != nil {
			return nil, err
		}
		memCache[relDir] = m
		return m, nil
	}

	// Track which boundary members ever matched a package, so glob members that
	// claim nothing surface as an advisory (mirrors WarnEmptyComponent). Keyed
	// by boundary index → member index.
	memberSeen := make(map[[2]int]bool)
	recordSeen := func(m []int) {
		for bi, mi := range m {
			if mi >= 0 {
				memberSeen[[2]int{bi, mi}] = true
			}
		}
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
		srcMembership, err := membership(p.RelDir)
		if err != nil {
			return nil, err
		}
		recordSeen(srcMembership)

		// Boundary membership is independent of component assignment: even a
		// package no component claims may sit inside a boundary member, and its
		// outgoing edges must still be checked (a sealed boundary forbids an
		// ungrouped source from importing in). So when the package is
		// component-unassigned we still walk its imports for the boundary gate,
		// but only emit the single unassigned warning and skip component rules.
		var (
			cs                  *ComponentStat
			rule                Rule
			hasRule, unassigned bool
			stance              Policy
		)
		if comp == "" {
			// No component means no rule to judge outgoing edges by; the
			// package is reported once instead of flooding the output.
			res.Warnings = append(res.Warnings, Warning{Kind: WarnUnassigned, Package: p.ImportPath, RelDir: p.RelDir})
			unassigned = true
		} else {
			cs = compStats[comp]
			cs.Packages++
			rule, hasRule = rs.Rules[comp]
			stance = rs.Stance(comp)
		}

		for _, imp := range p.Imports {
			// Edge stats count every import of a component-assigned source,
			// exactly as before; unassigned sources never counted their edges.
			if !unassigned {
				res.Stats.Edges++
				cs.Edges++
			}

			targetComp := ""
			if imp.Class == ClassInModule {
				if rs.Skipped(imp.RelDir) {
					continue
				}
				if targetComp, err = assign(imp.RelDir); err != nil {
					return nil, err
				}
			}

			// The boundary gate runs for every source (component-assigned or
			// not), honouring the same test_files relaxation as component rules.
			testExempt := false
			if imp.TestOnly {
				if rs.TestFiles == TestRelaxed {
					testExempt = true
				} else if rs.TestFiles == TestHybrid && imp.Class == ClassExternal {
					testExempt = true
				}
			}
			if !testExempt && imp.Class == ClassInModule && len(rs.Boundaries) > 0 {
				tgtMembership, merr := membership(imp.RelDir)
				if merr != nil {
					return nil, merr
				}
				if bv := rs.boundaryVerdict(p, imp, comp, targetComp, srcMembership, tgtMembership); bv != nil {
					res.Violations = append(res.Violations, *bv)
					if cs != nil {
						cs.Violations++
					}
					continue
				}
			}

			// Component rules only apply to a component-assigned source.
			if unassigned {
				continue
			}

			if imp.Class == ClassInModule && targetComp == comp {
				continue // imports within a component are always fine
			}
			if targetComp != "" {
				edge := compEdges[comp]
				if edge == nil {
					edge = map[string]bool{}
					compEdges[comp] = edge
				}
				edge[targetComp] = true
			}

			if testExempt {
				continue
			}

			if hasRule && matchAny(rule.Deny, imp, targetComp) {
				res.Violations = append(res.Violations,
					violation(p, comp, imp, targetComp, ruleText(comp, "deny", rule.Deny), sevByComp[comp]))
				cs.Violations++
				continue
			}
			if hasRule && matchAny(rule.Allow, imp, targetComp) {
				continue
			}
			if stance == PolicyAllow {
				continue // blacklist fallback: unmentioned edges pass
			}
			text := "default: deny"
			if hasRule && len(rule.Allow) > 0 {
				text = ruleText(comp, "allow", rule.Allow)
			}
			res.Violations = append(res.Violations, violation(p, comp, imp, targetComp, text, sevByComp[comp]))
			cs.Violations++
		}
	}

	res.Cycles = componentCycles(compEdges)

	for _, c := range rs.Components {
		stat := compStats[c.Name]
		res.Components = append(res.Components, *stat)
		if stat.Packages == 0 {
			// A component whose patterns claimed no package is usually a typo
			// or dead pattern; surface it without failing the build.
			res.Warnings = append(res.Warnings, Warning{Kind: WarnEmptyComponent, Component: c.Name})
		}
	}

	// A glob boundary member that matched no package is likely a typo or dead
	// glob; surface it (component members are already covered by the
	// empty-component warning). Deterministic: boundaries and members sorted.
	for bi := range rs.Boundaries {
		b := &rs.Boundaries[bi]
		for mi := range b.Members {
			if b.Members[mi].Component != "" {
				continue // component members ride the empty-component warning
			}
			if !memberSeen[[2]int{bi, mi}] {
				res.Warnings = append(res.Warnings, Warning{
					Kind: WarnEmptyBoundaryMember, Boundary: b.Name, Component: b.Members[mi].Label,
				})
			}
		}
	}
	return res, nil
}

// boundaryVerdict applies the boundary gate to one in-module edge and returns
// the single violation it produces, or nil when no boundary denies. When an
// edge crosses multiple boundaries the first denying boundary (in sorted order)
// wins, so exactly one violation is emitted per edge for determinism. A
// boundary crossing is a hard deny that wins over any component allow, so this
// is checked before the component decision.
func (rs *RuleSet) boundaryVerdict(p Package, imp Import, comp, targetComp string, src, tgt []int) *Violation {
	for bi := range rs.Boundaries {
		b := rs.Boundaries[bi]
		deny, sealed := crossesBoundary(b, src[bi], tgt[bi])
		if !deny {
			continue
		}
		reason := ReasonBoundary
		rule := fmt.Sprintf("denied by boundary %q", b.Name)
		if sealed {
			reason = ReasonBoundarySealed
			rule = fmt.Sprintf("denied by boundary %q (sealed)", b.Name)
		}
		v := violation(p, comp, imp, targetComp, rule, b.Severity)
		v.Reason = reason
		v.Boundary = b.Name
		return &v
	}
	return nil
}

func violation(p Package, comp string, imp Import, targetComp, rule string, sev Severity) Violation {
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
		Severity:      sev,
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
	case RefExternalModule:
		return imp.Class == ClassExternal &&
			(imp.Path == r.Name || strings.HasPrefix(imp.Path, r.Name+"/"))
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
