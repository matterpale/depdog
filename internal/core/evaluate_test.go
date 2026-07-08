package core

import "testing"

func mkImport(path string, class Class, relDir string, testOnly bool) Import {
	return Import{Path: path, Class: class, RelDir: relDir, TestOnly: testOnly,
		Positions: []Position{{File: "x.go", Line: 1}}}
}

// ddd builds the recurring fixture: main/domain/handler/service/repository
// under a deny policy, with domain restricted to std.
func ddd() *RuleSet {
	return &RuleSet{
		Components: []Component{
			{Name: "domain", Patterns: []string{"internal/domain/**"}},
			{Name: "handler", Patterns: []string{"internal/handler/**"}},
			{Name: "main", Patterns: []string{"cmd/**"}},
			{Name: "repository", Patterns: []string{"internal/repository/**"}},
			{Name: "service", Patterns: []string{"internal/service/**"}},
		},
		Rules: map[string]Rule{
			"main":       {Allow: []Ref{{Kind: RefAny}}},
			"domain":     {Allow: []Ref{{Kind: RefStd}}},
			"handler":    {Allow: []Ref{{Kind: RefComponent, Name: "domain"}, {Kind: RefStd}}},
			"service":    {Allow: []Ref{{Kind: RefComponent, Name: "domain"}, {Kind: RefStd}}},
			"repository": {Allow: []Ref{{Kind: RefComponent, Name: "domain"}, {Kind: RefStd}, {Kind: RefExternal}}},
		},
		Policy:    PolicyDeny,
		TestFiles: TestHybrid,
	}
}

func evaluate(t *testing.T, g *Graph, rs *RuleSet) *Result {
	t.Helper()
	res, err := Evaluate(g, rs)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return res
}

func TestEvaluateWhitelist(t *testing.T) {
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/domain/order", RelDir: "internal/domain/order", Imports: []Import{
			mkImport("strings", ClassStd, "", false),
			mkImport("github.com/google/uuid", ClassExternal, "", false),
			mkImport("m/internal/repository", ClassInModule, "internal/repository", false),
		}},
		{ImportPath: "m/internal/handler/checkout", RelDir: "internal/handler/checkout", Imports: []Import{
			mkImport("m/internal/domain/order", ClassInModule, "internal/domain/order", false),
			mkImport("m/internal/service", ClassInModule, "internal/service", false),
		}},
	}}
	res := evaluate(t, g, ddd())

	want := []struct{ from, imp, target string }{
		{"m/internal/domain/order", "github.com/google/uuid", "external"},
		{"m/internal/domain/order", "m/internal/repository", "repository"},
		{"m/internal/handler/checkout", "m/internal/service", "service"},
	}
	if len(res.Violations) != len(want) {
		t.Fatalf("got %d violations, want %d: %+v", len(res.Violations), len(want), res.Violations)
	}
	for i, w := range want {
		v := res.Violations[i]
		if v.FromPackage != w.from || v.ImportPath != w.imp || v.Target != w.target {
			t.Errorf("violation[%d] = %s → %s (target %s), want %s → %s (target %s)",
				i, v.FromPackage, v.ImportPath, v.Target, w.from, w.imp, w.target)
		}
	}
	if res.Violations[0].Rule != "domain: allow [std]" {
		t.Errorf("rule text = %q, want %q", res.Violations[0].Rule, "domain: allow [std]")
	}
}

func TestEvaluateComponentStats(t *testing.T) {
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/domain/order", RelDir: "internal/domain/order", Imports: []Import{
			mkImport("strings", ClassStd, "", false),
			mkImport("github.com/google/uuid", ClassExternal, "", false),
			mkImport("m/internal/repository", ClassInModule, "internal/repository", false),
		}},
		{ImportPath: "m/internal/handler/checkout", RelDir: "internal/handler/checkout", Imports: []Import{
			mkImport("m/internal/domain/order", ClassInModule, "internal/domain/order", false),
			mkImport("m/internal/service", ClassInModule, "internal/service", false),
		}},
	}}
	res := evaluate(t, g, ddd())

	// All declared components appear, sorted, even the empty ones.
	want := []ComponentStat{
		{Name: "domain", Packages: 1, Edges: 3, Violations: 2},
		{Name: "handler", Packages: 1, Edges: 2, Violations: 1},
		{Name: "main"},
		{Name: "repository"},
		{Name: "service"},
	}
	if len(res.Components) != len(want) {
		t.Fatalf("components = %d, want %d: %+v", len(res.Components), len(want), res.Components)
	}
	for i, w := range want {
		if res.Components[i] != w {
			t.Errorf("component[%d] = %+v, want %+v", i, res.Components[i], w)
		}
	}
}

func TestStanceInferredFromRule(t *testing.T) {
	rs := &RuleSet{
		Rules: map[string]Rule{
			"whitelisted": {Allow: []Ref{{Kind: RefStd}}},
			"blacklisted": {Deny: []Ref{{Kind: RefExternal}}},
			"both":        {Allow: []Ref{{Kind: RefStd}}, Deny: []Ref{{Kind: RefExternal}}},
			"empty":       {},
		},
		Policy: PolicyDeny,
	}
	cases := map[string]Policy{
		"whitelisted": PolicyDeny,  // an allow list ⇒ whitelist
		"blacklisted": PolicyAllow, // a deny-only rule ⇒ blacklist
		"both":        PolicyDeny,  // an allow list wins the stance
		"empty":       PolicyDeny,  // no lists ⇒ global policy
		"norule":      PolicyDeny,  // no rule ⇒ global policy
	}
	for name, want := range cases {
		if got := rs.Stance(name); got != want {
			t.Errorf("Stance(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestEvaluateInferredStance(t *testing.T) {
	// Global policy is deny, but a deny-only rule must behave as a blacklist
	// for its component (the footgun fix) while an allow list stays a whitelist.
	rs := &RuleSet{
		Components: []Component{
			{Name: "domain", Patterns: []string{"internal/domain/**"}},
			{Name: "handler", Patterns: []string{"internal/handler/**"}},
			{Name: "service", Patterns: []string{"internal/service/**"}},
		},
		Rules: map[string]Rule{
			"domain":  {Allow: []Ref{{Kind: RefStd}}},                       // whitelist: std only
			"handler": {Deny: []Ref{{Kind: RefComponent, Name: "service"}}}, // blacklist: all but service
		},
		Policy: PolicyDeny,
	}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/handler", RelDir: "internal/handler", Imports: []Import{
			mkImport("m/internal/domain", ClassInModule, "internal/domain", false),   // allowed (not denied)
			mkImport("m/internal/service", ClassInModule, "internal/service", false), // denied
		}},
		{ImportPath: "m/internal/domain", RelDir: "internal/domain", Imports: []Import{
			mkImport("m/internal/service", ClassInModule, "internal/service", false), // violates whitelist
		}},
	}}
	res := evaluate(t, g, rs)

	got := map[string]bool{}
	for _, v := range res.Violations {
		got[v.FromPackage+"→"+v.ImportPath] = true
	}
	if len(res.Violations) != 2 {
		t.Fatalf("violations = %d, want 2: %+v", len(res.Violations), res.Violations)
	}
	if !got["m/internal/handler→m/internal/service"] {
		t.Error("handler→service should be denied by the deny list")
	}
	if got["m/internal/handler→m/internal/domain"] {
		t.Error("handler→domain should pass: a deny-only rule is a blacklist, not deny-all")
	}
	if !got["m/internal/domain→m/internal/service"] {
		t.Error("domain→service should violate the whitelist")
	}
}

func TestDecide(t *testing.T) {
	rs := ddd() // whitelist: domain allow[std], handler allow[domain,std], main allow[*]
	cases := []struct {
		comp, target string
		allow        bool
	}{
		{"domain", "std", true},
		{"domain", "repository", false}, // not allowed under whitelist
		{"handler", "domain", true},
		{"handler", "service", false},
		{"domain", "domain", true},   // same component
		{"main", "repository", true}, // main allows *
	}
	for _, c := range cases {
		got, reason := rs.Decide(c.comp, c.target)
		if got != c.allow {
			t.Errorf("Decide(%q, %q) = %v (%s), want %v", c.comp, c.target, got, reason, c.allow)
		}
	}

	// Blacklist: a deny-only rule under policy allow.
	bl := &RuleSet{
		Components: []Component{{Name: "handler", Patterns: []string{"a"}}, {Name: "service", Patterns: []string{"b"}}},
		Rules:      map[string]Rule{"handler": {Deny: []Ref{{Kind: RefComponent, Name: "service"}}}},
		Policy:     PolicyAllow,
	}
	if got, _ := bl.Decide("handler", "service"); got {
		t.Error("handler → service should be denied by the deny list")
	}
	if got, _ := bl.Decide("handler", "domain"); !got {
		t.Error("handler → domain should pass under a blacklist")
	}
}

func TestDecideModule(t *testing.T) {
	rs := &RuleSet{
		Components: []Component{{Name: "domain", Patterns: []string{"d"}}},
		Rules: map[string]Rule{
			"domain": {Allow: []Ref{{Kind: RefStd}, {Kind: RefExternalModule, Name: "golang.org/x/sync"}}},
		},
		Policy: PolicyDeny,
	}
	if ok, _ := rs.DecideModule("domain", "golang.org/x/sync"); !ok {
		t.Error("the exact module should be allowed")
	}
	if ok, _ := rs.DecideModule("domain", "golang.org/x/sync/errgroup"); !ok {
		t.Error("a sub-path of the module should be allowed")
	}
	if ok, _ := rs.DecideModule("domain", "github.com/other/thing"); ok {
		t.Error("a different module should be denied under a whitelist")
	}

	broad := &RuleSet{
		Components: []Component{{Name: "app", Patterns: []string{"a"}}},
		Rules:      map[string]Rule{"app": {Allow: []Ref{{Kind: RefExternal}}}},
		Policy:     PolicyDeny,
	}
	if ok, _ := broad.DecideModule("app", "anything/at/all"); !ok {
		t.Error("a broad external allow should permit any module")
	}
}

func TestEvaluateExternalModuleAllow(t *testing.T) {
	// Whitelist only std and one external module prefix.
	rs := &RuleSet{
		Components: []Component{{Name: "domain", Patterns: []string{"internal/domain/**"}}},
		Rules: map[string]Rule{
			"domain": {Allow: []Ref{{Kind: RefStd}, {Kind: RefExternalModule, Name: "example.com/ok"}}},
		},
		Policy: PolicyDeny,
	}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/domain", RelDir: "internal/domain", Imports: []Import{
			mkImport("example.com/ok", ClassExternal, "", false),     // exact prefix -> allowed
			mkImport("example.com/ok/sub", ClassExternal, "", false), // sub-path -> allowed
			mkImport("example.com/other", ClassExternal, "", false),  // different module -> violation
		}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 || res.Violations[0].ImportPath != "example.com/other" {
		t.Fatalf("only example.com/other should violate: %+v", res.Violations)
	}
}

func TestEvaluateExternalModuleDenyWins(t *testing.T) {
	// Allow external broadly, but deny one specific module.
	rs := &RuleSet{
		Components: []Component{{Name: "app", Patterns: []string{"**"}}},
		Rules: map[string]Rule{
			"app": {Allow: []Ref{{Kind: RefExternal}}, Deny: []Ref{{Kind: RefExternalModule, Name: "example.com/bad"}}},
		},
		Policy: PolicyDeny,
	}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/app", RelDir: "app", Imports: []Import{
			mkImport("example.com/good", ClassExternal, "", false),  // allowed by external
			mkImport("example.com/bad/x", ClassExternal, "", false), // denied (deny prefix wins)
		}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 || res.Violations[0].ImportPath != "example.com/bad/x" {
		t.Fatalf("the deny module prefix should win: %+v", res.Violations)
	}
}

func TestEvaluateBlacklist(t *testing.T) {
	rs := &RuleSet{
		Components: []Component{
			{Name: "handler", Patterns: []string{"internal/handler/**"}},
			{Name: "service", Patterns: []string{"internal/service/**"}},
		},
		Rules: map[string]Rule{
			"handler": {Deny: []Ref{{Kind: RefComponent, Name: "service"}}},
		},
		Policy:    PolicyAllow,
		TestFiles: TestHybrid,
	}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/handler", RelDir: "internal/handler", Imports: []Import{
			mkImport("m/internal/service", ClassInModule, "internal/service", false),
			mkImport("github.com/anything/else", ClassExternal, "", false),
		}},
		{ImportPath: "m/internal/service", RelDir: "internal/service", Imports: []Import{
			mkImport("m/internal/handler", ClassInModule, "internal/handler", false),
		}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 {
		t.Fatalf("got %d violations, want 1: %+v", len(res.Violations), res.Violations)
	}
	v := res.Violations[0]
	if v.ImportPath != "m/internal/service" || v.Rule != "handler: deny [service]" {
		t.Errorf("got %s under rule %q", v.ImportPath, v.Rule)
	}
}

func TestEvaluateDenyBeatsAllow(t *testing.T) {
	rs := &RuleSet{
		Components: []Component{{Name: "app", Patterns: []string{"**"}}},
		Rules: map[string]Rule{
			"app": {Allow: []Ref{{Kind: RefAny}}, Deny: []Ref{{Kind: RefExternal}}},
		},
		Policy: PolicyDeny,
	}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/app", RelDir: "app", Imports: []Import{
			mkImport("github.com/x/y", ClassExternal, "", false),
			mkImport("fmt", ClassStd, "", false),
		}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 || res.Violations[0].ImportPath != "github.com/x/y" {
		t.Fatalf("deny should beat allow: %+v", res.Violations)
	}
}

func TestEvaluateTestFileModes(t *testing.T) {
	graph := func() *Graph {
		return &Graph{ModulePath: "m", Packages: []Package{
			{ImportPath: "m/internal/domain/order", RelDir: "internal/domain/order", Imports: []Import{
				mkImport("github.com/stretchr/testify", ClassExternal, "", true),
				mkImport("m/internal/repository", ClassInModule, "internal/repository", true),
			}},
		}}
	}

	tests := []struct {
		mode TestFileMode
		want int // violations
	}{
		{TestHybrid, 1},    // external ok, cross-component still flagged
		{TestSameRules, 2}, // both flagged
		{TestRelaxed, 0},   // both exempt
	}
	for _, tt := range tests {
		rs := ddd()
		rs.TestFiles = tt.mode
		res := evaluate(t, graph(), rs)
		if len(res.Violations) != tt.want {
			t.Errorf("mode %v: got %d violations, want %d: %+v", tt.mode, len(res.Violations), tt.want, res.Violations)
		}
	}
}

func TestEvaluateSelfImportAlwaysAllowed(t *testing.T) {
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/domain/order", RelDir: "internal/domain/order", Imports: []Import{
			mkImport("m/internal/domain/money", ClassInModule, "internal/domain/money", false),
		}},
	}}
	res := evaluate(t, g, ddd()) // domain only allows std, but money is also domain
	if len(res.Violations) != 0 {
		t.Fatalf("intra-component import flagged: %+v", res.Violations)
	}
}

func TestEvaluateUnassigned(t *testing.T) {
	g := &Graph{ModulePath: "m", Packages: []Package{
		// Unassigned source: warned once, edges not judged.
		{ImportPath: "m/pkg/util", RelDir: "pkg/util", Imports: []Import{
			mkImport("m/internal/repository", ClassInModule, "internal/repository", false),
		}},
		// Assigned source importing an unassigned target: violation.
		{ImportPath: "m/internal/service", RelDir: "internal/service", Imports: []Import{
			mkImport("m/pkg/util", ClassInModule, "pkg/util", false),
		}},
	}}
	res := evaluate(t, g, ddd())
	var un []Warning
	for _, w := range res.Warnings {
		if w.Kind == WarnUnassigned {
			un = append(un, w)
		}
	}
	if len(un) != 1 || un[0].RelDir != "pkg/util" {
		t.Fatalf("unassigned warnings = %+v, want one for pkg/util", un)
	}
	if len(res.Violations) != 1 || res.Violations[0].Target != "unassigned" {
		t.Fatalf("violations = %+v, want one with target unassigned", res.Violations)
	}
}

func TestEvaluateEmptyComponent(t *testing.T) {
	// Only domain has a package; the other declared components match nothing
	// and should be flagged as empty (dead patterns), never fatal.
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/domain/order", RelDir: "internal/domain/order", Imports: []Import{
			mkImport("fmt", ClassStd, "", false),
		}},
	}}
	res := evaluate(t, g, ddd())

	empty := map[string]bool{}
	for _, w := range res.Warnings {
		if w.Kind == WarnEmptyComponent {
			empty[w.Component] = true
		}
	}
	for _, name := range []string{"handler", "main", "repository", "service"} {
		if !empty[name] {
			t.Errorf("component %q claims no package and should be flagged empty", name)
		}
	}
	if empty["domain"] {
		t.Error("domain has a package and must not be flagged empty")
	}
	if len(res.Violations) != 0 {
		t.Errorf("empty components must not produce violations: %+v", res.Violations)
	}
}

func TestEvaluateCycles(t *testing.T) {
	// foo -> bar and bar -> foo at the component level, via distinct packages
	// (no package-level cycle). policy allow, so no violations — just the cycle.
	rs := &RuleSet{
		Components: []Component{
			{Name: "foo", Patterns: []string{"foo/**"}},
			{Name: "bar", Patterns: []string{"bar/**"}},
		},
		Policy: PolicyAllow,
	}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/foo/x", RelDir: "foo/x", Imports: []Import{mkImport("m/bar/p", ClassInModule, "bar/p", false)}},
		{ImportPath: "m/bar/q", RelDir: "bar/q", Imports: []Import{mkImport("m/foo/y", ClassInModule, "foo/y", false)}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Violations) != 0 {
		t.Fatalf("policy allow should yield no violations: %+v", res.Violations)
	}
	if len(res.Cycles) != 1 || len(res.Cycles[0]) != 2 ||
		res.Cycles[0][0] != "bar" || res.Cycles[0][1] != "foo" {
		t.Fatalf("cycles = %v, want [[bar foo]]", res.Cycles)
	}
}

func TestEvaluateNoFalseCycle(t *testing.T) {
	// A plain DAG (handler -> domain) must report no cycle.
	res := evaluate(t, &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/handler", RelDir: "internal/handler", Imports: []Import{
			mkImport("m/internal/domain", ClassInModule, "internal/domain", false),
		}},
	}}, ddd())
	if len(res.Cycles) != 0 {
		t.Errorf("a DAG should have no cycles: %v", res.Cycles)
	}
}

func TestEvaluateSkip(t *testing.T) {
	rs := ddd()
	rs.Skip = []string{"internal/legacy/**"}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/legacy/mess", RelDir: "internal/legacy/mess", Imports: []Import{
			mkImport("m/internal/repository", ClassInModule, "internal/repository", false),
		}},
		{ImportPath: "m/internal/service", RelDir: "internal/service", Imports: []Import{
			mkImport("m/internal/legacy/mess", ClassInModule, "internal/legacy/mess", false),
		}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Violations) != 0 {
		t.Fatalf("skipped packages should produce no violations: %+v", res.Violations)
	}
	for _, w := range res.Warnings {
		if w.Kind == WarnUnassigned {
			t.Fatalf("skipped package leaked as an unassigned warning: %+v", w)
		}
	}
	if res.Stats.Packages != 1 {
		t.Errorf("stats.Packages = %d, want 1", res.Stats.Packages)
	}
}

// twoServiceBoundary is the recurring boundaries fixture: two service globs in
// one boundary. sealed controls the one-way wall. Under policy allow, so the
// only violations produced are boundary-sourced.
func twoServiceBoundary(sealed bool) *RuleSet {
	return &RuleSet{
		Policy:    PolicyAllow,
		TestFiles: TestHybrid,
		Boundaries: []Boundary{{
			Name:   "cmd-services",
			Sealed: sealed,
			Members: []BoundaryMember{
				{Patterns: []string{"cmd/comparator/**"}, Label: "cmd/comparator/**"},
				{Patterns: []string{"cmd/query-ce/**"}, Label: "cmd/query-ce/**"},
			},
		}},
	}
}

// boundaryEdge builds a one-package graph with a single in-module edge.
func boundaryEdge(fromPath, fromDir, toPath, toDir string, testOnly bool) *Graph {
	return &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: fromPath, RelDir: fromDir, Imports: []Import{
			mkImport(toPath, ClassInModule, toDir, testOnly),
		}},
	}}
}

func TestEvaluateBoundaryMatrix(t *testing.T) {
	cases := []struct {
		name           string
		sealed         bool
		from, fromDir  string
		to, toDir      string
		wantViolations int
		wantReason     ReasonKind
	}{
		{"in-member allowed", false,
			"m/cmd/query-ce/a", "cmd/query-ce/a", "m/cmd/query-ce/b", "cmd/query-ce/b", 0, ""},
		{"cross-member denied", false,
			"m/cmd/comparator/x", "cmd/comparator/x", "m/cmd/query-ce/y", "cmd/query-ce/y", 1, ReasonBoundary},
		{"member to ungrouped allowed", false,
			"m/cmd/query-ce/a", "cmd/query-ce/a", "m/internal/shared", "internal/shared", 0, ""},
		{"ungrouped to member unsealed allowed", false,
			"m/internal/shared", "internal/shared", "m/cmd/query-ce/a", "cmd/query-ce/a", 0, ""},
		{"ungrouped to member sealed denied", true,
			"m/internal/shared", "internal/shared", "m/cmd/query-ce/a", "cmd/query-ce/a", 1, ReasonBoundarySealed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rs := twoServiceBoundary(c.sealed)
			g := boundaryEdge(c.from, c.fromDir, c.to, c.toDir, false)
			res := evaluate(t, g, rs)
			if len(res.Violations) != c.wantViolations {
				t.Fatalf("violations = %d, want %d: %+v", len(res.Violations), c.wantViolations, res.Violations)
			}
			if c.wantViolations > 0 {
				v := res.Violations[0]
				if v.Reason != c.wantReason {
					t.Errorf("reason = %q, want %q", v.Reason, c.wantReason)
				}
				if v.Boundary != "cmd-services" {
					t.Errorf("boundary = %q, want cmd-services", v.Boundary)
				}
			}
		})
	}
}

func TestEvaluateBoundarySealedComponentlessSource(t *testing.T) {
	// The critical control-flow case: the source package is claimed by NO
	// component (so the old loop continued before the imports loop), yet the
	// sealed rule must still fire on its outgoing edge into a member.
	rs := twoServiceBoundary(true)
	// No component covers internal/shared, so it is truly component-less.
	g := boundaryEdge("m/internal/shared", "internal/shared", "m/cmd/query-ce/a", "cmd/query-ce/a", false)
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 || res.Violations[0].Reason != ReasonBoundarySealed {
		t.Fatalf("a component-less ungrouped source must still be sealed-denied: %+v", res.Violations)
	}
	// The unassigned warning is orthogonal: it must still fire for the source.
	var un int
	for _, w := range res.Warnings {
		if w.Kind == WarnUnassigned && w.RelDir == "internal/shared" {
			un++
		}
	}
	if un != 1 {
		t.Errorf("membership must not silence the unassigned warning: got %d, want 1", un)
	}
}

func TestEvaluateBoundaryWinsOverComponentAllow(t *testing.T) {
	// Both packages are component-assigned with allow[*], yet the cross-member
	// boundary crossing is a hard deny that wins over the component allow.
	rs := &RuleSet{
		Policy:    PolicyAllow,
		TestFiles: TestHybrid,
		Components: []Component{
			{Name: "comparator", Patterns: []string{"cmd/comparator/**"}},
			{Name: "query-ce", Patterns: []string{"cmd/query-ce/**"}},
		},
		Rules: map[string]Rule{
			"comparator": {Allow: []Ref{{Kind: RefAny}}},
			"query-ce":   {Allow: []Ref{{Kind: RefAny}}},
		},
		Boundaries: []Boundary{{
			Name: "cmd-services",
			Members: []BoundaryMember{
				{Patterns: []string{"cmd/comparator/**"}, Label: "cmd/comparator/**"},
				{Patterns: []string{"cmd/query-ce/**"}, Label: "cmd/query-ce/**"},
			},
		}},
	}
	g := boundaryEdge("m/cmd/comparator/x", "cmd/comparator/x", "m/cmd/query-ce/y", "cmd/query-ce/y", false)
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 || res.Violations[0].Reason != ReasonBoundary {
		t.Fatalf("boundary deny must win over component allow: %+v", res.Violations)
	}
}

func TestEvaluateBoundaryMultiComposition(t *testing.T) {
	// An edge subject to two boundaries: the first (sorted) denying boundary
	// wins, and exactly one violation is emitted for determinism.
	rs := &RuleSet{
		Policy:    PolicyAllow,
		TestFiles: TestHybrid,
		Boundaries: []Boundary{
			// "aaa" sorts before "zzz"; both deny this cross-member edge.
			{Name: "aaa", Members: []BoundaryMember{
				{Patterns: []string{"cmd/comparator/**"}, Label: "cmd/comparator/**"},
				{Patterns: []string{"cmd/query-ce/**"}, Label: "cmd/query-ce/**"},
			}},
			{Name: "zzz", Members: []BoundaryMember{
				{Patterns: []string{"cmd/comparator/**"}, Label: "cmd/comparator/**"},
				{Patterns: []string{"cmd/query-ce/**"}, Label: "cmd/query-ce/**"},
			}},
		},
	}
	g := boundaryEdge("m/cmd/comparator/x", "cmd/comparator/x", "m/cmd/query-ce/y", "cmd/query-ce/y", false)
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 {
		t.Fatalf("multi-boundary edge must emit exactly one violation: %+v", res.Violations)
	}
	if res.Violations[0].Boundary != "aaa" {
		t.Errorf("first sorted boundary should win: got %q, want aaa", res.Violations[0].Boundary)
	}
}

func TestEvaluateBoundaryTestFileModes(t *testing.T) {
	// A cross-member edge that appears only in _test.go is relaxed exactly like
	// a component edge would be, under each test_files mode.
	tests := []struct {
		mode TestFileMode
		want int
	}{
		{TestHybrid, 1},    // in-module cross-member still enforced under hybrid
		{TestSameRules, 1}, // enforced
		{TestRelaxed, 0},   // relaxed: the test-only crossing is exempt
	}
	for _, tt := range tests {
		rs := twoServiceBoundary(false)
		rs.TestFiles = tt.mode
		g := boundaryEdge("m/cmd/comparator/x", "cmd/comparator/x", "m/cmd/query-ce/y", "cmd/query-ce/y", true)
		res := evaluate(t, g, rs)
		if len(res.Violations) != tt.want {
			t.Errorf("mode %v: got %d violations, want %d: %+v", tt.mode, len(res.Violations), tt.want, res.Violations)
		}
	}
}

func TestEvaluateBoundaryOffCycleAxis(t *testing.T) {
	// A boundary crossing must never appear as a component cycle.
	rs := &RuleSet{
		Policy:    PolicyAllow,
		TestFiles: TestHybrid,
		Components: []Component{
			{Name: "comparator", Patterns: []string{"cmd/comparator/**"}},
			{Name: "query-ce", Patterns: []string{"cmd/query-ce/**"}},
		},
		Boundaries: []Boundary{{
			Name: "cmd-services",
			Members: []BoundaryMember{
				{Patterns: []string{"cmd/comparator/**"}, Label: "cmd/comparator/**"},
				{Patterns: []string{"cmd/query-ce/**"}, Label: "cmd/query-ce/**"},
			},
		}},
	}
	// A mutual cross-member pair would be a cycle if it fed compEdges.
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/cmd/comparator/x", RelDir: "cmd/comparator/x", Imports: []Import{
			mkImport("m/cmd/query-ce/y", ClassInModule, "cmd/query-ce/y", false)}},
		{ImportPath: "m/cmd/query-ce/y", RelDir: "cmd/query-ce/y", Imports: []Import{
			mkImport("m/cmd/comparator/x", ClassInModule, "cmd/comparator/x", false)}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Cycles) != 0 {
		t.Errorf("boundary crossings must not create a component cycle: %v", res.Cycles)
	}
}

func TestEvaluateEmptyBoundaryMemberWarning(t *testing.T) {
	// A glob member matching no package surfaces an advisory, never fatal; a
	// component member does not (it rides the empty-component warning).
	rs := &RuleSet{
		Policy:    PolicyAllow,
		TestFiles: TestHybrid,
		Boundaries: []Boundary{{
			Name: "cmd-services",
			Members: []BoundaryMember{
				{Patterns: []string{"cmd/comparator/**"}, Label: "cmd/comparator/**"}, // matched
				{Patterns: []string{"cmd/ghost/**"}, Label: "cmd/ghost/**"},           // matches nothing
			},
		}},
	}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/cmd/comparator/x", RelDir: "cmd/comparator/x", Imports: nil},
	}}
	res := evaluate(t, g, rs)
	var empties []Warning
	for _, w := range res.Warnings {
		if w.Kind == WarnEmptyBoundaryMember {
			empties = append(empties, w)
		}
	}
	if len(empties) != 1 || empties[0].Boundary != "cmd-services" || empties[0].Component != "cmd/ghost/**" {
		t.Fatalf("want one empty-boundary-member warning for cmd/ghost/**: %+v", empties)
	}
}

func TestEvaluateNoRulePolicyDeny(t *testing.T) {
	rs := ddd()
	delete(rs.Rules, "service")
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/service", RelDir: "internal/service", Imports: []Import{
			mkImport("fmt", ClassStd, "", false),
		}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 || res.Violations[0].Rule != "default: deny" {
		t.Fatalf("want default: deny violation, got %+v", res.Violations)
	}
}
