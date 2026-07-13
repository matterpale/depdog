package core

import (
	"reflect"
	"strings"
	"testing"
)

// workFixture builds the WorkRules a parsed work file would produce for a
// four-unit monorepo: web (ts), shared (ts lib), api + billing (go services in
// a mutual-exclusion boundary), with web allowed only shared, shared allowed
// nothing, and shared exposing src/** while hiding internal/**.
func workFixture() *WorkRules {
	units := []WorkUnit{
		{Name: "api", Dir: "services/api", Identities: []string{"example.com/api"}},
		{Name: "billing", Dir: "services/billing", Identities: []string{"example.com/billing"}},
		{Name: "shared", Dir: "shared", Identities: []string{"@acme/shared"}},
		{Name: "web", Dir: "web", Identities: []string{"@acme/web"}},
	}
	rs := &RuleSet{
		Components: []Component{
			{Name: "api", Patterns: []string{"services/api"}},
			{Name: "billing", Patterns: []string{"services/billing"}},
			{Name: "shared", Patterns: []string{"shared"}},
			{Name: "web", Patterns: []string{"web"}},
		},
		Rules: map[string]Rule{
			"web":    {Allow: []Ref{{Kind: RefComponent, Name: "shared"}}},
			"shared": {Deny: []Ref{{Kind: RefAny}}},
		},
		Policy: PolicyAllow,
		Boundaries: []Boundary{{
			Name: "services",
			Members: []BoundaryMember{
				{Component: "api", Patterns: []string{"services/api"}, Label: "api"},
				{Component: "billing", Patterns: []string{"services/billing"}, Label: "billing"},
			},
		}},
	}
	return &WorkRules{
		Units: units,
		Rules: rs,
		Surfaces: map[string]Surface{
			"shared": {Exports: []string{"src/**"}, Internal: []string{"internal/**"}},
		},
	}
}

func TestOwner(t *testing.T) {
	w := &WorkRules{Units: []WorkUnit{
		{Name: "api", Dir: "services/api"},
		{Name: "root", Dir: "."},
		{Name: "svc", Dir: "services"},
	}}
	cases := []struct {
		dir, name, sub string
		ok             bool
	}{
		{"services/api/http", "api", "http", true},
		{"services/api", "api", "", true},
		{"services/x", "svc", "x", true},
		{"services", "svc", "", true},
		{"docs/readme", "root", "docs/readme", true},
		{".", "root", "", true},
	}
	for _, tc := range cases {
		name, sub, ok := w.Owner(tc.dir)
		if name != tc.name || sub != tc.sub || ok != tc.ok {
			t.Errorf("Owner(%q) = %q, %q, %v; want %q, %q, %v", tc.dir, name, sub, ok, tc.name, tc.sub, tc.ok)
		}
	}

	// Without a root unit, an unowned dir reports no owner.
	w2 := &WorkRules{Units: []WorkUnit{{Name: "web", Dir: "web"}}}
	if _, _, ok := w2.Owner("docs"); ok {
		t.Error("Owner(docs) with no covering unit reported an owner")
	}
	// A sibling prefix that is not a path prefix must not match.
	if _, _, ok := w2.Owner("webapp"); ok {
		t.Error("Owner(webapp) matched unit dir web by string prefix")
	}
}

func TestIdentityOwner(t *testing.T) {
	w := &WorkRules{Units: []WorkUnit{
		{Name: "acme", Dir: "acme", Identities: []string{"@acme"}},
		{Name: "shared", Dir: "shared", Identities: []string{"@acme/shared"}},
	}}
	name, sub, ok := w.identityOwner("@acme/shared/internal/secret")
	if !ok || name != "shared" || sub != "internal/secret" {
		t.Errorf("identityOwner = %q, %q, %v; want shared, internal/secret, true", name, sub, ok)
	}
	name, sub, ok = w.identityOwner("@acme/other")
	if !ok || name != "acme" || sub != "other" {
		t.Errorf("identityOwner = %q, %q, %v; want acme, other, true", name, sub, ok)
	}
	if _, _, ok := w.identityOwner("@acmeister/x"); ok {
		t.Error("identityOwner matched a non-segment prefix")
	}
}

func TestEvaluateWork(t *testing.T) {
	w := workFixture()
	inputs := []UnitGraph{
		{Unit: "web", Graph: &Graph{Packages: []Package{{
			ImportPath: "web", RelDir: ".",
			Imports: []Import{
				// Path channel: a relative import resolved into shared's tree.
				{Path: "../shared/internal/secret", Class: ClassInModule, RelDir: "../shared/internal",
					Positions: []Position{{File: "src/app.ts", Line: 3}}},
				// Identity channel, public root: passes the surface check.
				{Path: "@acme/shared", Class: ClassExternal,
					Positions: []Position{{File: "src/app.ts", Line: 1}}},
				// Identity channel, exported sub-path: passes.
				{Path: "@acme/shared/src/util", Class: ClassExternal,
					Positions: []Position{{File: "src/app.ts", Line: 2}}},
				// Intra-unit: never a cross edge.
				{Path: "./lib", Class: ClassInModule, RelDir: "lib"},
				// Std and unrelated external: ignored.
				{Path: "node:fs", Class: ClassStd},
				{Path: "left-pad", Class: ClassExternal},
			},
		}}}},
		{Unit: "shared", Graph: &Graph{Packages: []Package{{
			ImportPath: "shared", RelDir: ".",
			Imports: []Import{
				// Path channel back into web: shared denies everything.
				{Path: "../web/src/app", Class: ClassInModule, RelDir: "../web/src",
					Positions: []Position{{File: "index.ts", Line: 8}}},
			},
		}}}},
		{Unit: "api", Graph: &Graph{Packages: []Package{{
			ImportPath: "example.com/api", RelDir: ".",
			Imports: []Import{
				// Identity channel into the boundary peer.
				{Path: "example.com/billing/pkg", Class: ClassExternal,
					Positions: []Position{{File: "main.go", Line: 5}}},
			},
		}}}},
		{Unit: "billing", Graph: &Graph{Packages: []Package{{
			ImportPath: "example.com/billing", RelDir: ".",
			Imports: []Import{
				{Path: "example.com/api", Class: ClassExternal,
					Positions: []Position{{File: "billing.go", Line: 4}}},
			},
		}}}},
	}

	res, err := EvaluateWork(inputs, w)
	if err != nil {
		t.Fatal(err)
	}

	type brief struct {
		from, imp, target string
		reason            ReasonKind
	}
	var got []brief
	for _, v := range res.Violations {
		got = append(got, brief{v.FromPackage, v.ImportPath, v.Target, v.Reason})
	}
	want := []brief{
		{"api", "billing", "billing", ReasonCrossUnitBoundary},
		{"billing", "api", "api", ReasonCrossUnitBoundary},
		{"shared", "web", "web", ReasonCrossUnit},
		{"web", "shared/internal", "shared", ReasonCrossUnitSurface},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("violations = %+v\nwant %+v", got, want)
	}

	// Positions are root-relative: the source unit dir prefixes the file.
	for _, v := range res.Violations {
		if v.FromPackage == "web" {
			if len(v.Positions) != 1 || v.Positions[0].File != "web/src/app.ts" {
				t.Errorf("web surface violation positions = %+v, want web/src/app.ts", v.Positions)
			}
		}
		if v.FromPackage == "api" {
			if len(v.Positions) != 1 || v.Positions[0].File != "services/api/main.go" {
				t.Errorf("api violation positions = %+v, want services/api/main.go", v.Positions)
			}
		}
	}

	// Stats count units and detected unit edges.
	if res.Stats.Packages != 4 {
		t.Errorf("stats.packages = %d, want 4", res.Stats.Packages)
	}
	if res.Stats.Edges != 4 { // web→shared, shared→web, api→billing, billing→api
		t.Errorf("stats.edges = %d, want 4", res.Stats.Edges)
	}

	// The allowed web→shared unit edge produced no unit-level violation (only
	// the surface finding), and no warnings fired.
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", res.Warnings)
	}
}

func TestEvaluateWorkExplanations(t *testing.T) {
	w := workFixture()
	inputs := []UnitGraph{
		{Unit: "web", Graph: &Graph{Packages: []Package{{
			ImportPath: "web", RelDir: ".",
			Imports: []Import{
				{Path: "@acme/shared/internal/secret", Class: ClassExternal,
					Positions: []Position{{File: "src/app.ts", Line: 9}}},
				{Path: "example.com/api", Class: ClassExternal,
					Positions: []Position{{File: "src/call.ts", Line: 2}}},
			},
		}}}},
	}
	res, err := EvaluateWork(inputs, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("violations = %+v, want 2 (allow-list denial + surface)", res.Violations)
	}

	// web → api: not on web's allow list.
	rule := res.Violations[0]
	if rule.Reason != ReasonCrossUnit || rule.Target != "api" {
		t.Fatalf("first violation = %+v, want cross-unit web→api", rule)
	}
	text := Explanation(ExplainWorkViolation(rule, w))
	for _, frag := range []string{"unit `web` may depend only on", "`shared`", "depdog.work.yaml"} {
		if !strings.Contains(text, frag) {
			t.Errorf("cross-unit explanation missing %q: %s", frag, text)
		}
	}

	// web → shared/internal/secret: internal surface hit.
	surf := res.Violations[1]
	if surf.Reason != ReasonCrossUnitSurface {
		t.Fatalf("second violation = %+v, want cross-unit-surface", surf)
	}
	text = Explanation(ExplainWorkViolation(surf, w))
	for _, frag := range []string{"internal", "`internal/**`", "`src/**`"} {
		if !strings.Contains(text, frag) {
			t.Errorf("surface explanation missing %q: %s", frag, text)
		}
	}
}

// TestEvaluateWorkNestedSource: a package scanned by an outer unit but owned
// by a nested, more specific unit is judged by the nested unit's own scan —
// the outer scan must not fabricate edges on its behalf.
func TestEvaluateWorkNestedSource(t *testing.T) {
	w := &WorkRules{
		Units: []WorkUnit{
			{Name: "api", Dir: "services/api"},
			{Name: "svc", Dir: "services"},
			{Name: "web", Dir: "web"},
		},
		Rules: &RuleSet{
			Components: []Component{
				{Name: "api", Patterns: []string{"services/api"}},
				{Name: "svc", Patterns: []string{"services"}},
				{Name: "web", Patterns: []string{"web"}},
			},
			Rules:  map[string]Rule{"svc": {Deny: []Ref{{Kind: RefComponent, Name: "web"}}}},
			Policy: PolicyAllow,
		},
	}
	inputs := []UnitGraph{
		// The outer svc scan sees api's package too; its edge into web must be
		// attributed to nobody here (api's own scan would report it).
		{Unit: "svc", Graph: &Graph{Packages: []Package{{
			ImportPath: "svc/api-part", RelDir: "api/impl",
			Imports: []Import{{Path: "../../web/x", Class: ClassInModule, RelDir: "../web/x"}},
		}}}},
	}
	res, err := EvaluateWork(inputs, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("violations = %+v, want none (source owned by nested unit)", res.Violations)
	}

	// The same package scanned by the nested unit itself does produce the edge.
	inputs = []UnitGraph{{Unit: "api", Graph: &Graph{Packages: []Package{{
		ImportPath: "example.com/api/impl", RelDir: "impl",
		Imports: []Import{{Path: "../../web/x", Class: ClassInModule, RelDir: "../../web/x"}},
	}}}}}
	res, err = EvaluateWork(inputs, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("violations = %+v, want none (api may depend on web)", res.Violations)
	}
	if res.Stats.Edges != 1 {
		t.Errorf("stats.edges = %d, want 1 (api→web detected)", res.Stats.Edges)
	}
}

func TestEvaluateWorkTestOnly(t *testing.T) {
	w := workFixture()
	inputs := []UnitGraph{{Unit: "shared", Graph: &Graph{Packages: []Package{{
		ImportPath: "shared", RelDir: ".",
		Imports: []Import{{Path: "../web/testutil", Class: ClassInModule, RelDir: "../web/testutil",
			TestOnly: true, Positions: []Position{{File: "x_test.ts", Line: 1}}}},
	}}}}}
	res, err := EvaluateWork(inputs, w)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 1 || !res.Violations[0].TestOnly {
		t.Errorf("violations = %+v, want one test-only shared→web denial", res.Violations)
	}
}
