package golang

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// BenchmarkLoadLarge measures a metadata load of a synthetic 300-package
// module (a chain where each package imports the previous plus std), to check
// the loader stays well under a second on a large module — the PLAN's target,
// and the data behind any future caching decision. Run with:
//
//	go test ./internal/lang/golang -bench BenchmarkLoadLarge -run '^$'
func BenchmarkLoadLarge(b *testing.B) {
	const n = 300
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bench.test/large\n\ngo 1.21\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		pkg := filepath.Join(dir, "internal", fmt.Sprintf("p%03d", i))
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			b.Fatal(err)
		}
		src := fmt.Sprintf("package p%03d\n\nimport _ \"strings\"\n", i)
		if i > 0 {
			src = fmt.Sprintf("package p%03d\n\nimport (\n\t_ \"fmt\"\n\t_ \"bench.test/large/internal/p%03d\"\n)\n", i, i-1)
		}
		if err := os.WriteFile(filepath.Join(pkg, "x.go"), []byte(src), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	b.Setenv("GOWORK", "off")
	l := &Loader{Dir: dir}
	ctx := context.Background()
	g, err := l.Load(ctx)
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	if len(g.Packages) < n {
		b.Fatalf("loaded %d packages, want >= %d", len(g.Packages), n)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := l.Load(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// TestLoadDegradedFallback proves the Go adapter degrades to a best-effort graph
// (rather than aborting) when `go list` cannot resolve every import — the
// "works on code that doesn't compile yet" property the pure-static adapters
// have. The resolvable edges still classify exactly; the unresolved one is
// bucketed by the path heuristic; a human-actionable warning is recorded.
func TestLoadDegradedFallback(t *testing.T) {
	t.Setenv("GOWORK", "off") // hermetic on machines using workspaces

	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/deg\n\ngo 1.21\n")
	// Package a imports stdlib (strings), a resolvable sibling (b), and an
	// in-module path that does not exist — the unresolved import go list reports.
	write("a/a.go", "package a\n\nimport (\n\t\"strings\"\n\n\t\"example.com/deg/b\"\n\t\"example.com/deg/missing\"\n)\n\nvar _ = strings.TrimSpace\nvar _ = b.V\nvar _ = missing.X\n")
	write("b/b.go", "package b\n\nvar V = 1\n")

	g, err := (&Loader{Dir: dir}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load degraded module returned error (should degrade, not abort): %v", err)
	}
	if len(g.LoadWarnings) == 0 {
		t.Fatalf("expected a LoadWarning for the unresolved import; got none")
	}
	if !strings.Contains(g.LoadWarnings[0], "go mod download") {
		t.Errorf("warning is not actionable (no fix): %q", g.LoadWarnings[0])
	}

	byPath := make(map[string]core.Package, len(g.Packages))
	for _, p := range g.Packages {
		byPath[p.ImportPath] = p
	}
	a, ok := byPath["example.com/deg/a"]
	if !ok {
		t.Fatalf("package a missing from degraded graph (have %d packages)", len(g.Packages))
	}
	find := func(imp string) (core.Import, bool) {
		for _, i := range a.Imports {
			if i.Path == imp {
				return i, true
			}
		}
		return core.Import{}, false
	}
	if i, ok := find("strings"); !ok || i.Class != core.ClassStd {
		t.Errorf("resolvable std edge missing/misclassified: %+v ok=%v", i, ok)
	}
	if i, ok := find("example.com/deg/b"); !ok || i.Class != core.ClassInModule {
		t.Errorf("resolvable in-module edge missing/misclassified: %+v ok=%v", i, ok)
	}
	// The unresolved import survives, bucketed by the heuristic as in-module.
	if i, ok := find("example.com/deg/missing"); !ok || i.Class != core.ClassInModule || i.RelDir != "missing" {
		t.Errorf("unresolved import missing/misclassified (want in-module RelDir=missing): %+v ok=%v", i, ok)
	}
}
