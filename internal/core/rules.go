package core

import (
	"fmt"
	"slices"
	"strings"
)

// RefKind distinguishes the things a rule may reference.
type RefKind int

const (
	RefComponent RefKind = iota
	RefStd
	RefExternal
	RefUnassigned
	RefAny
)

// Ref is a single entry of an allow or deny list.
type Ref struct {
	Kind RefKind
	Name string // component name when Kind == RefComponent
}

func (r Ref) String() string {
	switch r.Kind {
	case RefStd:
		return "std"
	case RefExternal:
		return "external"
	case RefUnassigned:
		return "unassigned"
	case RefAny:
		return "*"
	default:
		return r.Name
	}
}

// Rule constrains the outgoing imports of one component. Deny wins over
// Allow; edges neither list mentions fall back to the policy.
type Rule struct {
	Allow []Ref
	Deny  []Ref
}

// Policy is the fallback for edges no rule mentions: PolicyDeny gives
// whitelist semantics, PolicyAllow blacklist semantics.
type Policy int

const (
	PolicyDeny Policy = iota
	PolicyAllow
)

// TestFileMode controls how imports that appear only in _test.go files are
// treated.
type TestFileMode int

const (
	// TestHybrid allows test-only imports of external modules while still
	// enforcing component-to-component rules.
	TestHybrid TestFileMode = iota
	TestSameRules
	TestRelaxed
)

// Component is a named set of packages, defined by path patterns relative
// to the module root.
type Component struct {
	Name     string
	Patterns []string
}

// RuleSet is the compiled form of a depdog config. Components must be
// sorted by name for deterministic evaluation.
type RuleSet struct {
	Components []Component
	Rules      map[string]Rule
	Policy     Policy
	TestFiles  TestFileMode
	Skip       []string // package-dir patterns excluded from analysis
}

// AmbiguityError reports a package matched by equally specific patterns of
// different components.
type AmbiguityError struct {
	RelDir     string
	Components []string
}

func (e *AmbiguityError) Error() string {
	return fmt.Sprintf("package %q matches components %s equally well — make one pattern more specific",
		e.RelDir, strings.Join(e.Components, " and "))
}

// AssignComponent resolves which component owns the package at relDir. The
// most specific matching pattern wins; "" means the package is unassigned.
func (rs *RuleSet) AssignComponent(relDir string) (string, error) {
	var (
		found    bool
		best     string
		bestSpec specificity
		ties     []string
	)
	for _, c := range rs.Components {
		for _, pat := range c.Patterns {
			ok, err := MatchPattern(pat, relDir)
			if err != nil {
				return "", fmt.Errorf("component %q pattern %q: %w", c.Name, pat, err)
			}
			if !ok {
				continue
			}
			spec := patternSpecificity(pat)
			if !found {
				found, best, bestSpec = true, c.Name, spec
				continue
			}
			switch cmp := spec.compare(bestSpec); {
			case cmp > 0:
				best, bestSpec, ties = c.Name, spec, nil
			case cmp == 0 && c.Name != best && !slices.Contains(ties, c.Name):
				ties = append(ties, c.Name)
			}
		}
	}
	if ties != nil {
		return "", &AmbiguityError{RelDir: relDir, Components: append([]string{best}, ties...)}
	}
	if !found {
		return "", nil
	}
	return best, nil
}

// Stance reports the fallback for a component's edges that neither its allow
// nor its deny list mentions. The stance is inferred from the rule's word
// choice: an allow list makes the component a whitelist (PolicyDeny — only
// listed imports pass); a deny-only rule makes it a blacklist (PolicyAllow —
// everything passes except what is listed). A component with no rule, or a rule
// with neither list, falls back to the global policy. An explicit deny always
// wins regardless of stance.
func (rs *RuleSet) Stance(component string) Policy {
	r, ok := rs.Rules[component]
	switch {
	case ok && len(r.Allow) > 0:
		return PolicyDeny
	case ok && len(r.Deny) > 0:
		return PolicyAllow
	default:
		return rs.Policy
	}
}

// Skipped reports whether the package dir is excluded from analysis.
func (rs *RuleSet) Skipped(relDir string) bool {
	for _, pat := range rs.Skip {
		if ok, err := MatchPattern(pat, relDir); err == nil && ok {
			return true
		}
	}
	return false
}
