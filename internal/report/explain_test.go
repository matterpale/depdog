package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func explainFixture() (*core.RuleSet, []core.PackageView, *core.Result) {
	rs := &core.RuleSet{
		Components: []core.Component{{Name: "domain", Patterns: []string{"internal/domain/**"}}},
		Rules:      map[string]core.Rule{"domain": {Allow: []core.Ref{{Kind: core.RefStd}}}},
		Policy:     core.PolicyDeny,
	}
	views := []core.PackageView{
		{ImportPath: "m/internal/domain", Component: "domain", Imports: []core.ImportView{
			{Path: "fmt", Class: core.ClassStd},
			{Path: "m/internal/repo", Class: core.ClassInModule, Component: "repository"},
		}, Importers: []string{"m/cmd/app"}},
	}
	res := &core.Result{
		ModulePath: "m",
		Violations: []core.Violation{{
			FromPackage: "m/internal/domain", FromComponent: "domain",
			ImportPath: "m/internal/repo", Rule: "domain: allow [std]",
			Positions: []core.Position{{File: "internal/domain/x.go", Line: 3}},
		}},
	}
	return rs, views, res
}

func TestExplainComponent(t *testing.T) {
	rs, views, res := explainFixture()
	var buf bytes.Buffer
	if err := Explain(&buf, "domain", rs, views, res); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"component domain", "allow:    [std]", "m/internal/domain", "violations (1)", "m/internal/repo"} {
		if !strings.Contains(out, want) {
			t.Errorf("component explain missing %q\n%s", want, out)
		}
	}
}

func TestExplainPackage(t *testing.T) {
	rs, views, res := explainFixture()
	var buf bytes.Buffer
	if err := Explain(&buf, "m/internal/domain", rs, views, res); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"package m/internal/domain", "✗ m/internal/repo", "[repository]",
		"internal/domain/x.go:3", "fmt  [std]", "imported by (1)", "m/cmd/app",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("package explain missing %q\n%s", want, out)
		}
	}
}

func TestExplainEdge(t *testing.T) {
	rs := &core.RuleSet{
		Components: []core.Component{
			{Name: "domain", Patterns: []string{"internal/domain/**"}},
			{Name: "repository", Patterns: []string{"internal/repository/**"}},
		},
		Rules:  map[string]core.Rule{"domain": {Allow: []core.Ref{{Kind: core.RefStd}}}},
		Policy: core.PolicyDeny,
	}
	views := []core.PackageView{
		{ImportPath: "m/internal/domain", Component: "domain"},
		{ImportPath: "m/internal/repo", Component: "repository"},
	}
	res := &core.Result{ModulePath: "m"}

	var denied, allowed bytes.Buffer
	if err := ExplainEdge(&denied, "m/internal/domain", "repository", rs, views, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(denied.String(), "denied by domain: allow [std]") {
		t.Errorf("expected denial by the domain rule:\n%s", denied.String())
	}
	if err := ExplainEdge(&allowed, "m/internal/domain", "std", rs, views, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(allowed.String(), "allowed by domain: allow [std]") {
		t.Errorf("expected std to be allowed:\n%s", allowed.String())
	}
	if err := ExplainEdge(&bytes.Buffer{}, "m/ghost", "std", rs, views, res); err == nil {
		t.Error("an unresolvable source should error")
	}
	if err := ExplainEdge(&bytes.Buffer{}, "m/internal/domain", "ghost", rs, views, res); err == nil {
		t.Error("an unresolvable target should error")
	}
}

func TestExplainNotFound(t *testing.T) {
	rs, views, res := explainFixture()
	if err := Explain(&bytes.Buffer{}, "ghost", rs, views, res); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("want a not-found error naming the target, got %v", err)
	}
}
