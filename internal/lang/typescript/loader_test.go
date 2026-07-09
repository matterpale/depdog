package typescript

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// interface assertion mirrors the golang adapter.
var _ lang.Loader = (*Loader)(nil)

// buildProject lays down a layered TS project under t.TempDir and returns root.
func buildProject(t *testing.T) string {
	t.Helper()
	return setupProject(t, map[string]string{
		"package.json": `{ "name": "example-ts" }`,
		"tsconfig.json": `{
  "compilerOptions": {
    "baseUrl": "./src",
    "paths": { "@app/*": ["*"] }
  }
}`,
		// domain: std only
		"src/domain/order.ts": `import * as crypto from 'node:crypto';
export const id = () => crypto.randomUUID();`,
		// service: imports domain (relative) + a node builtin
		"src/service/svc.ts": `import { id } from '../domain/order';
import * as fs from 'fs';
export const use = () => id() + String(fs);`,
		// api: imports service (via alias) + a bare external
		"src/api/handler.ts": `import { use } from '@app/service/svc';
import express from 'express';
export const app = express();`,
		// a test-only edge: only the test file imports lodash
		"src/api/handler.test.ts": `import { app } from './handler';
import _ from 'lodash';
test('app', () => { expect(app).toBeDefined(); void _; });`,
	})
}

func TestLoadBuildsGraph(t *testing.T) {
	root := buildProject(t)
	l := &Loader{Dir: root}
	g, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if g.ModulePath != "example-ts" {
		t.Errorf("ModulePath = %q, want example-ts", g.ModulePath)
	}

	byRelDir := make(map[string]core.Package)
	for _, p := range g.Packages {
		byRelDir[p.RelDir] = p
	}
	for _, want := range []string{"src/domain", "src/service", "src/api"} {
		if _, ok := byRelDir[want]; !ok {
			t.Errorf("node %q missing (have %d nodes: %v)", want, len(g.Packages), relDirs(g))
		}
	}

	// ImportPath = <ModulePath>/<RelDir>.
	if api := byRelDir["src/api"]; api.ImportPath != "example-ts/src/api" {
		t.Errorf("api ImportPath = %q, want example-ts/src/api", api.ImportPath)
	}

	find := func(relDir, path string) (core.Import, bool) {
		for _, i := range byRelDir[relDir].Imports {
			if i.Path == path {
				return i, true
			}
		}
		return core.Import{}, false
	}

	// domain -> node:crypto is std.
	if i, ok := find("src/domain", "node:crypto"); !ok || i.Class != core.ClassStd {
		t.Errorf("domain->node:crypto: %+v ok=%v, want std", i, ok)
	}

	// service -> ../domain/order is in-module, target relDir src/domain.
	if i, ok := find("src/service", "../domain/order"); !ok || i.Class != core.ClassInModule || i.RelDir != "src/domain" {
		t.Errorf("service->domain: %+v ok=%v, want in-module src/domain", i, ok)
	}
	// service -> fs is std.
	if i, ok := find("src/service", "fs"); !ok || i.Class != core.ClassStd {
		t.Errorf("service->fs: %+v ok=%v, want std", i, ok)
	}

	// api -> @app/service/svc alias resolves in-module to src/service.
	if i, ok := find("src/api", "@app/service/svc"); !ok || i.Class != core.ClassInModule || i.RelDir != "src/service" {
		t.Errorf("api->@app/service/svc: %+v ok=%v, want in-module src/service", i, ok)
	}
	// api -> express is external.
	if i, ok := find("src/api", "express"); !ok || i.Class != core.ClassExternal {
		t.Errorf("api->express: %+v ok=%v, want external", i, ok)
	}
	// api's production import of express must not be marked test-only.
	if i, _ := find("src/api", "express"); i.TestOnly {
		t.Errorf("api->express marked TestOnly, want production")
	}

	// api -> lodash appears only in handler.test.ts => TestOnly.
	if i, ok := find("src/api", "lodash"); !ok || i.Class != core.ClassExternal || !i.TestOnly {
		t.Errorf("api->lodash: %+v ok=%v, want external test-only", i, ok)
	}

	// Position files are module-relative.
	if i, _ := find("src/service", "../domain/order"); len(i.Positions) == 0 ||
		i.Positions[0].File != "src/service/svc.ts" || i.Positions[0].Line != 1 {
		t.Errorf("service->domain positions = %+v", i.Positions)
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
		// Packages sorted by ImportPath.
		if !sort.SliceIsSorted(g.Packages, func(i, j int) bool {
			return g.Packages[i].ImportPath < g.Packages[j].ImportPath
		}) {
			t.Errorf("packages not sorted by ImportPath: %v", importPaths(g))
		}
		// Imports sorted by Path within each package.
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

func TestLoadMissingRoot(t *testing.T) {
	// A directory with neither tsconfig.json nor package.json anywhere above.
	dir := t.TempDir()
	l := &Loader{Dir: dir}
	_, err := l.Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for a project with no tsconfig/package.json")
	}
	// Actionable message names both markers and the search directory.
	msg := err.Error()
	for _, want := range []string{"tsconfig.json", "package.json"} {
		if !contains(msg, want) {
			t.Errorf("error %q should mention %q", msg, want)
		}
	}
}

func TestLoadSkipsNodeModulesAndDotDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"package.json":              `{ "name": "skips" }`,
		"src/app.ts":                `import x from './lib';`,
		"src/lib.ts":                ``,
		"node_modules/pkg/index.ts": `import bad from './nope';`,
		".hidden/secret.ts":         `import bad from './nope';`,
	})
	l := &Loader{Dir: root}
	g, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range g.Packages {
		if contains(p.RelDir, "node_modules") || contains(p.RelDir, ".hidden") {
			t.Errorf("node %q should have been skipped", p.RelDir)
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

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func TestLoadRelDirRoot(t *testing.T) {
	root := setupProject(t, map[string]string{
		"package.json": `{ "name": "rooted" }`,
		"index.ts":     `import fs from 'fs';`,
	})
	l := &Loader{Dir: root}
	g, err := l.Load(context.Background())
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

func TestLoadWithPatterns(t *testing.T) {
	root := setupProject(t, map[string]string{
		"package.json": `{ "name": "patterned" }`,
		"src/a/x.ts":   `import fs from 'fs';`,
		"src/b/y.ts":   `import path from 'path';`,
	})
	l := &Loader{Dir: root}
	g, err := l.Load(context.Background(), filepath.Join("src", "a"))
	if err != nil {
		t.Fatalf("Load with pattern: %v", err)
	}
	for _, p := range g.Packages {
		if contains(p.RelDir, "src/b") {
			t.Errorf("pattern src/a should not include %q", p.RelDir)
		}
	}
	if len(g.Packages) == 0 {
		t.Errorf("pattern src/a produced no nodes")
	}
}
