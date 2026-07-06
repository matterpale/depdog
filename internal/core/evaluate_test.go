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

func TestEvaluateNoRulePolicyDeny(t *testing.T) {
	rs := ddd()
	delete(rs.Rules, "service")
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/service", RelDir: "internal/service", Imports: []Import{
			mkImport("fmt", ClassStd, "", false),
		}},
	}}
	res := evaluate(t, g, rs)
	if len(res.Violations) != 1 || res.Violations[0].Rule != "policy: deny" {
		t.Fatalf("want policy: deny violation, got %+v", res.Violations)
	}
}
