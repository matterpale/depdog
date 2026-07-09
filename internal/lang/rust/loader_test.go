package rust

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// interface assertion mirrors the golang/typescript/python adapters.
var _ lang.Loader = (*Loader)(nil)

// buildProject lays down a layered Rust crate and returns its root.
func buildProject(t *testing.T) string {
	t.Helper()
	return setupProject(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"example-rs\"\nversion = \"0.1.0\"\n",
		"src/lib.rs": `pub mod domain;
pub mod service;
pub mod handler;
`,
		"src/domain/mod.rs": `use std::fmt;
use uuid::Uuid;

pub struct Order {
    pub id: Uuid,
}
`,
		"src/service/mod.rs": `use std::collections::HashMap;
use crate::domain::Order;

pub fn place_order() {}
`,
		"src/service/tests.rs": `use crate::service::place_order;
`,
		"src/handler/mod.rs": `use std::io;
use serde::Serialize;
use crate::service::place_order;

pub fn handle() {}
`,
	})
}

func findPkg(g *core.Graph, relDir string) *core.Package {
	for i := range g.Packages {
		if g.Packages[i].RelDir == relDir {
			return &g.Packages[i]
		}
	}
	return nil
}

func findImport(pkg *core.Package, path string) *core.Import {
	if pkg == nil {
		return nil
	}
	for i := range pkg.Imports {
		if pkg.Imports[i].Path == path {
			return &pkg.Imports[i]
		}
	}
	return nil
}

func TestLoadBuildsGraph(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if g.ModulePath != "example-rs" {
		t.Errorf("ModulePath = %q, want example-rs", g.ModulePath)
	}

	// Classification: std vs external vs in-crate.
	domain := findPkg(g, "src/domain")
	if imp := findImport(domain, "std::fmt"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("domain->std::fmt should be std, got %+v", imp)
	}
	if imp := findImport(domain, "uuid::Uuid"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("domain->uuid::Uuid should be external, got %+v", imp)
	}

	service := findPkg(g, "src/service")
	if imp := findImport(service, "crate::domain::Order"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/domain" {
		t.Errorf("service->crate::domain::Order should be in-crate src/domain, got %+v", imp)
	}
	if imp := findImport(service, "std::collections::HashMap"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("service->std::collections::HashMap should be std, got %+v", imp)
	}

	handler := findPkg(g, "src/handler")
	if imp := findImport(handler, "serde::Serialize"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("handler->serde::Serialize should be external, got %+v", imp)
	}
	if imp := findImport(handler, "crate::service::place_order"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/service" {
		t.Errorf("handler->crate::service::place_order should be in-crate src/service, got %+v", imp)
	}
}

func TestLoadModDeclarations(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// src/lib.rs declares `pub mod domain;` etc. -> in-crate edges to the child dirs.
	src := findPkg(g, "src")
	if imp := findImport(src, "mod domain"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/domain" {
		t.Errorf("src->mod domain should be in-crate src/domain, got %+v", imp)
	}
	if imp := findImport(src, "mod service"); imp == nil || imp.RelDir != "src/service" {
		t.Errorf("src->mod service should be in-crate src/service, got %+v", imp)
	}
}

func TestLoadTestOnlyAttribution(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/service")
	// crate::service::place_order is imported only from tests.rs -> test-only edge.
	imp := findImport(service, "crate::service::place_order")
	if imp == nil {
		t.Fatalf("service->crate::service::place_order edge missing")
	}
	if !imp.TestOnly {
		t.Errorf("service->crate::service::place_order should be test-only (from tests.rs)")
	}
	if len(imp.Positions) == 0 || imp.Positions[0].File != "src/service/tests.rs" {
		t.Errorf("positions = %+v, want src/service/tests.rs", imp.Positions)
	}
	// The domain edge is a production edge.
	if prod := findImport(service, "crate::domain::Order"); prod == nil || prod.TestOnly {
		t.Errorf("service->crate::domain::Order should be a production edge, got %+v", prod)
	}
}

func TestLoadSelfSuperImports(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Cargo.toml":           "[package]\nname = \"rel\"\n",
		"src/lib.rs":           "pub mod domain;\n",
		"src/domain/mod.rs":    "pub mod order;\nuse self::order::Order;\n",
		"src/domain/order.rs":  "use super::helper;\npub struct Order;\n",
		"src/domain/helper.rs": "",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	domain := findPkg(g, "src/domain")
	if domain == nil {
		t.Fatalf("src/domain node missing: %v", relDirs(g))
	}
	// `use self::order::Order` -> current dir (src/domain).
	if imp := findImport(domain, "self::order::Order"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/domain" {
		t.Errorf("self::order::Order should resolve to in-crate src/domain, got %+v", imp)
	}
	// `use super::helper` from order.rs (in src/domain) -> parent dir (src).
	if imp := findImport(domain, "super::helper"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src" {
		t.Errorf("super::helper should resolve to in-crate src, got %+v", imp)
	}
}

func TestLoadIsDeterministicAndSorted(t *testing.T) {
	root := buildProject(t)
	l := &Loader{Dir: root}

	var prev *core.Graph
	for iter := 0; iter < 3; iter++ {
		g, err := l.Load(context.Background())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !sort.SliceIsSorted(g.Packages, func(i, j int) bool {
			return g.Packages[i].ImportPath < g.Packages[j].ImportPath
		}) {
			t.Errorf("packages not sorted by ImportPath: %v", importPaths(g))
		}
		for _, p := range g.Packages {
			if !sort.SliceIsSorted(p.Imports, func(i, j int) bool {
				return p.Imports[i].Path < p.Imports[j].Path
			}) {
				t.Errorf("%s imports not sorted by Path", p.ImportPath)
			}
			for _, imp := range p.Imports {
				if !sort.SliceIsSorted(imp.Positions, func(i, j int) bool {
					pi, pj := imp.Positions[i], imp.Positions[j]
					return pi.File < pj.File || (pi.File == pj.File && pi.Line < pj.Line)
				}) {
					t.Errorf("%s->%s positions not sorted", p.ImportPath, imp.Path)
				}
			}
		}
		if prev != nil && !graphsEqual(prev, g) {
			t.Errorf("Load not deterministic across runs")
		}
		prev = g
	}
}

func TestLoadWithPatterns(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Cargo.toml":   "[package]\nname = \"patterned\"\n",
		"src/lib.rs":   "pub mod a;\npub mod b;\n",
		"src/a/mod.rs": "use std::io;\n",
		"src/b/mod.rs": "use std::fmt;\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background(), "src/a")
	if err != nil {
		t.Fatalf("Load with pattern: %v", err)
	}
	for _, p := range g.Packages {
		if strings.HasPrefix(p.RelDir, "src/b") {
			t.Errorf("pattern src/a should not include %q", p.RelDir)
		}
	}
	if len(g.Packages) == 0 {
		t.Errorf("pattern src/a produced no nodes")
	}
}

func TestLoadRelDirRoot(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"rooted\"\n",
		"main.rs":    "use std::io;\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(g.Packages) != 1 {
		t.Fatalf("want 1 node, got %d: %v", len(g.Packages), relDirs(g))
	}
	if g.Packages[0].RelDir != "." {
		t.Errorf("root node RelDir = %q, want .", g.Packages[0].RelDir)
	}
	if g.Packages[0].ImportPath != "rooted" {
		t.Errorf("root node ImportPath = %q, want rooted", g.Packages[0].ImportPath)
	}
}

func TestLoadSkipsTargetAndDotDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Cargo.toml":           "[package]\nname = \"skips\"\n",
		"src/lib.rs":           "use std::io;\n",
		"target/debug/junk.rs": "use bad::thing;\n",
		".git/hooks/hook.rs":   "use bad::thing;\n",
		".hidden/secret.rs":    "use bad::thing;\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range g.Packages {
		for _, skip := range []string{"target", ".git", ".hidden"} {
			if strings.Contains(p.RelDir, skip) {
				t.Errorf("node %q should have been skipped (%s)", p.RelDir, skip)
			}
		}
	}
}

func TestLoadMissingRoot(t *testing.T) {
	// A directory with no Cargo.toml anywhere above it.
	dir := t.TempDir()
	_, err := (&Loader{Dir: dir}).Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for a project with no Cargo.toml marker")
	}
	if !strings.Contains(err.Error(), "Cargo.toml") {
		t.Errorf("error %q should mention Cargo.toml", err.Error())
	}
}

func TestCrateNameSources(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "package name",
			files: map[string]string{"Cargo.toml": "[package]\nname = \"mycrate\"\nversion = \"0.1.0\"\n"},
			want:  "mycrate",
		},
		{
			name:  "name with comment",
			files: map[string]string{"Cargo.toml": "[package]\nname = \"withcomment\" # the crate\n"},
			want:  "withcomment",
		},
		{
			name:  "name ignored outside package table",
			files: map[string]string{"Cargo.toml": "[dependencies]\nname = \"notthis\"\n\n[package]\nname = \"real\"\n"},
			want:  "real",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := setupProject(t, tc.files)
			if got := crateName(root); got != tc.want {
				t.Errorf("crateName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCrateNameFallsBackToDir(t *testing.T) {
	// A workspace-only manifest (no [package] name) falls back to the dir basename.
	root := setupProject(t, map[string]string{"Cargo.toml": "[workspace]\nmembers = [\"a\"]\n"})
	got := crateName(root)
	if got == "" || strings.Contains(got, "/") {
		t.Errorf("crateName fallback = %q, want the root basename", got)
	}
}

// helpers

func relDirs(g *core.Graph) []string {
	out := make([]string, 0, len(g.Packages))
	for _, p := range g.Packages {
		out = append(out, p.RelDir)
	}
	return out
}

func importPaths(g *core.Graph) []string {
	out := make([]string, 0, len(g.Packages))
	for _, p := range g.Packages {
		out = append(out, p.ImportPath)
	}
	return out
}

func graphsEqual(a, b *core.Graph) bool {
	if a.ModulePath != b.ModulePath || len(a.Packages) != len(b.Packages) {
		return false
	}
	for i := range a.Packages {
		pa, pb := a.Packages[i], b.Packages[i]
		if pa.ImportPath != pb.ImportPath || pa.RelDir != pb.RelDir || len(pa.Imports) != len(pb.Imports) {
			return false
		}
		for j := range pa.Imports {
			ia, ib := pa.Imports[j], pb.Imports[j]
			if ia.Path != ib.Path || ia.Class != ib.Class || ia.RelDir != ib.RelDir ||
				ia.TestOnly != ib.TestOnly || len(ia.Positions) != len(ib.Positions) {
				return false
			}
			for k := range ia.Positions {
				if ia.Positions[k] != ib.Positions[k] {
					return false
				}
			}
		}
	}
	return true
}
