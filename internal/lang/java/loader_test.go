package java

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

// interface assertion mirrors the golang/typescript/python/rust adapters.
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

// buildProject lays down a layered Java project and returns its root.
func buildProject(t *testing.T) string {
	t.Helper()
	return setupProject(t, map[string]string{
		"pom.xml": "<project><artifactId>example-java</artifactId></project>\n",
		"src/main/java/com/example/domain/Order.java": `package com.example.domain;

import java.util.concurrent.atomic.AtomicLong;

public final class Order {}
`,
		"src/main/java/com/example/service/OrderService.java": `package com.example.service;

import java.util.Map;

import com.example.domain.Order;

public final class OrderService {}
`,
		"src/main/java/com/example/handler/OrderHandler.java": `package com.example.handler;

import java.io.IOException;

import com.google.gson.Gson;

import com.example.service.OrderService;

public final class OrderHandler {}
`,
		"src/test/java/com/example/service/OrderServiceTest.java": `package com.example.service;

import com.example.domain.Order;

public final class OrderServiceTest {}
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

	if g.ModulePath != "example-java" {
		t.Errorf("ModulePath = %q, want example-java", g.ModulePath)
	}

	domainDir := "src/main/java/com/example/domain"
	serviceDir := "src/main/java/com/example/service"

	domain := findPkg(g, domainDir)
	if imp := findImport(domain, "java.util.concurrent.atomic.AtomicLong"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("domain->AtomicLong should be std, got %+v", imp)
	}

	service := findPkg(g, serviceDir)
	if imp := findImport(service, "com.example.domain.Order"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != domainDir {
		t.Errorf("service->domain.Order should be in-module %s, got %+v", domainDir, imp)
	}
	if imp := findImport(service, "java.util.Map"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("service->java.util.Map should be std, got %+v", imp)
	}

	handler := findPkg(g, "src/main/java/com/example/handler")
	if imp := findImport(handler, "com.google.gson.Gson"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("handler->gson.Gson should be external, got %+v", imp)
	}
	if imp := findImport(handler, "com.example.service.OrderService"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != serviceDir {
		t.Errorf("handler->service.OrderService should be in-module %s, got %+v", serviceDir, imp)
	}
}

func TestLoadTestOnlyEdge(t *testing.T) {
	// The production service source's import of domain.Order is a production
	// edge. The test source lives under src/test/... so it is its own node whose
	// domain.Order edge is TestOnly — the standard Maven layout keeps main and
	// test packages in separate directories, hence separate graph nodes.
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/main/java/com/example/service")
	if imp := findImport(service, "com.example.domain.Order"); imp == nil || imp.TestOnly {
		t.Errorf("production service->domain.Order should be a non-test edge, got %+v", imp)
	}

	testNode := findPkg(g, "src/test/java/com/example/service")
	imp := findImport(testNode, "com.example.domain.Order")
	if imp == nil {
		t.Fatal("test node -> domain.Order edge missing")
	}
	if !imp.TestOnly {
		t.Errorf("test node -> domain.Order should be TestOnly, got %+v", imp)
	}
	if imp.Class != core.ClassInModule || imp.RelDir != "src/main/java/com/example/domain" {
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

func TestLoadWildcardInModule(t *testing.T) {
	root := setupProject(t, map[string]string{
		"pom.xml": "<project><artifactId>wc</artifactId></project>\n",
		"src/main/java/com/example/domain/Order.java": "package com.example.domain;\npublic class Order {}\n",
		"src/main/java/com/example/service/Svc.java":  "package com.example.service;\nimport com.example.domain.*;\npublic class Svc {}\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/main/java/com/example/service")
	imp := findImport(service, "com.example.domain.*")
	if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/main/java/com/example/domain" {
		t.Errorf("service->domain.* wildcard should be in-module, got %+v", imp)
	}
}

func TestLoadDropsSelfImport(t *testing.T) {
	// A wildcard import of a class's own package is a self-edge; it must not
	// appear as an edge.
	root := setupProject(t, map[string]string{
		"pom.xml": "<project><artifactId>self</artifactId></project>\n",
		"src/main/java/com/example/domain/A.java": "package com.example.domain;\npublic class A {}\n",
		"src/main/java/com/example/domain/B.java": "package com.example.domain;\nimport com.example.domain.*;\npublic class B {}\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	domain := findPkg(g, "src/main/java/com/example/domain")
	if imp := findImport(domain, "com.example.domain.*"); imp != nil {
		t.Errorf("self-edge domain->domain.* should be dropped, got %+v", imp)
	}
}

func TestLoadSkipsBuildAndDotDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"pom.xml":                                "<project><artifactId>skips</artifactId></project>\n",
		"src/main/java/com/example/app/App.java": "package com.example.app;\nimport java.util.List;\npublic class App {}\n",
		"target/generated/Junk.java":             "package junk;\nimport bad.Thing;\npublic class Junk {}\n",
		"build/classes/Compiled.java":            "package junk;\nimport bad.Thing;\npublic class Compiled {}\n",
		".git/hooks/Hook.java":                   "package junk;\nimport bad.Thing;\npublic class Hook {}\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range g.Packages {
		for _, skip := range []string{"target", "build", ".git"} {
			if strings.Contains(p.RelDir, skip) {
				t.Errorf("node %q should have been skipped (%s)", p.RelDir, skip)
			}
		}
	}
}

func TestLoadPatternScoping(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background(), "src/main/java/com/example/domain")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(g.Packages) != 1 || g.Packages[0].RelDir != "src/main/java/com/example/domain" {
		t.Errorf("pattern-scoped load = %v, want only the domain node", relDirs(g))
	}
}

func TestLoadMissingRoot(t *testing.T) {
	dir := t.TempDir()
	_, err := (&Loader{Dir: dir}).Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for a project with no build marker")
	}
	if !strings.Contains(err.Error(), "pom.xml") {
		t.Errorf("error %q should mention pom.xml", err.Error())
	}
}

func TestProjectNameSources(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "maven artifactId",
			files: map[string]string{"pom.xml": "<project><groupId>g</groupId><artifactId>myapp</artifactId></project>"},
			want:  "myapp",
		},
		{
			name: "maven artifactId ignores nested dependency/parent",
			files: map[string]string{"pom.xml": `<project>
  <parent><artifactId>a-parent</artifactId></parent>
  <artifactId>real</artifactId>
  <dependencies><dependency><artifactId>dep</artifactId></dependency></dependencies>
</project>`},
			want: "real",
		},
		{
			name:  "maven artifactId ignores leading comment",
			files: map[string]string{"pom.xml": "<!-- top comment with > inside? no -->\n<project><artifactId>commented</artifactId></project>"},
			want:  "commented",
		},
		{
			name:  "gradle rootProject.name",
			files: map[string]string{"build.gradle": "plugins { id 'java' }\n", "settings.gradle": "rootProject.name = 'gradleapp'\n"},
			want:  "gradleapp",
		},
		{
			name:  "gradle kts rootProject.name",
			files: map[string]string{"build.gradle.kts": "plugins { java }\n", "settings.gradle.kts": "rootProject.name = \"ktsapp\"\n"},
			want:  "ktsapp",
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
	// A pom with no artifactId falls back to the root dir basename.
	root := setupProject(t, map[string]string{"pom.xml": "<project><groupId>g</groupId></project>"})
	got := projectName(root)
	if got == "" || strings.Contains(got, "/") {
		t.Errorf("projectName fallback = %q, want the root basename", got)
	}
}
