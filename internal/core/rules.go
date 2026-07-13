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
	// RefExternalModule matches a specific third-party module by import-path
	// prefix (depguard-style), e.g. "golang.org/x/sync".
	RefExternalModule
)

// Ref is a single entry of an allow or deny list.
type Ref struct {
	Kind RefKind
	Name string // component name (RefComponent) or module prefix (RefExternalModule)
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
	Skip       []string   // package-dir patterns excluded from analysis
	Boundaries []Boundary // mutual-exclusion groups, orthogonal to components; sorted by name
	// Lang, when non-empty, is the language adapter pinned by the config's
	// optional `lang:` key. It is carried opaquely — core attaches no meaning to
	// the string; the CLI (which owns the adapter registry) validates and
	// resolves it. Empty means "auto-detect the adapter".
	Lang string
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

// Decide reports whether a component may import the given target, and the rule
// or policy that decides it. target is a component name or one of "std",
// "external", "unassigned". An import within the same component is always
// allowed. This is the per-edge decision `explain` reports; it mirrors Evaluate.
func (rs *RuleSet) Decide(component, target string) (allowed bool, reason string) {
	if target == component {
		return true, "same component"
	}
	rule, hasRule := rs.Rules[component]
	if hasRule {
		for _, r := range rule.Deny {
			if refMatchesTarget(r, target) {
				return false, ruleText(component, "deny", rule.Deny)
			}
		}
		for _, r := range rule.Allow {
			if refMatchesTarget(r, target) {
				return true, ruleText(component, "allow", rule.Allow)
			}
		}
	}
	return rs.fallback(component, rule, hasRule)
}

// DecideModule reports whether a component may import a specific external module
// (by import path), and the rule or policy that decides it. Used by `explain`
// when the target is a bare module path.
func (rs *RuleSet) DecideModule(component, module string) (allowed bool, reason string) {
	rule, hasRule := rs.Rules[component]
	if hasRule {
		for _, r := range rule.Deny {
			if moduleRefMatches(r, module) {
				return false, ruleText(component, "deny", rule.Deny)
			}
		}
		for _, r := range rule.Allow {
			if moduleRefMatches(r, module) {
				return true, ruleText(component, "allow", rule.Allow)
			}
		}
	}
	return rs.fallback(component, rule, hasRule)
}

// fallback resolves an edge no allow/deny entry matched, using the component's
// inferred stance.
func (rs *RuleSet) fallback(component string, rule Rule, hasRule bool) (bool, string) {
	if rs.Stance(component) == PolicyAllow {
		if hasRule && len(rule.Deny) > 0 {
			return true, "not denied by " + ruleText(component, "deny", rule.Deny)
		}
		return true, "default: allow"
	}
	if hasRule && len(rule.Allow) > 0 {
		return false, ruleText(component, "allow", rule.Allow)
	}
	return false, "default: deny"
}

func refMatchesTarget(r Ref, target string) bool {
	switch r.Kind {
	case RefAny:
		return true
	case RefStd:
		return target == "std"
	case RefExternal:
		return target == "external"
	case RefUnassigned:
		return target == "unassigned"
	case RefComponent:
		return r.Name == target
	}
	return false
}

// moduleRefMatches reports whether a ref covers a specific external module: "*"
// and "external" cover any, and an external-module ref matches by prefix.
func moduleRefMatches(r Ref, module string) bool {
	switch r.Kind {
	case RefAny, RefExternal:
		return true
	case RefExternalModule:
		return module == r.Name || strings.HasPrefix(module, r.Name+"/")
	}
	return false
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
