package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// Explain describes a single component or package. For a component it shows the
// rule that constrains it, its member packages and its violations; for a
// package it shows its component, its classified imports (with the rule and
// file:line for any that violate) and its importers. Output is plain text and
// deterministic.
func Explain(w io.Writer, target string, rs *core.RuleSet, views []core.PackageView, res *core.Result) error {
	for _, c := range rs.Components {
		if c.Name == target {
			return explainComponent(w, c, rs, res, views)
		}
	}
	if pv, ok := findPackage(views, target, res.ModulePath); ok {
		return explainPackage(w, pv, res)
	}
	return fmt.Errorf("no component or package matches %q — pass a component name or an import path", target)
}

// ExplainEdge answers whether the package `from` may import `to`, and which rule
// or policy decides it. `to` may be a package, a component name, one of std/
// external/unassigned, or a specific external module by import path.
func ExplainEdge(w io.Writer, from, to string, rs *core.RuleSet, views []core.PackageView, res *core.Result) error {
	pv, ok := findPackage(views, from, res.ModulePath)
	if !ok {
		return fmt.Errorf("no package matches %q", from)
	}
	target, isModule, err := resolveTarget(to, rs, views, res.ModulePath)
	if err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) → %s (%s)\n", pv.ImportPath, orUnassigned(pv.Component), to, target)
	if pv.Component == "" {
		b.WriteString("  the source package is unassigned — no rule governs its imports\n")
		_, werr := io.WriteString(w, b.String())
		return werr
	}
	var (
		allowed bool
		reason  string
	)
	if isModule {
		allowed, reason = rs.DecideModule(pv.Component, to)
	} else {
		allowed, reason = rs.Decide(pv.Component, target)
	}
	verdict := "denied"
	if allowed {
		verdict = "allowed"
	}
	fmt.Fprintf(&b, "  %s by %s\n", verdict, reason)
	_, werr := io.WriteString(w, b.String())
	return werr
}

// resolveTarget maps a `to` argument to a rule target: a component name, one of
// std/external/unassigned, or (isModule) a specific external module. A package
// resolves to its component.
func resolveTarget(to string, rs *core.RuleSet, views []core.PackageView, module string) (target string, isModule bool, err error) {
	switch to {
	case "std", "external", "unassigned":
		return to, false, nil
	}
	for _, c := range rs.Components {
		if c.Name == to {
			return to, false, nil
		}
	}
	if pv, ok := findPackage(views, to, module); ok {
		return orUnassigned(pv.Component), false, nil
	}
	if strings.ContainsAny(to, "/.") {
		return "external module", true, nil
	}
	return "", false, fmt.Errorf("cannot resolve %q to a component, package, module, or std/external", to)
}

func explainComponent(w io.Writer, c core.Component, rs *core.RuleSet, res *core.Result, views []core.PackageView) error {
	var b strings.Builder
	fmt.Fprintf(&b, "component %s\n", c.Name)
	fmt.Fprintf(&b, "  patterns: %s\n", strings.Join(c.Patterns, ", "))
	fmt.Fprintf(&b, "  stance:   %s\n", stanceName(rs.Stance(c.Name)))

	rule, ok := rs.Rules[c.Name]
	switch {
	case !ok || (len(rule.Allow) == 0 && len(rule.Deny) == 0):
		fmt.Fprintf(&b, "  rule:     none — imports fall back to policy %s\n", policyName(rs.Policy))
	default:
		if len(rule.Allow) > 0 {
			fmt.Fprintf(&b, "  allow:    %s\n", refList(rule.Allow))
		}
		if len(rule.Deny) > 0 {
			fmt.Fprintf(&b, "  deny:     %s\n", refList(rule.Deny))
		}
	}

	var members []string
	for _, pv := range views {
		if pv.Component == c.Name {
			members = append(members, pv.ImportPath)
		}
	}
	sort.Strings(members)
	fmt.Fprintf(&b, "  packages (%d):\n", len(members))
	for _, m := range members {
		fmt.Fprintf(&b, "    %s\n", m)
	}

	var vs []core.Violation
	for _, v := range res.Violations {
		if v.FromComponent == c.Name {
			vs = append(vs, v)
		}
	}
	if len(vs) == 0 {
		b.WriteString("  violations: none\n")
	} else {
		fmt.Fprintf(&b, "  violations (%d):\n", len(vs))
		for _, v := range vs {
			fmt.Fprintf(&b, "    %s → %s  (%s)\n", v.FromPackage, v.ImportPath, v.Rule)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func explainPackage(w io.Writer, pv core.PackageView, res *core.Result) error {
	byEdge := make(map[[2]string]core.Violation, len(res.Violations))
	for _, v := range res.Violations {
		byEdge[[2]string{v.FromPackage, v.ImportPath}] = v
	}

	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", pv.ImportPath)
	fmt.Fprintf(&b, "  component: %s\n", orUnassigned(pv.Component))
	fmt.Fprintf(&b, "  imports (%d):\n", len(pv.Imports))
	for _, iv := range pv.Imports {
		tag := iv.Class.String()
		if iv.Class == core.ClassInModule {
			tag = orUnassigned(iv.Component)
		}
		test := ""
		if iv.TestOnly {
			test = " [test]"
		}
		if v, ok := byEdge[[2]string{pv.ImportPath, iv.Path}]; ok {
			fmt.Fprintf(&b, "    ✗ %s  [%s]%s  (%s)\n", iv.Path, tag, test, v.Rule)
			for _, p := range v.Positions {
				fmt.Fprintf(&b, "        %s:%d\n", p.File, p.Line)
			}
		} else {
			fmt.Fprintf(&b, "      %s  [%s]%s\n", iv.Path, tag, test)
		}
	}
	if len(pv.Importers) > 0 {
		fmt.Fprintf(&b, "  imported by (%d):\n", len(pv.Importers))
		for _, imp := range pv.Importers {
			fmt.Fprintf(&b, "    %s\n", imp)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// findPackage resolves a target to a package: by exact import path, by
// module-relative path, or by a unique trailing segment match.
func findPackage(views []core.PackageView, target, module string) (core.PackageView, bool) {
	rel := module + "/" + target
	for _, pv := range views {
		if pv.ImportPath == target || pv.ImportPath == rel {
			return pv, true
		}
	}
	for _, pv := range views {
		if strings.HasSuffix(pv.ImportPath, "/"+target) {
			return pv, true
		}
	}
	return core.PackageView{}, false
}

func policyName(p core.Policy) string {
	if p == core.PolicyAllow {
		return "allow"
	}
	return "deny"
}

// stanceName describes a fallback policy in whitelist/blacklist terms, matching
// how the rules read.
func stanceName(p core.Policy) string {
	if p == core.PolicyAllow {
		return "blacklist (all pass except denied)"
	}
	return "whitelist (only allowed pass)"
}

func refList(refs []core.Ref) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
