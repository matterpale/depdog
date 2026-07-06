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
	if len(res.Warnings) != 1 || res.Warnings[0].RelDir != "pkg/util" {
		t.Fatalf("warnings = %+v, want one for pkg/util", res.Warnings)
	}
	if len(res.Violations) != 1 || res.Violations[0].Target != "unassigned" {
		t.Fatalf("violations = %+v, want one with target unassigned", res.Violations)
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
	if len(res.Violations) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("skipped packages should be invisible: %+v %+v", res.Violations, res.Warnings)
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
