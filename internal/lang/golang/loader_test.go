package golang

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func TestLoadCleanFixture(t *testing.T) {
	t.Setenv("GOWORK", "off") // keep the test hermetic on machines using workspaces

	l := &Loader{Dir: filepath.Join("..", "..", "..", "testdata", "fixtures", "clean")}
	g, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.ModulePath != "example.test/clean" {
		t.Fatalf("module = %q", g.ModulePath)
	}

	byPath := make(map[string]core.Package, len(g.Packages))
	for _, p := range g.Packages {
		byPath[p.ImportPath] = p
	}
	for _, want := range []string{
		"example.test/clean/cmd/app",
		"example.test/clean/internal/domain/order",
		"example.test/clean/internal/handler",
		"example.test/clean/internal/repository",
		"example.test/clean/internal/service",
		"example.test/clean/internal/util",
	} {
		if _, ok := byPath[want]; !ok {
			t.Errorf("package %s missing from graph (have %d packages)", want, len(g.Packages))
		}
	}

	if app := byPath["example.test/clean/cmd/app"]; app.RelDir != "cmd/app" {
		t.Errorf("cmd/app RelDir = %q", app.RelDir)
	}

	find := func(pkg, imp string) core.Import {
		t.Helper()
		for _, i := range byPath[pkg].Imports {
			if i.Path == imp {
				return i
			}
		}
		t.Fatalf("%s does not import %s: %+v", pkg, imp, byPath[pkg].Imports)
		return core.Import{}
	}

	// std classification
	if i := find("example.test/clean/internal/domain/order", "strings"); i.Class != core.ClassStd {
		t.Errorf("strings classified as %v", i.Class)
	}

	// external classification via replace directive, production import
	i := find("example.test/clean/internal/repository", "example.test/extlib")
	if i.Class != core.ClassExternal || i.TestOnly {
		t.Errorf("repository→extlib: class=%v testOnly=%v", i.Class, i.TestOnly)
	}
	if len(i.Positions) == 0 || i.Positions[0].File != "internal/repository/repo.go" || i.Positions[0].Line == 0 {
		t.Errorf("repository→extlib positions = %+v", i.Positions)
	}

	// test-only external import, merged from the test variant
	if i := find("example.test/clean/internal/service", "example.test/extlib"); i.Class != core.ClassExternal || !i.TestOnly {
		t.Errorf("service→extlib: class=%v testOnly=%v, want external test-only", i.Class, i.TestOnly)
	}

	// in-module classification, and prod import stays prod even though the
	// test file imports it too
	if i := find("example.test/clean/internal/service", "example.test/clean/internal/domain/order"); i.Class != core.ClassInModule || i.RelDir != "internal/domain/order" || i.TestOnly {
		t.Errorf("service→order: %+v", i)
	}
}

// BenchmarkLoad measures a full metadata load of the dirty fixture (which pulls
// in its external extlib sibling), the check pipeline's bottleneck. Run with:
//
//	go test ./internal/lang/golang -bench BenchmarkLoad -run '^$'
func BenchmarkLoad(b *testing.B) {
	b.Setenv("GOWORK", "off")
	l := &Loader{Dir: filepath.Join("..", "..", "..", "testdata", "fixtures", "dirty")}
	ctx := context.Background()
	if _, err := l.Load(ctx); err != nil { // fail fast before timing
		b.Fatalf("Load: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := l.Load(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
