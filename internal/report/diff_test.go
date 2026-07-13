package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

// pkg builds a package with in-module imports to the given rel dirs. Each
// import's path is derived from its rel dir under module "m"; the target's
// component is resolved by the RuleSet at build time via BuildPackageViews, so
// tests only need to place packages under component patterns.
func pkg(relDir string, imports ...string) core.Package {
	p := core.Package{ImportPath: "m/" + relDir, RelDir: relDir}
	for _, to := range imports {
		p.Imports = append(p.Imports, core.Import{
			Path:   "m/" + to,
			Class:  core.ClassInModule,
			RelDir: to,
		})
	}
	return p
}

// graph assembles a *core.Graph from packages (packages must already be given
// in sorted order for determinism, matching the adapter contract).
func graph(pkgs ...core.Package) *core.Graph {
	return &core.Graph{ModulePath: "m", Packages: pkgs}
}

// ruleSet maps each rel-dir prefix to a component of the same name, allowing
// everything (rules do not affect Diff — only component assignment does).
func ruleSet(components ...string) *core.RuleSet {
	rs := &core.RuleSet{Policy: core.PolicyAllow}
	for _, name := range components {
		rs.Components = append(rs.Components, core.Component{
			Name:     name,
			Patterns: []string{name + "/**"},
		})
	}
	return rs
}

func TestDiffAddedEdge(t *testing.T) {
	rs := ruleSet("a", "b")
	before := graph(pkg("a/x"), pkg("b/y"))
	after := graph(pkg("a/x", "b/y"), pkg("b/y"))

	d, err := Diff(before, after, rs)
	if err != nil {
		t.Fatal(err)
	}
	if d.AddedCount != 1 || d.RemovedCount != 0 {
		t.Fatalf("counts: added=%d removed=%d, want 1/0", d.AddedCount, d.RemovedCount)
	}
	got := d.Added[0]
	if got.From != "a" || got.To != "b" {
		t.Errorf("added edge = %s → %s, want a → b", got.From, got.To)
	}
	if got.CrossesBoundary {
		t.Errorf("no boundaries configured, edge should not cross one: %+v", got)
	}
}

func TestDiffRemovedEdge(t *testing.T) {
	rs := ruleSet("a", "b")
	before := graph(pkg("a/x", "b/y"), pkg("b/y"))
	after := graph(pkg("a/x"), pkg("b/y"))

	d, err := Diff(before, after, rs)
	if err != nil {
		t.Fatal(err)
	}
	if d.AddedCount != 0 || d.RemovedCount != 1 {
		t.Fatalf("counts: added=%d removed=%d, want 0/1", d.AddedCount, d.RemovedCount)
	}
	if got := d.Removed[0]; got.From != "a" || got.To != "b" {
		t.Errorf("removed edge = %s → %s, want a → b", got.From, got.To)
	}
}

func TestDiffNoChange(t *testing.T) {
	rs := ruleSet("a", "b")
	before := graph(pkg("a/x", "b/y"), pkg("b/y"))
	after := graph(pkg("a/x", "b/y"), pkg("b/y"))

	d, err := Diff(before, after, rs)
	if err != nil {
		t.Fatal(err)
	}
	if d.AddedCount != 0 || d.RemovedCount != 0 {
		t.Fatalf("an unchanged graph should diff empty: added=%d removed=%d", d.AddedCount, d.RemovedCount)
	}
}

func TestDiffIntraComponentIgnored(t *testing.T) {
	// a/x imports a/z: same component, not a cross-component edge.
	rs := ruleSet("a", "b")
	before := graph(pkg("a/x"), pkg("a/z"))
	after := graph(pkg("a/x", "a/z"), pkg("a/z"))

	d, err := Diff(before, after, rs)
	if err != nil {
		t.Fatal(err)
	}
	if d.AddedCount != 0 {
		t.Fatalf("an intra-component edge must be ignored, got %d added: %+v", d.AddedCount, d.Added)
	}
}

func TestDiffStdExternalIgnored(t *testing.T) {
	rs := ruleSet("a", "b")
	// after adds a std and an external import out of a/x — neither is a
	// cross-component edge.
	stdExt := pkg("a/x")
	stdExt.Imports = []core.Import{
		{Path: "fmt", Class: core.ClassStd},
		{Path: "github.com/pkg/errors", Class: core.ClassExternal},
	}
	before := graph(pkg("a/x"), pkg("b/y"))
	after := graph(stdExt, pkg("b/y"))

	d, err := Diff(before, after, rs)
	if err != nil {
		t.Fatal(err)
	}
	if d.AddedCount != 0 || d.RemovedCount != 0 {
		t.Fatalf("std/external targets must be ignored: added=%d removed=%d", d.AddedCount, d.RemovedCount)
	}
}

func TestDiffBoundaryCrossingFlagged(t *testing.T) {
	rs := ruleSet("a", "b")
	// A boundary making a and b mutually exclusive members: an a → b edge
	// crosses it.
	rs.Boundaries = []core.Boundary{{
		Name: "layers",
		Members: []core.BoundaryMember{
			{Component: "a", Patterns: []string{"a/**"}, Label: "a"},
			{Component: "b", Patterns: []string{"b/**"}, Label: "b"},
		},
	}}
	before := graph(pkg("a/x"), pkg("b/y"))
	after := graph(pkg("a/x", "b/y"), pkg("b/y"))

	d, err := Diff(before, after, rs)
	if err != nil {
		t.Fatal(err)
	}
	if d.AddedCount != 1 {
		t.Fatalf("want 1 added edge, got %d", d.AddedCount)
	}
	got := d.Added[0]
	if !got.CrossesBoundary || got.Boundary != "layers" {
		t.Errorf("added a → b should cross boundary %q, got %+v", "layers", got)
	}
	if d.BoundaryCrossings != 1 {
		t.Errorf("BoundaryCrossings = %d, want 1", d.BoundaryCrossings)
	}
}

func TestDiffDeterministicOrder(t *testing.T) {
	rs := ruleSet("a", "b", "c")
	// after adds three cross-component edges out of a; they must come back
	// sorted by from then to regardless of package/import ordering.
	before := graph(pkg("a/x"), pkg("b/y"), pkg("c/z"))
	after := graph(
		pkg("a/x", "c/z", "b/y"), // imports listed c before b on purpose
		pkg("b/y"),
		pkg("c/z"),
	)

	d, err := Diff(before, after, rs)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added) != 2 {
		t.Fatalf("want 2 added edges, got %d: %+v", len(d.Added), d.Added)
	}
	if d.Added[0].To != "b" || d.Added[1].To != "c" {
		t.Errorf("added edges not sorted by To: %+v", d.Added)
	}
}

func TestDiffTextEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := DiffText(&buf, ArchDiff{}, "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "no cross-component edge changes since HEAD~1") {
		t.Errorf("empty diff should read clearly:\n%s", out)
	}
}

func TestDiffTextSummaryAndEdges(t *testing.T) {
	d := ArchDiff{
		Added: []ComponentEdge{
			{From: "a", To: "b"},
			{From: "a", To: "c", CrossesBoundary: true, Boundary: "layers"},
		},
		Removed:           []ComponentEdge{{From: "d", To: "e"}},
		AddedCount:        2,
		RemovedCount:      1,
		BoundaryCrossings: 1,
	}
	var buf bytes.Buffer
	if err := DiffText(&buf, d, "main"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"depdog diff — since main",
		"2 cross-component edges added, 1 removed",
		"1 crosses a boundary",
		"+ a → b",
		`+ a → c  (crosses boundary "layers")`,
		"- d → e",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff text missing %q\n%s", want, out)
		}
	}
}
