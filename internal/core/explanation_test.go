package core

import (
	"strings"
	"testing"
)

// TestExplanation exercises the prose generator across every ReasonKind,
// every ReasonRule stance (whitelist / blacklist / default-deny), every target
// classification (component / std / external / external-module / unassigned),
// and both boundary shapes (sealed vs cross-member, including multi-member
// peers). Each case asserts the WHY and the actionable Fix are present and that
// the concrete refs are named (never depdog jargon alone), and pins the exact
// deterministic string.
func TestExplanation(t *testing.T) {
	tests := []struct {
		name string
		in   Explain
		// wantContains: substrings that must appear (the load-bearing refs and
		// the fix clause).
		wantContains []string
		// want: the exact deterministic string (pins the wording).
		want string
	}{
		{
			name: "rule/whitelist/component target",
			in: Explain{
				From:          "m/internal/handler/checkout",
				To:            "m/internal/repository",
				Kind:          ReasonRule,
				FromComponent: "handler",
				Allow:         []Ref{{Kind: RefComponent, Name: "domain"}, {Kind: RefStd}},
				Stance:        PolicyDeny,
				Target:        "repository",
			},
			wantContains: []string{"handler", "`domain`, `std`", "not among them", "Fix:", "add `repository`"},
			want: "`m/internal/handler/checkout` (component `handler`) may import only `domain`, `std`; " +
				"`m/internal/repository` (component `repository`) is not among them. " +
				"Fix: add `repository` to `handler`'s allow list, or depend only on what `handler` already allows.",
		},
		{
			name: "rule/whitelist/std target",
			in: Explain{
				From:          "m/internal/domain/order",
				To:            "unsafe",
				Kind:          ReasonRule,
				FromComponent: "domain",
				Allow:         []Ref{{Kind: RefComponent, Name: "shared"}},
				Stance:        PolicyDeny,
				Target:        "std",
			},
			wantContains: []string{"domain", "standard library", "Fix:", "add `std`"},
			want: "`m/internal/domain/order` (component `domain`) may import only `shared`; " +
				"`unsafe` (the standard library) is not among them. " +
				"Fix: add `std` to `domain`'s allow list, or depend only on what `domain` already allows.",
		},
		{
			name: "rule/whitelist/external target",
			in: Explain{
				From:          "m/internal/domain/order",
				To:            "github.com/some/pkg",
				Kind:          ReasonRule,
				FromComponent: "domain",
				Allow:         []Ref{{Kind: RefStd}},
				Stance:        PolicyDeny,
				Target:        "external",
			},
			wantContains: []string{"domain", "external dependency", "Fix:", "add `external`"},
			want: "`m/internal/domain/order` (component `domain`) may import only `std`; " +
				"`github.com/some/pkg` (an external dependency) is not among them. " +
				"Fix: add `external` to `domain`'s allow list, or depend only on what `domain` already allows.",
		},
		{
			name: "rule/whitelist/external-module target",
			in: Explain{
				From:          "m/internal/handler/checkout",
				To:            "github.com/evil/mod",
				Kind:          ReasonRule,
				FromComponent: "handler",
				Allow:         []Ref{{Kind: RefExternalModule, Name: "golang.org/x/sync"}},
				Stance:        PolicyDeny,
				Target:        "external module",
				TargetRef:     "github.com/evil/mod",
			},
			wantContains: []string{"handler", "`golang.org/x/sync`", "external dependency", "Fix:", "add `github.com/evil/mod`"},
			want: "`m/internal/handler/checkout` (component `handler`) may import only `golang.org/x/sync`; " +
				"`github.com/evil/mod` (an external dependency) is not among them. " +
				"Fix: add `github.com/evil/mod` to `handler`'s allow list, or depend only on what `handler` already allows.",
		},
		{
			// Same external-module edge as above, but with the classification the
			// check path emits (Target "external" rather than the explain/MCP
			// path's "external module"). Both MUST read identically — this pins
			// the two paths together so the cross-surface wording can't drift.
			name: "rule/whitelist/external target (check-path shape) matches explain-path wording",
			in: Explain{
				From:          "m/internal/handler/checkout",
				To:            "github.com/evil/mod",
				Kind:          ReasonRule,
				FromComponent: "handler",
				Allow:         []Ref{{Kind: RefExternalModule, Name: "golang.org/x/sync"}},
				Stance:        PolicyDeny,
				Target:        "external",
				TargetRef:     "github.com/evil/mod",
			},
			wantContains: []string{"handler", "`golang.org/x/sync`", "external dependency", "Fix:", "add `github.com/evil/mod`"},
			want: "`m/internal/handler/checkout` (component `handler`) may import only `golang.org/x/sync`; " +
				"`github.com/evil/mod` (an external dependency) is not among them. " +
				"Fix: add `github.com/evil/mod` to `handler`'s allow list, or depend only on what `handler` already allows.",
		},
		{
			name: "rule/blacklist/explicit deny component",
			in: Explain{
				From:          "m/internal/service/billing",
				To:            "m/internal/repository",
				Kind:          ReasonRule,
				FromComponent: "service",
				Deny:          []Ref{{Kind: RefComponent, Name: "repository"}},
				Stance:        PolicyAllow,
				Target:        "repository",
			},
			wantContains: []string{"explicitly denies", "`repository`", "Fix:", "remove `repository`"},
			want: "`m/internal/service/billing` (component `service`) explicitly denies importing " +
				"`m/internal/repository` (its deny list names `repository`). " +
				"Fix: remove `repository` from `service`'s deny list if the dependency is intended, or drop the import.",
		},
		{
			name: "rule/blacklist/explicit deny external module",
			in: Explain{
				From:          "m/internal/handler/checkout",
				To:            "golang.org/x/net/http2",
				Kind:          ReasonRule,
				FromComponent: "handler",
				Deny:          []Ref{{Kind: RefExternalModule, Name: "golang.org/x/net"}},
				Stance:        PolicyAllow,
				Target:        "external module",
				TargetRef:     "golang.org/x/net/http2",
			},
			wantContains: []string{"explicitly denies", "`golang.org/x/net`", "Fix:", "remove `golang.org/x/net`"},
			want: "`m/internal/handler/checkout` (component `handler`) explicitly denies importing " +
				"`golang.org/x/net/http2` (its deny list names `golang.org/x/net`). " +
				"Fix: remove `golang.org/x/net` from `handler`'s deny list if the dependency is intended, or drop the import.",
		},
		{
			name: "rule/default-deny/unassigned target",
			in: Explain{
				From:          "m/internal/handler/checkout",
				To:            "m/internal/orphan",
				Kind:          ReasonRule,
				FromComponent: "handler",
				Stance:        PolicyDeny,
				Target:        "unassigned",
			},
			wantContains: []string{"handler", "no rule permitting", "default is to deny", "unassigned package", "Fix:", "add `unassigned`"},
			want: "`m/internal/handler/checkout` (component `handler`) has no rule permitting " +
				"`m/internal/orphan` (an unassigned package), and the default is to deny. " +
				"Fix: add `unassigned` to `handler`'s allow list, or give `handler` a rule that allows this dependency.",
		},
		{
			name: "rule/default-deny/component target no allow list",
			in: Explain{
				From:          "m/internal/handler/checkout",
				To:            "m/internal/repository",
				Kind:          ReasonRule,
				FromComponent: "handler",
				Stance:        PolicyDeny,
				Target:        "repository",
			},
			wantContains: []string{"handler", "no rule permitting", "default is to deny", "component `repository`", "Fix:", "add `repository`"},
			want: "`m/internal/handler/checkout` (component `handler`) has no rule permitting " +
				"`m/internal/repository` (component `repository`), and the default is to deny. " +
				"Fix: add `repository` to `handler`'s allow list, or give `handler` a rule that allows this dependency.",
		},
		{
			name: "boundary/cross-member two peers",
			in: Explain{
				From:     "m/internal/lang/ruby",
				To:       "m/internal/lang/rust",
				Kind:     ReasonBoundary,
				Boundary: "adapters",
				Peers:    []string{"ruby", "rust"},
			},
			wantContains: []string{"peers", "`adapters`", "mutually exclusive", "`ruby` and `rust`", "Fix:", "move the shared code"},
			want: "`m/internal/lang/ruby` and `m/internal/lang/rust` are peers in the `adapters` boundary " +
				"(its members include `ruby` and `rust`), which is mutually exclusive, so neither may import the other. " +
				"Fix: move the shared code into a component outside `adapters`, or remove one member from the boundary.",
		},
		{
			name: "boundary/cross-member multi peers",
			in: Explain{
				From:     "m/internal/lang/ruby",
				To:       "m/internal/lang/rust",
				Kind:     ReasonBoundary,
				Boundary: "adapters",
				Peers:    []string{"go", "ruby", "rust"},
			},
			wantContains: []string{"`go`, `ruby` and `rust`", "`adapters`", "Fix:"},
			want: "`m/internal/lang/ruby` and `m/internal/lang/rust` are peers in the `adapters` boundary " +
				"(its members include `go`, `ruby` and `rust`), which is mutually exclusive, so neither may import the other. " +
				"Fix: move the shared code into a component outside `adapters`, or remove one member from the boundary.",
		},
		{
			name: "boundary/cross-member no peers supplied",
			in: Explain{
				From:     "m/internal/lang/ruby",
				To:       "m/internal/lang/rust",
				Kind:     ReasonBoundary,
				Boundary: "adapters",
			},
			wantContains: []string{"peers", "`adapters`", "mutually exclusive", "Fix:"},
			want: "`m/internal/lang/ruby` and `m/internal/lang/rust` are peers in the `adapters` boundary, " +
				"which is mutually exclusive, so neither may import the other. " +
				"Fix: move the shared code into a component outside `adapters`, or remove one member from the boundary.",
		},
		{
			name: "boundary/sealed",
			in: Explain{
				From:     "m/cmd/tool",
				To:       "m/internal/lang/ruby",
				Kind:     ReasonBoundarySealed,
				Boundary: "adapters",
			},
			wantContains: []string{"sealed", "`adapters`", "outside", "import inward", "Fix:", "add `m/cmd/tool`"},
			want: "the `adapters` boundary is sealed — only its members may import inward. " +
				"`m/cmd/tool` is outside `adapters`, so it may not import `m/internal/lang/ruby` (a member). " +
				"Fix: add `m/cmd/tool` to `adapters`, or route through an allowed component.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Explanation(tt.in)
			if got != tt.want {
				t.Errorf("Explanation() mismatch\n got: %s\nwant: %s", got, tt.want)
			}
			for _, sub := range tt.wantContains {
				if !strings.Contains(got, sub) {
					t.Errorf("Explanation() = %q\nmissing substring %q", got, sub)
				}
			}
			// Every explanation must carry an actionable fix clause and must not
			// be jargon-only: it names the source component or the endpoints.
			if !strings.Contains(got, "Fix:") {
				t.Errorf("Explanation() = %q\nmissing a Fix: clause", got)
			}
		})
	}
}

// TestExplanationDenyAnyStar checks a blacklist deny of "*" names the concrete
// destination rather than the bare "*" jargon.
func TestExplanationDenyAnyStar(t *testing.T) {
	got := Explanation(Explain{
		From:          "m/internal/domain/order",
		To:            "github.com/some/pkg",
		Kind:          ReasonRule,
		FromComponent: "domain",
		Deny:          []Ref{{Kind: RefAny}},
		Stance:        PolicyAllow,
		Target:        "external",
	})
	if strings.Contains(got, "`*`") {
		t.Errorf("Explanation() leaked the `*` jargon: %q", got)
	}
	if !strings.Contains(got, "`external`") {
		t.Errorf("Explanation() should name the concrete destination ref: %q", got)
	}
}

// TestExplanationDefaultDenyUnassignedFromComponent guards the fail-closed path
// when the component has an empty allow list under a plain default-deny policy.
func TestExplanationDefaultDeny(t *testing.T) {
	got := Explanation(Explain{
		From:          "m/internal/handler/checkout",
		To:            "std",
		Kind:          ReasonRule,
		FromComponent: "handler",
		Stance:        PolicyDeny,
		Target:        "std",
	})
	if !strings.Contains(got, "default is to deny") {
		t.Errorf("expected fail-closed phrasing, got %q", got)
	}
	if !strings.Contains(got, "add `std`") {
		t.Errorf("expected the concrete std ref in the fix, got %q", got)
	}
}
