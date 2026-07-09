package python

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// interface assertion mirrors the golang/typescript adapters.
var _ lang.Loader = (*Loader)(nil)

// buildProject lays down a layered Python project and returns its root.
func buildProject(t *testing.T) string {
	t.Helper()
	return setupProject(t, map[string]string{
		"pyproject.toml":     "[project]\nname = \"example-py\"\n",
		"domain/__init__.py": "",
		"domain/order.py": `import uuid
from dataclasses import dataclass
`,
		"service/__init__.py": "",
		"service/orders.py": `import logging
from domain.order import Order
`,
		"service/orders_test.py": `import unittest
from service.orders import place_order
`,
		"handler/__init__.py": "",
		"handler/api.py": `import json
import requests
from service.orders import place_order
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

	if g.ModulePath != "example-py" {
		t.Errorf("ModulePath = %q, want example-py", g.ModulePath)
	}
	if len(g.Packages) != 3 {
		t.Fatalf("want 3 nodes, got %d: %v", len(g.Packages), relDirs(g))
	}

	// Classification: stdlib vs external vs in-module.
	domain := findPkg(g, "domain")
	if imp := findImport(domain, "uuid"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("domain->uuid should be std, got %+v", imp)
	}
	if imp := findImport(domain, "dataclasses"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("domain->dataclasses should be std, got %+v", imp)
	}

	service := findPkg(g, "service")
	if imp := findImport(service, "domain.order"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "domain" {
		t.Errorf("service->domain.order should be in-module domain, got %+v", imp)
	}
	if imp := findImport(service, "logging"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("service->logging should be std, got %+v", imp)
	}

	handler := findPkg(g, "handler")
	if imp := findImport(handler, "requests"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("handler->requests should be external, got %+v", imp)
	}
	if imp := findImport(handler, "service.orders"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "service" {
		t.Errorf("handler->service.orders should be in-module service, got %+v", imp)
	}
}

func TestLoadTestOnlyAttribution(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "service")
	// service.orders is imported only from the test file -> test-only edge.
	imp := findImport(service, "service.orders")
	if imp == nil {
		t.Fatalf("service->service.orders edge missing")
	}
	if !imp.TestOnly {
		t.Errorf("service->service.orders should be test-only (from orders_test.py)")
	}
	// Positions are module-relative.
	if len(imp.Positions) == 0 || imp.Positions[0].File != "service/orders_test.py" {
		t.Errorf("positions = %+v, want service/orders_test.py", imp.Positions)
	}
	// The domain edge is a production edge.
	if prod := findImport(service, "domain.order"); prod == nil || prod.TestOnly {
		t.Errorf("service->domain.order should be a production edge, got %+v", prod)
	}
}

func TestLoadRelativeImports(t *testing.T) {
	root := setupProject(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"rel\"\n",
		"pkg/__init__.py": "",
		"pkg/a.py":        "from . import b\nfrom ..pkg import a\n",
		"pkg/b.py":        "",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pkg := findPkg(g, "pkg")
	if pkg == nil {
		t.Fatalf("pkg node missing: %v", relDirs(g))
	}
	// `from . import b` -> current package dir (pkg), display ".".
	if imp := findImport(pkg, "."); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "pkg" {
		t.Errorf("`from . import b` should resolve to in-module pkg, got %+v", imp)
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
		"pyproject.toml": "[project]\nname = \"patterned\"\n",
		"a/__init__.py":  "",
		"a/x.py":         "import os\n",
		"b/__init__.py":  "",
		"b/y.py":         "import sys\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background(), "a")
	if err != nil {
		t.Fatalf("Load with pattern: %v", err)
	}
	for _, p := range g.Packages {
		if strings.HasPrefix(p.RelDir, "b") {
			t.Errorf("pattern a should not include %q", p.RelDir)
		}
	}
	if len(g.Packages) == 0 {
		t.Errorf("pattern a produced no nodes")
	}
}

func TestLoadRelDirRoot(t *testing.T) {
	root := setupProject(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"rooted\"\n",
		"main.py":        "import os\n",
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

func TestLoadSkipsVenvsCachesAndDotDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"pyproject.toml":        "[project]\nname = \"skips\"\n",
		"app/__init__.py":       "",
		"app/main.py":           "import os\n",
		".venv/lib/junk.py":     "import bad\n",
		"__pycache__/cached.py": "import bad\n",
		".hidden/secret.py":     "import bad\n",
		"build/generated.py":    "import bad\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range g.Packages {
		for _, skip := range []string{".venv", "__pycache__", ".hidden", "build"} {
			if strings.Contains(p.RelDir, skip) {
				t.Errorf("node %q should have been skipped (%s)", p.RelDir, skip)
			}
		}
	}
}

func TestLoadMissingRoot(t *testing.T) {
	// A directory with none of the Python markers anywhere above it.
	dir := t.TempDir()
	_, err := (&Loader{Dir: dir}).Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for a project with no pyproject/setup markers")
	}
	msg := err.Error()
	for _, want := range []string{"pyproject.toml", "setup.py", "setup.cfg"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should mention %q", msg, want)
		}
	}
}

func TestProjectNameSources(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "pyproject PEP 621",
			files: map[string]string{"pyproject.toml": "[project]\nname = \"pep621\"\n"},
			want:  "pep621",
		},
		{
			name:  "pyproject poetry",
			files: map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"poet\"\n"},
			want:  "poet",
		},
		{
			name:  "pyproject with comment",
			files: map[string]string{"pyproject.toml": "[project]\nname = \"withcomment\"  # the name\n"},
			want:  "withcomment",
		},
		{
			name:  "setup.cfg metadata",
			files: map[string]string{"setup.py": "", "setup.cfg": "[metadata]\nname = cfgname\n"},
			want:  "cfgname",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := setupProject(t, tc.files)
			if got := projectName(root); got != tc.want {
				t.Errorf("projectName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProjectNameFallsBackToDir(t *testing.T) {
	// A bare setup.py with no name anywhere falls back to the dir basename.
	root := setupProject(t, map[string]string{"setup.py": "from setuptools import setup\nsetup()\n"})
	got := projectName(root)
	if got == "" || strings.Contains(got, "/") {
		t.Errorf("projectName fallback = %q, want the root basename", got)
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
