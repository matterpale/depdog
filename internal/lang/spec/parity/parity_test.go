package parity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang/ruby"
	"github.com/matterpale/depdog/internal/lang/spec"
)

// loadRubySpec loads the illustrative Ruby spec (the declarative re-expression of
// the hand-written adapter) from the engine's testdata.
func loadRubySpec(t *testing.T) *spec.Spec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "ruby.yaml"))
	if err != nil {
		t.Fatalf("reading ruby.yaml: %v", err)
	}
	sp, err := spec.Load(data)
	if err != nil {
		t.Fatalf("loading ruby spec: %v", err)
	}
	return sp
}

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// TestSpecReproducesRubyAdapter is the trust gate: for each of the hand-written
// Ruby adapter's own fixtures, the spec-driven engine must produce a graph
// byte-identical to ruby.Loader's. Any divergence fails here — the engine is only
// trustworthy once it reproduces a hand-written adapter on its own goldens.
func TestSpecReproducesRubyAdapter(t *testing.T) {
	sp := loadRubySpec(t)

	// Fixtures copied from internal/lang/ruby/loader_test.go so the two loaders run
	// on identical inputs.
	fixtures := []struct {
		name     string
		files    map[string]string
		patterns []string
	}{
		{
			name: "layered project with a gemspec",
			files: map[string]string{
				"Gemfile":           "source \"https://rubygems.org\"\n",
				"example.gemspec":   "Gem::Specification.new do |spec|\n  spec.name = \"example-rb\"\nend\n",
				"domain/order.rb":   "require \"securerandom\"\n",
				"service/orders.rb": "require \"logger\"\nrequire_relative \"../domain/order\"\n",
				"service/orders_spec.rb": "require \"minitest/autorun\"\n" +
					"require_relative \"orders\"\n",
				"handler/api.rb": "require \"json\"\nrequire \"sinatra/base\"\n" +
					"require_relative \"../service/orders\"\n",
			},
		},
		{
			name: "relative imports including a self-edge",
			files: map[string]string{
				"Gemfile":  "source \"https://rubygems.org\"\n",
				"pkg/a.rb": "require_relative \"b\"\nrequire_relative \"../pkg/a\"\n",
				"pkg/b.rb": "",
			},
		},
		{
			name: "pattern-scoped load",
			files: map[string]string{
				"Gemfile": "source \"https://rubygems.org\"\n",
				"a/x.rb":  "require \"json\"\n",
				"b/y.rb":  "require \"set\"\n",
			},
			patterns: []string{"a"},
		},
		{
			name: "root node",
			files: map[string]string{
				"Gemfile": "source \"https://rubygems.org\"\n",
				"main.rb": "require \"json\"\n",
			},
		},
		{
			name: "skips vendor/tmp/dotdirs",
			files: map[string]string{
				"Gemfile":            "source \"https://rubygems.org\"\n",
				"app/main.rb":        "require \"json\"\n",
				"vendor/bundle/x.rb": "require \"bad\"\n",
				"tmp/cache/y.rb":     "require \"bad\"\n",
				".hidden/secret.rb":  "require \"bad\"\n",
			},
		},
		{
			name: "gemspec-only root",
			files: map[string]string{
				"thing.gemspec": "Gem::Specification.new { |s| s.name = \"g\" }\n",
				"lib/thing.rb":  "require \"json\"\n",
			},
		},
		{
			name: "nested std feature and external gem",
			files: map[string]string{
				"Gemfile": "source \"https://rubygems.org\"\n",
				"net/client.rb": "require \"net/http\"\nrequire \"rexml/document\"\n" +
					"require \"aws-sdk-s3\"\nautoload :Thing, \"domain/thing\"\n",
				"domain/thing.rb": "",
			},
		},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			root := writeProject(t, fx.files)

			want, err := (&ruby.Loader{Dir: root}).Load(context.Background(), fx.patterns...)
			if err != nil {
				t.Fatalf("ruby.Loader: %v", err)
			}
			got, err := (&spec.Loader{Spec: sp, Dir: root}).Load(context.Background(), fx.patterns...)
			if err != nil {
				t.Fatalf("spec.Loader: %v", err)
			}

			if !reflect.DeepEqual(want, got) {
				t.Errorf("spec engine diverges from ruby adapter:\n--- ruby (want) ---\n%s\n--- spec (got) ---\n%s",
					dumpGraph(want), dumpGraph(got))
			}
		})
	}
}

// dumpGraph renders a graph as a stable, human-readable string for diffing.
func dumpGraph(g *core.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "module=%s\n", g.ModulePath)
	pkgs := append([]core.Package(nil), g.Packages...)
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	for _, p := range pkgs {
		fmt.Fprintf(&b, "node %s (relDir=%s)\n", p.ImportPath, p.RelDir)
		for _, imp := range p.Imports {
			fmt.Fprintf(&b, "  %s [%s] relDir=%q testOnly=%v pos=%v\n",
				imp.Path, imp.Class, imp.RelDir, imp.TestOnly, imp.Positions)
		}
	}
	return b.String()
}
