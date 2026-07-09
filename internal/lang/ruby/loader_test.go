package ruby

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

// buildProject lays down a layered Ruby project and returns its root.
func buildProject(t *testing.T) string {
	t.Helper()
	return setupProject(t, map[string]string{
		"Gemfile":           "source \"https://rubygems.org\"\n",
		"example.gemspec":   "Gem::Specification.new do |spec|\n  spec.name = \"example-rb\"\nend\n",
		"domain/order.rb":   "require \"securerandom\"\n",
		"service/orders.rb": "require \"logger\"\nrequire_relative \"../domain/order\"\n",
		"service/orders_spec.rb": "require \"minitest/autorun\"\n" +
			"require_relative \"orders\"\n",
		"handler/api.rb": "require \"json\"\nrequire \"sinatra/base\"\n" +
			"require_relative \"../service/orders\"\n",
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

	if g.ModulePath != "example-rb" {
		t.Errorf("ModulePath = %q, want example-rb", g.ModulePath)
	}
	if len(g.Packages) != 3 {
		t.Fatalf("want 3 nodes, got %d: %v", len(g.Packages), relDirs(g))
	}

	// Classification: stdlib vs external vs in-module.
	domain := findPkg(g, "domain")
	if imp := findImport(domain, "securerandom"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("domain->securerandom should be std, got %+v", imp)
	}

	service := findPkg(g, "service")
	if imp := findImport(service, "../domain/order"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "domain" {
		t.Errorf("service->../domain/order should be in-module domain, got %+v", imp)
	}
	if imp := findImport(service, "logger"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("service->logger should be std, got %+v", imp)
	}

	handler := findPkg(g, "handler")
	if imp := findImport(handler, "sinatra/base"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("handler->sinatra/base should be external, got %+v", imp)
	}
	if imp := findImport(handler, "json"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("handler->json should be std, got %+v", imp)
	}
	if imp := findImport(handler, "../service/orders"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "service" {
		t.Errorf("handler->../service/orders should be in-module service, got %+v", imp)
	}
}

func TestLoadTestOnlyAttribution(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "service")
	// `require_relative "orders"` appears only in the spec file -> test-only edge.
	imp := findImport(service, "orders")
	if imp == nil {
		t.Fatalf("service->orders edge missing")
	}
	if !imp.TestOnly {
		t.Errorf("service->orders should be test-only (from orders_spec.rb)")
	}
	// Positions are module-relative.
	if len(imp.Positions) == 0 || imp.Positions[0].File != "service/orders_spec.rb" {
		t.Errorf("positions = %+v, want service/orders_spec.rb", imp.Positions)
	}
	// The domain edge is a production edge.
	if prod := findImport(service, "../domain/order"); prod == nil || prod.TestOnly {
		t.Errorf("service->../domain/order should be a production edge, got %+v", prod)
	}
}

func TestLoadRelativeImports(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Gemfile":  "source \"https://rubygems.org\"\n",
		"pkg/a.rb": "require_relative \"b\"\nrequire_relative \"../pkg/a\"\n",
		"pkg/b.rb": "",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pkg := findPkg(g, "pkg")
	if pkg == nil {
		t.Fatalf("pkg node missing: %v", relDirs(g))
	}
	// `require_relative "b"` -> sibling file in pkg, display "b".
	if imp := findImport(pkg, "b"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "pkg" {
		t.Errorf("`require_relative \"b\"` should resolve to in-module pkg, got %+v", imp)
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
		"Gemfile": "source \"https://rubygems.org\"\n",
		"a/x.rb":  "require \"json\"\n",
		"b/y.rb":  "require \"set\"\n",
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
		"Gemfile": "source \"https://rubygems.org\"\n",
		"main.rb": "require \"json\"\n",
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
}

func TestLoadSkipsVendorTmpAndDotDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Gemfile":            "source \"https://rubygems.org\"\n",
		"app/main.rb":        "require \"json\"\n",
		"vendor/bundle/x.rb": "require \"bad\"\n",
		"tmp/cache/y.rb":     "require \"bad\"\n",
		".hidden/secret.rb":  "require \"bad\"\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range g.Packages {
		for _, skip := range []string{"vendor", "tmp", ".hidden"} {
			if strings.Contains(p.RelDir, skip) {
				t.Errorf("node %q should have been skipped (%s)", p.RelDir, skip)
			}
		}
	}
}

func TestLoadMissingRoot(t *testing.T) {
	// A directory with none of the Ruby markers anywhere above it.
	dir := t.TempDir()
	_, err := (&Loader{Dir: dir}).Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for a project with no Gemfile/Rakefile/gemspec markers")
	}
	msg := err.Error()
	for _, want := range []string{"Gemfile", ".ruby-version", "Rakefile", "gemspec"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should mention %q", msg, want)
		}
	}
}

func TestProjectRootFromGemspec(t *testing.T) {
	// A directory with only a *.gemspec (no Gemfile/Rakefile) is still a root.
	root := setupProject(t, map[string]string{
		"thing.gemspec": "Gem::Specification.new { |s| s.name = \"g\" }\n",
		"lib/thing.rb":  "require \"json\"\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.ModulePath != "g" {
		t.Errorf("ModulePath = %q, want g", g.ModulePath)
	}
}

func TestProjectNameSources(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "gemspec spec receiver",
			files: map[string]string{"a.gemspec": "Gem::Specification.new do |spec|\n  spec.name = \"specname\"\nend\n"},
			want:  "specname",
		},
		{
			name:  "gemspec s receiver",
			files: map[string]string{"a.gemspec": "Gem::Specification.new do |s|\n  s.name = 'sname'\nend\n"},
			want:  "sname",
		},
		{
			name:  "gemspec name with freeze chain",
			files: map[string]string{"a.gemspec": "spec.name = \"frozen\".freeze\n"},
			want:  "frozen",
		},
		{
			name:  "gemspec comment ignored",
			files: map[string]string{"a.gemspec": "# spec.name = \"commented\"\nspec.name = \"real\"\n"},
			want:  "real",
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
	// A bare Gemfile with no gemspec falls back to the dir basename.
	root := setupProject(t, map[string]string{"Gemfile": "source \"https://rubygems.org\"\n"})
	got := projectName(root)
	if got == "" || strings.Contains(got, "/") {
		t.Errorf("projectName fallback = %q, want the root basename", got)
	}
}

func TestIsTestFile(t *testing.T) {
	cases := map[string]bool{
		"service/orders_spec.rb": true,
		"test/thing_test.rb":     true,
		"spec/models/user.rb":    true,
		"domain/order.rb":        false,
		"lib/app.rb":             false,
	}
	for file, want := range cases {
		if got := isTestFile(file); got != want {
			t.Errorf("isTestFile(%q) = %v, want %v", file, got, want)
		}
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
