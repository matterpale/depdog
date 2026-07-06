package core

import (
	"reflect"
	"testing"
)

func TestBuildPackageViews(t *testing.T) {
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/domain", RelDir: "internal/domain", Imports: []Import{
			mkImport("fmt", ClassStd, "", false),
			mkImport("m/internal/repo", ClassInModule, "internal/repository", false),
		}},
		{ImportPath: "m/internal/repo", RelDir: "internal/repository", Imports: nil},
	}}

	views, err := BuildPackageViews(g, ddd())
	if err != nil {
		t.Fatalf("BuildPackageViews: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %d, want 2", len(views))
	}

	dom := views[0]
	if dom.ImportPath != "m/internal/domain" || dom.Component != "domain" {
		t.Fatalf("domain view = %+v", dom)
	}
	if len(dom.Imports) != 2 {
		t.Fatalf("domain imports = %d, want 2", len(dom.Imports))
	}
	if dom.Imports[0].Class != ClassStd {
		t.Errorf("first import class = %v, want std", dom.Imports[0].Class)
	}
	if dom.Imports[1].Class != ClassInModule || dom.Imports[1].Component != "repository" {
		t.Errorf("second import = %+v, want in-module -> repository", dom.Imports[1])
	}

	repo := views[1]
	if repo.Component != "repository" {
		t.Errorf("repo component = %q", repo.Component)
	}
	if !reflect.DeepEqual(repo.Importers, []string{"m/internal/domain"}) {
		t.Errorf("repo importers = %v, want [m/internal/domain]", repo.Importers)
	}
}

func TestBuildPackageViewsSkips(t *testing.T) {
	rs := ddd()
	rs.Skip = []string{"internal/legacy/**"}
	g := &Graph{ModulePath: "m", Packages: []Package{
		{ImportPath: "m/internal/legacy", RelDir: "internal/legacy", Imports: nil},
		{ImportPath: "m/internal/domain", RelDir: "internal/domain", Imports: []Import{
			mkImport("m/internal/legacy", ClassInModule, "internal/legacy", false),
		}},
	}}
	views, err := BuildPackageViews(g, rs)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].ImportPath != "m/internal/domain" {
		t.Fatalf("skipped package should be omitted: %+v", views)
	}
	if len(views[0].Imports) != 0 {
		t.Errorf("edge into a skipped package should be omitted: %+v", views[0].Imports)
	}
}
