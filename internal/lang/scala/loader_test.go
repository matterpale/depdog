package scala

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// interface assertion mirrors the golang/typescript/python/rust/java/kotlin
// adapters.
var _ lang.Loader = (*Loader)(nil)

// writeFile writes content to path, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupProject lays down files (keyed by slash-relative path) under a temp root.
func setupProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}
	return root
}

// buildProject lays down a layered Scala project and returns its root.
func buildProject(t *testing.T) string {
	t.Helper()
	return setupProject(t, map[string]string{
		"build.sbt": "name := \"example-scala\"\nscalaVersion := \"3.3.1\"\n",
		"src/main/scala/com/example/domain/Order.scala": `package com.example.domain

import java.util.concurrent.atomic.AtomicLong

class Order
`,
		"src/main/scala/com/example/service/OrderService.scala": `package com.example.service

import scala.collection.mutable.Map

import com.example.domain.Order

class OrderService
`,
		"src/main/scala/com/example/handler/OrderHandler.scala": `package com.example.handler

import java.io.IOException

import io.circe.Json

import com.example.service.OrderService

class OrderHandler
`,
		"src/test/scala/com/example/service/OrderServiceTest.scala": `package com.example.service

import com.example.domain.Order

class OrderServiceTest
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

func relDirs(g *core.Graph) []string {
	out := make([]string, 0, len(g.Packages))
	for _, p := range g.Packages {
		out = append(out, p.RelDir)
	}
	return out
}

func TestLoadBuildsGraph(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if g.ModulePath != "example-scala" {
		t.Errorf("ModulePath = %q, want example-scala", g.ModulePath)
	}

	domainDir := "src/main/scala/com/example/domain"
	serviceDir := "src/main/scala/com/example/service"

	domain := findPkg(g, domainDir)
	if imp := findImport(domain, "java.util.concurrent.atomic.AtomicLong"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("domain->AtomicLong should be std, got %+v", imp)
	}

	service := findPkg(g, serviceDir)
	if imp := findImport(service, "com.example.domain.Order"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != domainDir {
		t.Errorf("service->domain.Order should be in-module %s, got %+v", domainDir, imp)
	}
	if imp := findImport(service, "scala.collection.mutable.Map"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("service->scala.collection.mutable.Map should be std, got %+v", imp)
	}

	handler := findPkg(g, "src/main/scala/com/example/handler")
	if imp := findImport(handler, "io.circe.Json"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("handler->circe.Json should be external, got %+v", imp)
	}
	if imp := findImport(handler, "com.example.service.OrderService"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != serviceDir {
		t.Errorf("handler->service.OrderService should be in-module %s, got %+v", serviceDir, imp)
	}
}

func TestLoadTestOnlyEdge(t *testing.T) {
	// The production service source's import of domain.Order is a production edge.
	// The test source lives under src/test/... so it is its own node whose
	// domain.Order edge is TestOnly — the standard sbt layout keeps main and test
	// packages in separate directories, hence separate graph nodes.
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/main/scala/com/example/service")
	if imp := findImport(service, "com.example.domain.Order"); imp == nil || imp.TestOnly {
		t.Errorf("production service->domain.Order should be a non-test edge, got %+v", imp)
	}

	testNode := findPkg(g, "src/test/scala/com/example/service")
	imp := findImport(testNode, "com.example.domain.Order")
	if imp == nil {
		t.Fatal("test node -> domain.Order edge missing")
	}
	if !imp.TestOnly {
		t.Errorf("test node -> domain.Order should be TestOnly, got %+v", imp)
	}
	if imp.Class != core.ClassInModule || imp.RelDir != "src/main/scala/com/example/domain" {
		t.Errorf("test edge should resolve to the main domain dir, got %+v", imp)
	}
}

func TestLoadIsDeterministic(t *testing.T) {
	root := buildProject(t)
	var prev *core.Graph
	for i := 0; i < 5; i++ {
		g, err := (&Loader{Dir: root}).Load(context.Background())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !sort.SliceIsSorted(g.Packages, func(i, j int) bool {
			return g.Packages[i].ImportPath < g.Packages[j].ImportPath
		}) {
			t.Fatalf("packages not sorted by ImportPath: %v", relDirs(g))
		}
		for _, p := range g.Packages {
			if !sort.SliceIsSorted(p.Imports, func(i, j int) bool {
				return p.Imports[i].Path < p.Imports[j].Path
			}) {
				t.Fatalf("imports of %s not sorted by Path", p.ImportPath)
			}
		}
		if prev != nil && !reflect.DeepEqual(prev, g) {
			t.Fatalf("Load is non-deterministic across runs")
		}
		prev = g
	}
}

func TestLoadRenamedInModule(t *testing.T) {
	// An `import ... {X => Alias}` resolves to the same in-module package as the
	// unaliased form; the alias never contaminates the specifier or the edge.
	root := setupProject(t, map[string]string{
		"build.sbt": "name := \"renamed\"\n",
		"src/main/scala/com/example/domain/Order.scala": "package com.example.domain\nclass Order\n",
		"src/main/scala/com/example/service/Svc.scala":  "package com.example.service\nimport com.example.domain.{Order => DomainOrder}\nclass Svc\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/main/scala/com/example/service")
	imp := findImport(service, "com.example.domain.Order")
	if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/main/scala/com/example/domain" {
		t.Errorf("service->domain.Order (renamed) should be in-module, got %+v", imp)
	}
}

func TestLoadWildcardInModule(t *testing.T) {
	// Both the Scala 2 `._` and Scala 3 `.*` wildcards resolve to the imported
	// package.
	for _, wildcard := range []string{"._", ".*"} {
		root := setupProject(t, map[string]string{
			"build.sbt": "name := \"wild\"\n",
			"src/main/scala/com/example/domain/Order.scala": "package com.example.domain\nclass Order\n",
			"src/main/scala/com/example/service/Svc.scala":  "package com.example.service\nimport com.example.domain" + wildcard + "\nclass Svc\n",
		})
		g, err := (&Loader{Dir: root}).Load(context.Background())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		service := findPkg(g, "src/main/scala/com/example/service")
		imp := findImport(service, "com.example.domain"+wildcard)
		if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/main/scala/com/example/domain" {
			t.Errorf("service->domain%s wildcard should be in-module, got %+v", wildcard, imp)
		}
	}
}

func TestLoadGivenInModule(t *testing.T) {
	// A Scala 3 `import a.b.given` resolves to the imported package.
	root := setupProject(t, map[string]string{
		"build.sbt": "name := \"givens\"\n",
		"src/main/scala/com/example/domain/Ords.scala": "package com.example.domain\nobject Ords\n",
		"src/main/scala/com/example/service/Svc.scala": "package com.example.service\nimport com.example.domain.given\nclass Svc\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/main/scala/com/example/service")
	imp := findImport(service, "com.example.domain.given")
	if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/main/scala/com/example/domain" {
		t.Errorf("service->domain.given should be in-module, got %+v", imp)
	}
}

func TestLoadSelectorGroupInModule(t *testing.T) {
	// A selector group `{A, B}` produces one in-module edge per member.
	root := setupProject(t, map[string]string{
		"build.sbt": "name := \"selectors\"\n",
		"src/main/scala/com/example/domain/Types.scala": "package com.example.domain\nclass Order\nclass Line\n",
		"src/main/scala/com/example/service/Svc.scala":  "package com.example.service\nimport com.example.domain.{Order, Line}\nclass Svc\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/main/scala/com/example/service")
	for _, sym := range []string{"com.example.domain.Order", "com.example.domain.Line"} {
		imp := findImport(service, sym)
		if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/main/scala/com/example/domain" {
			t.Errorf("service->%s should be in-module, got %+v", sym, imp)
		}
	}
}

func TestLoadDropsSelfImport(t *testing.T) {
	// A wildcard import of a class's own package is a self-edge; it must not appear
	// as an edge.
	root := setupProject(t, map[string]string{
		"build.sbt": "name := \"selfimp\"\n",
		"src/main/scala/com/example/domain/A.scala": "package com.example.domain\nclass A\n",
		"src/main/scala/com/example/domain/B.scala": "package com.example.domain\nimport com.example.domain._\nclass B\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	domain := findPkg(g, "src/main/scala/com/example/domain")
	if imp := findImport(domain, "com.example.domain._"); imp != nil {
		t.Errorf("self-edge domain->domain._ should be dropped, got %+v", imp)
	}
}

func TestLoadSkipsBuildAndDotDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"build.sbt": "name := \"skips\"\n",
		"src/main/scala/com/example/app/App.scala": "package com.example.app\nimport scala.collection.mutable.Map\nclass App\n",
		"target/scala-3.3.1/Junk.scala":            "package junk\nimport bad.Thing\nclass Junk\n",
		"project/Build.scala":                      "package junk\nimport bad.Thing\nobject Build\n",
		".metals/Cached.scala":                     "package junk\nimport bad.Thing\nclass Cached\n",
		".git/hooks/Hook.scala":                    "package junk\nimport bad.Thing\nclass Hook\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range g.Packages {
		for _, skip := range []string{"target", "project", ".metals", ".git"} {
			if strings.Contains(p.RelDir, skip) {
				t.Errorf("node %q should have been skipped (%s)", p.RelDir, skip)
			}
		}
	}
}

func TestLoadPatternScoping(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background(), "src/main/scala/com/example/domain")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(g.Packages) != 1 || g.Packages[0].RelDir != "src/main/scala/com/example/domain" {
		t.Errorf("pattern-scoped load = %v, want only the domain node", relDirs(g))
	}
}

func TestLoadMissingRoot(t *testing.T) {
	dir := t.TempDir()
	_, err := (&Loader{Dir: dir}).Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for a project with no build marker")
	}
	if !strings.Contains(err.Error(), "build.sbt") {
		t.Errorf("error %q should mention build.sbt", err.Error())
	}
}

func TestProjectNameSources(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "sbt name setting",
			files: map[string]string{"build.sbt": "name := \"sbtapp\"\nscalaVersion := \"3.3.1\"\n"},
			want:  "sbtapp",
		},
		{
			name:  "sbt name ignores commented line",
			files: map[string]string{"build.sbt": "// name := \"commented\"\nname := \"real\"\n"},
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
	// A Mill build.sc (no simple assignable name) falls back to the root dir
	// basename.
	root := setupProject(t, map[string]string{"build.sc": "import mill._\n"})
	got := projectName(root)
	if got == "" || strings.Contains(got, "/") {
		t.Errorf("projectName fallback = %q, want the root basename", got)
	}
}

func TestLoadMillMarker(t *testing.T) {
	// A Mill project rooted by build.sc is discovered and scanned.
	root := setupProject(t, map[string]string{
		"build.sc":                           "import mill._\nimport mill.scalalib._\n",
		"src/com/example/domain/Order.scala": "package com.example.domain\nclass Order\n",
		"src/com/example/service/Svc.scala":  "package com.example.service\nimport com.example.domain.Order\nclass Svc\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/com/example/service")
	imp := findImport(service, "com.example.domain.Order")
	if imp == nil || imp.Class != core.ClassInModule {
		t.Errorf("Mill service->domain.Order should be in-module, got %+v", imp)
	}
}
