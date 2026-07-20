package core

import (
	"fmt"
	"strings"
)

// Explain is the input to Explanation: everything the prose generator needs to
// phrase one denied edge as a plain-English WHY plus an actionable fix, drawn
// from the decided verdict and its rule/boundary metadata. It is deliberately
// decoupled from RuleSet so every render surface (JSON, text explain, github,
// LSP hover, MCP) can populate it from whatever it already has in hand, and so
// the generator stays pure and trivially table-testable.
//
// The vocabulary is language-agnostic (components, boundaries, std/external):
// no language-specific knowledge belongs here.
type Explain struct {
	// From and To are the display labels for the two endpoints (an import path,
	// a component name, a module-relative dir — whatever the surface shows).
	From string
	To   string

	// Kind selects the template. For ReasonRule the stance/target fields below
	// drive the wording; for the boundary kinds the boundary fields do.
	Kind ReasonKind

	// FromComponent is the source component's name (the component whose rule
	// denied the edge). Empty is tolerated but the rule templates read best with
	// it set.
	FromComponent string

	// Allow and Deny are the source component's allow/deny refs, used to name the
	// concrete refs the fix should mention (never depdog jargon alone).
	Allow []Ref
	Deny  []Ref

	// GlobalDeny is the module-wide deny list, set only for the ReasonGlobalDeny
	// kind, so the prose can name the banned ref the top-level `deny` list carries.
	GlobalDeny []Ref

	// Stance is the source component's inferred fallback: PolicyDeny =
	// whitelist (only listed imports pass), PolicyAllow = blacklist (everything
	// passes except what is listed). It disambiguates the ReasonRule phrasings.
	Stance Policy

	// Target classifies the destination for the rule templates: a component
	// name, or one of "std" / "external" / "external module" / "unassigned".
	// TargetRef is the ref token the fix should name for the destination (e.g.
	// the component name, "std", or the module path); when empty it is derived
	// from Target.
	Target    string
	TargetRef string

	// Boundary is the boundary name for the boundary kinds. Peers are the other
	// member labels of that boundary (used to name the concrete peers a
	// cross-member deny involves); it may be empty when unavailable.
	Boundary string
	Peers    []string

	// SurfaceExports / SurfaceInternal are the target unit's declared surface
	// globs, and SurfaceInternalHit says whether the internal list (rather than
	// the exports whitelist) denied the edge. Cross-unit-surface kind only.
	SurfaceExports     []string
	SurfaceInternal    []string
	SurfaceInternalHit bool
}

// ExplainViolation builds the Explanation input for a decided Violation, drawn
// from the violation and the rule set that judged it, so every render surface
// (JSON, text, github, SARIF) phrases a denied edge identically without
// duplicating the population logic. The source component's allow/deny refs and
// inferred stance come from rs; for a boundary violation the boundary's peer
// member labels come from rs too. For an external destination TargetRef is the
// concrete import path so a module-scoped deny/allow can name the module.
func ExplainViolation(v Violation, rs *RuleSet) Explain {
	e := Explain{
		From:          v.FromPackage,
		To:            v.ImportPath,
		Kind:          v.Reason,
		FromComponent: v.FromComponent,
		Target:        v.Target,
		Boundary:      v.Boundary,
	}
	if rule, ok := rs.Rules[v.FromComponent]; ok {
		e.Allow, e.Deny = rule.Allow, rule.Deny
	}
	e.Stance = rs.Stance(v.FromComponent)
	if v.Target == "external" || v.Target == "external module" {
		e.TargetRef = v.ImportPath
	}
	if v.Reason == ReasonBoundary || v.Reason == ReasonBoundarySealed {
		e.Peers = rs.boundaryPeers(v.Boundary)
	}
	if v.Reason == ReasonGlobalDeny {
		e.GlobalDeny = rs.GlobalDeny
	}
	return e
}

// boundaryPeers returns the member labels of the named boundary, in the
// boundary's own sorted order, so a boundary explanation can name the concrete
// peers it involves. Empty when no boundary of that name is declared.
func (rs *RuleSet) boundaryPeers(name string) []string {
	for bi := range rs.Boundaries {
		if rs.Boundaries[bi].Name != name {
			continue
		}
		labels := make([]string, 0, len(rs.Boundaries[bi].Members))
		for _, m := range rs.Boundaries[bi].Members {
			labels = append(labels, m.Label)
		}
		return labels
	}
	return nil
}

// Explanation renders the prose WHY plus an actionable fix for one denied edge,
// templated per ReasonKind (see the plan's D2). It is pure and std-only: the
// same inputs always produce the same string, so it is safe to pin in goldens
// and to call from every render surface without drift.
//
// The wording always names concrete refs — the source component, the
// destination ref, the boundary and its peers — rather than depdog jargon
// alone, so a newcomer or an agent can act on it without knowing depdog's
// vocabulary.
func Explanation(e Explain) string {
	switch e.Kind {
	case ReasonBoundary:
		return boundaryExplanation(e)
	case ReasonBoundarySealed:
		return boundarySealedExplanation(e)
	case ReasonCrossUnit:
		return crossUnitExplanation(e)
	case ReasonCrossUnitBoundary:
		return crossUnitBoundaryExplanation(e)
	case ReasonCrossUnitBoundarySealed:
		return crossUnitBoundarySealedExplanation(e)
	case ReasonCrossUnitSurface:
		return crossUnitSurfaceExplanation(e)
	case ReasonGlobalDeny:
		return globalDenyExplanation(e)
	default:
		return ruleExplanation(e)
	}
}

// globalDenyExplanation phrases a module-wide ban: the destination is denied
// everywhere by the top-level `deny` list, regardless of the source component's
// own allow rule, so the fix is to drop the import or lift the ban — never to
// edit the component's allow list (which cannot override a global deny).
func globalDenyExplanation(e Explain) string {
	ref := e.globallyDeniedBy()
	return fmt.Sprintf(
		"`%s` is denied everywhere by the top-level `deny` list (which names `%s`), so no package — including `%s` (component `%s`) — may import it. "+
			"Fix: drop the import, or remove `%s` from the top-level `deny` list if the dependency is intended (a component allow list cannot override a global deny).",
		e.To, ref, e.From, e.FromComponent, ref)
}

// ruleExplanation phrases an ordinary component allow/deny/stance denial,
// branching on whether an explicit deny fired (blacklist), an allow-list gates
// the edge (whitelist), or the fail-closed default denied it.
func ruleExplanation(e Explain) string {
	comp := e.FromComponent
	targetRef := e.targetRef()
	targetDesc := e.targetDescription()

	// An explicit deny always wins, regardless of stance: if the destination is
	// named on the deny list, that is the reason to report.
	if denyRef, ok := e.deniedBy(); ok {
		return fmt.Sprintf(
			"`%s` (component `%s`) explicitly denies importing `%s` (its deny list names `%s`). "+
				"Fix: remove `%s` from `%s`'s deny list if the dependency is intended, or drop the import.",
			e.From, comp, e.To, denyRef, denyRef, comp)
	}

	// A whitelist component (allow list present, PolicyDeny fallback) permits
	// only its listed refs; name them and the fix that adds the destination.
	if e.Stance == PolicyDeny && len(e.Allow) > 0 {
		return fmt.Sprintf(
			"`%s` (component `%s`) may import only %s; `%s` (%s) is not among them. "+
				"Fix: add `%s` to `%s`'s allow list, or depend only on what `%s` already allows.",
			e.From, comp, refList(e.Allow), e.To, targetDesc, targetRef, comp, comp)
	}

	// Fail-closed: nothing is allowed by default (a bare default-deny policy, or
	// a component with no allow list under a deny stance). Common when the target
	// is unassigned.
	return fmt.Sprintf(
		"`%s` (component `%s`) has no rule permitting `%s` (%s), and the default is to deny. "+
			"Fix: add `%s` to `%s`'s allow list, or give `%s` a rule that allows this dependency.",
		e.From, comp, e.To, targetDesc, targetRef, comp, comp)
}

// boundaryExplanation phrases a cross-member boundary crossing (two distinct
// members of the same boundary may not import each other).
func boundaryExplanation(e Explain) string {
	peers := ""
	if names := formatPeers(e.Peers); names != "" {
		peers = fmt.Sprintf(" (its members include %s)", names)
	}
	return fmt.Sprintf(
		"`%s` and `%s` are peers in the `%s` boundary%s, which is mutually exclusive, so neither may import the other. "+
			"Fix: move the shared code into a component outside `%s`, or remove one member from the boundary.",
		e.From, e.To, e.Boundary, peers, e.Boundary)
}

// boundarySealedExplanation phrases a sealed-boundary denial (a source outside
// the boundary may not import inward to a member).
func boundarySealedExplanation(e Explain) string {
	return fmt.Sprintf(
		"the `%s` boundary is sealed — only its members may import inward. `%s` is outside `%s`, "+
			"so it may not import `%s` (a member). Fix: add `%s` to `%s`, or route through an allowed component.",
		e.Boundary, e.From, e.Boundary, e.To, e.From, e.Boundary)
}

// ExplainWorkViolation builds the Explanation input for a cross-unit
// violation, drawn from the violation and the work rules that judged it. The
// unit-level kinds populate the same rule/boundary fields ExplainViolation
// does (the work rule set's components are the units); the surface kind adds
// the target unit's surface globs and which list fired — recovered from the
// rule text EvaluateWork stamped, so verdict and explanation cannot drift.
func ExplainWorkViolation(v Violation, w *WorkRules) Explain {
	e := ExplainViolation(v, w.Rules)
	if v.Reason == ReasonCrossUnitSurface {
		s := w.Surfaces[v.Target]
		e.SurfaceExports = s.Exports
		e.SurfaceInternal = s.Internal
		e.SurfaceInternalHit = strings.HasPrefix(v.Rule, v.Target+": internal")
	}
	if v.Reason == ReasonCrossUnitBoundary || v.Reason == ReasonCrossUnitBoundarySealed {
		e.Peers = w.Rules.boundaryPeers(v.Boundary)
	}
	return e
}

// crossUnitExplanation phrases a unit-level rule/stance denial: the same three
// branches as ruleExplanation, in unit vocabulary and pointing the fix at
// depdog.work.yaml.
func crossUnitExplanation(e Explain) string {
	from, to := e.FromComponent, e.Target
	if denyRef, ok := e.deniedBy(); ok {
		return fmt.Sprintf(
			"unit `%s` explicitly denies depending on `%s` (its deny list in depdog.work.yaml names `%s`). "+
				"Fix: remove `%s` from `%s`'s deny list if the dependency is intended, or drop the reference to `%s`.",
			from, to, denyRef, denyRef, from, e.To)
	}
	if e.Stance == PolicyDeny && len(e.Allow) > 0 {
		return fmt.Sprintf(
			"unit `%s` may depend only on %s; unit `%s` is not among them. "+
				"Fix: add `%s` to `%s`'s allow list in depdog.work.yaml, or depend only on what `%s` already allows.",
			from, refList(e.Allow), to, to, from, from)
	}
	return fmt.Sprintf(
		"no rule permits unit `%s` to depend on unit `%s`, and the work file's default is to deny. "+
			"Fix: add `%s` to `%s`'s allow list in depdog.work.yaml.",
		from, to, to, from)
}

// crossUnitBoundaryExplanation phrases two units that are peers of the same
// work-file boundary depending on each other.
func crossUnitBoundaryExplanation(e Explain) string {
	peers := ""
	if names := formatPeers(e.Peers); names != "" {
		peers = fmt.Sprintf(" (its members include %s)", names)
	}
	return fmt.Sprintf(
		"units `%s` and `%s` are peers in the `%s` boundary%s, which is mutually exclusive, so neither may depend on the other. "+
			"Fix: extract the shared code into a unit outside `%s`, or remove one member from the boundary.",
		e.FromComponent, e.Target, e.Boundary, peers, e.Boundary)
}

// crossUnitBoundarySealedExplanation phrases a unit outside a sealed work-file
// boundary depending inward on a member.
func crossUnitBoundarySealedExplanation(e Explain) string {
	return fmt.Sprintf(
		"the `%s` boundary is sealed — only its member units may depend inward. `%s` is outside `%s`, "+
			"so it may not depend on `%s` (a member). Fix: add `%s` to `%s`, or route through an allowed unit.",
		e.Boundary, e.FromComponent, e.Boundary, e.Target, e.FromComponent, e.Boundary)
}

// crossUnitSurfaceExplanation phrases an edge that is allowed at unit level
// but reaches a sub-path the target unit does not expose: either it matched
// the internal list, or the exports whitelist does not cover it.
func crossUnitSurfaceExplanation(e Explain) string {
	if e.SurfaceInternalHit {
		exported := ""
		if len(e.SurfaceExports) > 0 {
			exported = fmt.Sprintf(" through its exported surface (%s)", surfaceList(e.SurfaceExports))
		}
		return fmt.Sprintf(
			"`%s` reaches into `%s`'s internal paths: `%s` matches %s, which `%s` declares internal. "+
				"Fix: depend on `%s`%s, or widen `%s`'s surface in depdog.work.yaml.",
			e.FromComponent, e.Target, e.To, surfaceList(e.SurfaceInternal), e.Target,
			e.Target, exported, e.Target)
	}
	return fmt.Sprintf(
		"`%s` may reach `%s` only through its exported surface (%s); `%s` is not exported. "+
			"Fix: depend on an exported path of `%s`, or add this path to `%s`'s exports in depdog.work.yaml.",
		e.FromComponent, e.Target, surfaceList(e.SurfaceExports), e.To, e.Target, e.Target)
}

// surfaceList renders surface globs as a backtick-quoted list for prose.
func surfaceList(globs []string) string {
	if len(globs) == 0 {
		return "nothing"
	}
	parts := make([]string, len(globs))
	for i, g := range globs {
		parts[i] = "`" + g + "`"
	}
	return strings.Join(parts, ", ")
}

// deniedBy reports the ref token of a deny entry that covers the destination,
// so an explicit-deny explanation can name the concrete ref. It matches the
// destination against the deny list the same way Decide/DecideModule do: by
// target classification, and by module prefix for an external-module target.
func (e Explain) deniedBy() (string, bool) {
	for _, r := range e.Deny {
		if e.refCoversTarget(r) {
			return e.denyRefName(r), true
		}
	}
	return "", false
}

// globallyDeniedBy names the module-wide deny entry that covers this edge's
// destination, so a global-deny explanation can point at the concrete ref. It
// falls back to the first entry (and finally a generic phrase) so the prose
// always names something even if the covering ref cannot be pinpointed.
func (e Explain) globallyDeniedBy() string {
	for _, r := range e.GlobalDeny {
		if e.refCoversTarget(r) {
			return e.denyRefName(r)
		}
	}
	// No entry pinpointed the destination — name the destination itself rather
	// than an arbitrary list entry, which in a security-ban context would be
	// actively misleading (naming a module that did not ban this edge).
	return e.targetRef()
}

// refCoversTarget reports whether ref r covers this edge's destination, given
// the Target classification (and, for an external-module target, the TargetRef
// module path). Mirrors refMatchesTarget / moduleRefMatches without needing an
// Import.
func (e Explain) refCoversTarget(r Ref) bool {
	switch r.Kind {
	case RefAny:
		return true
	case RefStd:
		return e.Target == "std"
	case RefExternal:
		return e.Target == "external" || e.Target == "external module"
	case RefUnassigned:
		return e.Target == "unassigned"
	case RefComponent:
		return e.Target != "std" && e.Target != "external" &&
			e.Target != "external module" && e.Target != "unassigned" &&
			r.Name == e.Target
	case RefExternalModule:
		if e.Target != "external" && e.Target != "external module" {
			return false
		}
		mod := e.TargetRef
		return mod == r.Name || strings.HasPrefix(mod, r.Name+"/")
	}
	return false
}

// denyRefName is the concrete token to name in a deny explanation: the ref's
// own string (component name / "std" / "external" / module prefix), except a
// bare "*" (RefAny) is spelled out against the destination for clarity.
func (e Explain) denyRefName(r Ref) string {
	if r.Kind == RefAny {
		return e.targetRef()
	}
	return r.String()
}

// targetRef is the token a fix should name for the destination: an explicit
// TargetRef when the surface supplied one, else derived from the Target
// classification.
func (e Explain) targetRef() string {
	if e.TargetRef != "" {
		return e.TargetRef
	}
	switch e.Target {
	case "external module":
		return "external"
	case "":
		return "unassigned"
	default:
		return e.Target
	}
}

// targetDescription is the parenthetical describing the destination's kind in
// prose: "std" / "external" / "external module" / "unassigned", or
// "component <name>" for a component target.
func (e Explain) targetDescription() string {
	switch e.Target {
	case "std":
		return "the standard library"
	case "external", "external module":
		// Both classify the destination as outside the module. The two spellings
		// are a resolution-path artifact (check emits "external"; explain/MCP
		// resolve a specific module and emit "external module"), so they must
		// read identically — the concrete module path is named via TargetRef.
		return "an external dependency"
	case "unassigned", "":
		return "an unassigned package"
	default:
		return "component `" + e.Target + "`"
	}
}

// refList renders a comma-separated, backtick-quoted list of refs for prose,
// e.g. "`domain`, `std`". Empty refs render as "nothing".
func refList(refs []Ref) string {
	if len(refs) == 0 {
		return "nothing"
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = "`" + r.String() + "`"
	}
	return strings.Join(parts, ", ")
}

// formatPeers renders the peer member labels as a backtick-quoted list for the
// boundary explanation, or "" when none are supplied.
func formatPeers(peers []string) string {
	if len(peers) == 0 {
		return ""
	}
	parts := make([]string, len(peers))
	for i, p := range peers {
		parts[i] = "`" + p + "`"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}
