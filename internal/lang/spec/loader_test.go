package spec

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

var _ lang.Loader = (*Loader)(nil)

// setupProject lays down files (relative paths -> contents) under a fresh temp
// dir and returns the root.
func setupProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
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

// rubyProject mirrors ruby/loader_test.go's buildProject so the path-mode loader
// is exercised on a realistic layered project.
func rubyProject(t *testing.T) string {
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

func TestLoadPathGraph(t *testing.T) {
	root := rubyProject(t)
	g, err := (&Loader{Spec: rubySpec(t), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if g.ModulePath != "example-rb" {
		t.Errorf("ModulePath = %q, want example-rb (from gemspec)", g.ModulePath)
	}
	if len(g.Packages) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(g.Packages))
	}

	if imp := findImport(findPkg(g, "domain"), "securerandom"); imp == nil || imp.Class != core.ClassStd {
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
	root := rubyProject(t)
	g, err := (&Loader{Spec: rubySpec(t), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "service")
	imp := findImport(service, "orders")
	if imp == nil {
		t.Fatalf("service->orders edge missing")
	}
	if !imp.TestOnly {
		t.Errorf("service->orders should be test-only (from orders_spec.rb)")
	}
	if len(imp.Positions) == 0 || imp.Positions[0].File != "service/orders_spec.rb" {
		t.Errorf("positions = %+v, want service/orders_spec.rb", imp.Positions)
	}
	if prod := findImport(service, "../domain/order"); prod == nil || prod.TestOnly {
		t.Errorf("service->../domain/order should be a production edge, got %+v", prod)
	}
}

func TestLoadRootNodeAndEmptyFiles(t *testing.T) {
	// A single root-level file, plus an import-less sibling, produces a "." node
	// and a node for the empty file's directory.
	root := setupProject(t, map[string]string{
		"Gemfile":  "source \"x\"\n",
		"main.rb":  "require \"json\"\n",
		"lib/x.rb": "", // no imports; still a node
	})
	g, err := (&Loader{Spec: rubySpec(t), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if findPkg(g, ".") == nil {
		t.Errorf("root node . missing: %v", relDirsOf(g))
	}
	if lib := findPkg(g, "lib"); lib == nil {
		t.Errorf("empty-file node lib missing: %v", relDirsOf(g))
	} else if len(lib.Imports) != 0 {
		t.Errorf("lib should have no edges, got %+v", lib.Imports)
	}
}

func TestLoadDeterministicAndSorted(t *testing.T) {
	root := rubyProject(t)
	l := &Loader{Spec: rubySpec(t), Dir: root}
	var prev *core.Graph
	for iter := 0; iter < 3; iter++ {
		g, err := l.Load(context.Background())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !sort.SliceIsSorted(g.Packages, func(i, j int) bool { return g.Packages[i].ImportPath < g.Packages[j].ImportPath }) {
			t.Errorf("packages not sorted by ImportPath")
		}
		for _, p := range g.Packages {
			if !sort.SliceIsSorted(p.Imports, func(i, j int) bool { return p.Imports[i].Path < p.Imports[j].Path }) {
				t.Errorf("%s imports not sorted", p.ImportPath)
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
		"Gemfile": "source \"x\"\n",
		"a/x.rb":  "require \"json\"\n",
		"b/y.rb":  "require \"set\"\n",
	})
	g, err := (&Loader{Spec: rubySpec(t), Dir: root}).Load(context.Background(), "a")
	if err != nil {
		t.Fatalf("Load with pattern: %v", err)
	}
	if findPkg(g, "b") != nil {
		t.Errorf("pattern a should not include node b")
	}
	if findPkg(g, "a") == nil {
		t.Errorf("pattern a produced no a node")
	}
}

func TestLoadSkipsConfiguredDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Gemfile":            "source \"x\"\n",
		"app/main.rb":        "require \"json\"\n",
		"vendor/bundle/x.rb": "require \"bad\"\n",
		"tmp/cache/y.rb":     "require \"bad\"\n",
		".hidden/secret.rb":  "require \"bad\"\n",
	})
	g, err := (&Loader{Spec: rubySpec(t), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range g.Packages {
		for _, bad := range []string{"vendor", "tmp", ".hidden"} {
			if p.RelDir == bad || strings.HasPrefix(p.RelDir, bad+"/") {
				t.Errorf("node %q should have been skipped", p.RelDir)
			}
		}
	}
}

func TestLoadModuleLabelFallsBackToBasename(t *testing.T) {
	// No gemspec -> ModulePath is the root directory basename.
	root := setupProject(t, map[string]string{
		"Gemfile": "source \"x\"\n",
		"main.rb": "require \"json\"\n",
	})
	g, err := (&Loader{Spec: rubySpec(t), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.ModulePath != filepath.Base(root) {
		t.Errorf("ModulePath = %q, want basename %q", g.ModulePath, filepath.Base(root))
	}
}

func TestLoadGemspecOnlyRoot(t *testing.T) {
	// A directory with only a *.gemspec (glob marker) is a valid root.
	root := setupProject(t, map[string]string{
		"thing.gemspec": "Gem::Specification.new { |s| s.name = \"g\" }\n",
		"lib/thing.rb":  "require \"json\"\n",
	})
	g, err := (&Loader{Spec: rubySpec(t), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.ModulePath != "g" {
		t.Errorf("ModulePath = %q, want g", g.ModulePath)
	}
}

func TestLoadMissingRootErrors(t *testing.T) {
	_, err := (&Loader{Spec: rubySpec(t), Dir: t.TempDir()}).Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for a project with no markers")
	}
	for _, want := range []string{"Gemfile", "Rakefile", "*.gemspec"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}

// indexFileSpec previews path resolution with a "." separator and index files
// (Lua/Python style): `req "pkg.sub"` -> pkg/sub, `req "pkg"` -> pkg/init via the
// index file.
func indexFileSpec() *Spec {
	return &Spec{
		Name:       "lx",
		Markers:    []string{"lx.toml"},
		Extensions: []string{".lx"},
		Strings:    []StringForm{{Kind: KindQuoted, Open: "\"", Escape: "\\"}},
		Imports:    []Surface{{Keyword: "req", Capture: CaptureString, Kind: "plain"}},
		Resolve: Resolve{
			Mode:       ModePath,
			Separator:  ".",
			Roots:      []string{"."},
			Extensions: []string{".lx"},
			IndexFiles: []string{"init.lx"},
		},
	}
}

func TestLoadIndexFilesAndSeparator(t *testing.T) {
	root := setupProject(t, map[string]string{
		"lx.toml":     "",
		"main.lx":     "req \"pkg.sub\"\nreq \"pkg\"\nreq \"external.thing\"\n",
		"pkg/sub.lx":  "",
		"pkg/init.lx": "",
	})
	g, err := (&Loader{Spec: indexFileSpec(), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rootPkg := findPkg(g, ".")
	if rootPkg == nil {
		t.Fatalf("root node missing: %v", relDirsOf(g))
	}
	// "pkg.sub" -> file pkg/sub.lx, whose directory node is pkg.
	if imp := findImport(rootPkg, "pkg.sub"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "pkg" {
		t.Errorf("pkg.sub should be in-module pkg, got %+v", imp)
	}
	// "pkg" -> directory pkg via its init.lx index file.
	if imp := findImport(rootPkg, "pkg"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "pkg" {
		t.Errorf("pkg (index file) should be in-module pkg, got %+v", imp)
	}
	if imp := findImport(rootPkg, "external.thing"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("external.thing should be external, got %+v", imp)
	}
}

// --- helpers ---

func relDirsOf(g *core.Graph) []string {
	out := make([]string, 0, len(g.Packages))
	for _, p := range g.Packages {
		out = append(out, p.RelDir)
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
