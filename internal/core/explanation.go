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
	default:
		return ruleExplanation(e)
	}
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
	case "external":
		return "an external dependency"
	case "external module":
		return "an external module"
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
