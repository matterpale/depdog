package elm

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

// interface assertion mirrors the other adapters.
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

// appElmJSON is a minimal application elm.json with the default source dir.
const appElmJSON = `{
    "type": "application",
    "source-directories": ["src"],
    "elm-version": "0.19.1",
    "dependencies": {"direct": {}, "indirect": {}},
    "test-dependencies": {"direct": {}, "indirect": {}}
}
`

// buildProject lays down a layered Elm application and returns its root.
func buildProject(t *testing.T) string {
	t.Helper()
	return setupProject(t, map[string]string{
		"elm.json": appElmJSON,
		"src/Domain/Order.elm": `module Domain.Order exposing (Order)

import List

type alias Order = { total : Int }
`,
		"src/Service/Orders.elm": `module Service.Orders exposing (place)

import Domain.Order exposing (Order)
import Dict

place : Int -> Order
place total = { total = total }
`,
		"src/Handler/Api.elm": `module Handler.Api exposing (handle)

import Service.Orders
import Json.Decode

handle : Int -> Int
handle n = n
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

	if g.ModulePath != filepath.Base(root) {
		t.Errorf("ModulePath = %q, want %q (root basename for an application)", g.ModulePath, filepath.Base(root))
	}

	// Domain imports List -> std.
	domain := findPkg(g, "src/Domain")
	if imp := findImport(domain, "List"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("domain->List should be std, got %+v", imp)
	}

	// Service imports Domain.Order (in-module -> src/Domain) and Dict (std).
	service := findPkg(g, "src/Service")
	if imp := findImport(service, "Domain.Order"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/Domain" {
		t.Errorf("service->Domain.Order should be in-module src/Domain, got %+v", imp)
	}
	if imp := findImport(service, "Dict"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("service->Dict should be std, got %+v", imp)
	}

	// Handler imports Service.Orders (in-module -> src/Service) and Json.Decode
	// (external: elm/json, not elm/core).
	handler := findPkg(g, "src/Handler")
	if imp := findImport(handler, "Service.Orders"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/Service" {
		t.Errorf("handler->Service.Orders should be in-module src/Service, got %+v", imp)
	}
	if imp := findImport(handler, "Json.Decode"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("handler->Json.Decode should be external, got %+v", imp)
	}
}

func TestLoadImportPositions(t *testing.T) {
	// The recorded position points at the import statement's line and file.
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/Service")
	imp := findImport(service, "Domain.Order")
	if imp == nil || len(imp.Positions) != 1 {
		t.Fatalf("service->Domain.Order positions = %+v", imp)
	}
	if imp.Positions[0].File != "src/Service/Orders.elm" || imp.Positions[0].Line != 3 {
		t.Errorf("position = %+v, want src/Service/Orders.elm:3", imp.Positions[0])
	}
}

func TestLoadNonDefaultSourceDirectory(t *testing.T) {
	// A source-directories value other than the default ["src"] must be honored:
	// modules live under "app/", and resolution uses that prefix.
	root := setupProject(t, map[string]string{
		"elm.json": `{
    "type": "application",
    "source-directories": ["app"],
    "elm-version": "0.19.1"
}
`,
		"app/Domain/Order.elm":   "module Domain.Order exposing (..)\nimport List\n",
		"app/Service/Orders.elm": "module Service.Orders exposing (..)\nimport Domain.Order\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "app/Service")
	imp := findImport(service, "Domain.Order")
	if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "app/Domain" {
		t.Errorf("service->Domain.Order should be in-module app/Domain, got %+v", imp)
	}
}

func TestLoadMultipleSourceDirectories(t *testing.T) {
	// A module resolves as in-module when it lives under ANY of several
	// source-directories.
	root := setupProject(t, map[string]string{
		"elm.json": `{
    "type": "application",
    "source-directories": ["src", "vendored"],
    "elm-version": "0.19.1"
}
`,
		"src/App.elm":              "module App exposing (..)\nimport Shared.Util\n",
		"vendored/Shared/Util.elm": "module Shared.Util exposing (..)\nimport List\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	app := findPkg(g, "src")
	imp := findImport(app, "Shared.Util")
	if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "vendored/Shared" {
		t.Errorf("App->Shared.Util should be in-module vendored/Shared, got %+v", imp)
	}
}

func TestLoadPackageNameLabel(t *testing.T) {
	// A package elm.json carries a `name` (author/project) used as the ModulePath,
	// and always builds from src/.
	root := setupProject(t, map[string]string{
		"elm.json": `{
    "type": "package",
    "name": "acme/widgets",
    "summary": "widgets",
    "license": "BSD-3-Clause",
    "version": "1.0.0",
    "exposed-modules": ["Widget"],
    "elm-version": "0.19.0 <= v < 0.20.0",
    "dependencies": {},
    "test-dependencies": {}
}
`,
		"src/Widget.elm":          "module Widget exposing (..)\nimport Widget.Internal\nimport List\n",
		"src/Widget/Internal.elm": "module Widget.Internal exposing (..)\nimport Dict\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.ModulePath != "acme/widgets" {
		t.Errorf("ModulePath = %q, want acme/widgets", g.ModulePath)
	}
	widget := findPkg(g, "src")
	imp := findImport(widget, "Widget.Internal")
	if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/Widget" {
		t.Errorf("Widget->Widget.Internal should be in-module src/Widget, got %+v", imp)
	}
}

func TestLoadDropsSelfImport(t *testing.T) {
	// A module importing a sibling in its own directory node is NOT dropped; but an
	// import resolving to the importing file's own node dir IS a self-edge.
	root := setupProject(t, map[string]string{
		"elm.json":         appElmJSON,
		"src/Domain/A.elm": "module Domain.A exposing (..)\ntype A = A\n",
		"src/Domain/B.elm": "module Domain.B exposing (..)\nimport Domain.A\ntype B = B\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	domain := findPkg(g, "src/Domain")
	// Domain.B imports Domain.A; both live in src/Domain, so this is a self-edge
	// (same node dir) and must be dropped.
	if imp := findImport(domain, "Domain.A"); imp != nil {
		t.Errorf("self-edge Domain.B->Domain.A (same dir) should be dropped, got %+v", imp)
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

func TestLoadSkipsBuildAndDotDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"elm.json":               appElmJSON,
		"src/App.elm":            "module App exposing (..)\nimport List\n",
		"src/elm-stuff/Junk.elm": "module Junk exposing (..)\nimport Bad.Thing\n",
		"src/.hidden/Cached.elm": "module Cached exposing (..)\nimport Bad.Thing\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range g.Packages {
		for _, skip := range []string{"elm-stuff", ".hidden"} {
			if strings.Contains(p.RelDir, skip) {
				t.Errorf("node %q should have been skipped (%s)", p.RelDir, skip)
			}
		}
	}
}

func TestLoadPatternScoping(t *testing.T) {
	root := buildProject(t)
	g, err := (&Loader{Dir: root}).Load(context.Background(), "src/Domain")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(g.Packages) != 1 || g.Packages[0].RelDir != "src/Domain" {
		t.Errorf("pattern-scoped load = %v, want only the Domain node", relDirs(g))
	}
}

func TestLoadMissingRoot(t *testing.T) {
	dir := t.TempDir()
	_, err := (&Loader{Dir: dir}).Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for a project with no elm.json")
	}
	if !strings.Contains(err.Error(), "elm.json") {
		t.Errorf("error %q should mention elm.json", err.Error())
	}
}

func TestLoadInvalidElmJSON(t *testing.T) {
	// A malformed elm.json is an error (not a silent guess): the marker is present
	// but the project is broken.
	root := setupProject(t, map[string]string{
		"elm.json":    "{ not valid json",
		"src/App.elm": "module App exposing (..)\nimport List\n",
	})
	_, err := (&Loader{Dir: root}).Load(context.Background())
	if err == nil {
		t.Fatal("expected an error for an invalid elm.json")
	}
	if !strings.Contains(err.Error(), "elm.json") {
		t.Errorf("error %q should mention elm.json", err.Error())
	}
}

func TestLoadDefaultSourceDirWhenAbsent(t *testing.T) {
	// An application elm.json without an explicit source-directories defaults to
	// ["src"].
	root := setupProject(t, map[string]string{
		"elm.json":               `{"type": "application", "elm-version": "0.19.1"}`,
		"src/Domain/Order.elm":   "module Domain.Order exposing (..)\nimport List\n",
		"src/Service/Orders.elm": "module Service.Orders exposing (..)\nimport Domain.Order\n",
	})
	g, err := (&Loader{Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	service := findPkg(g, "src/Service")
	imp := findImport(service, "Domain.Order")
	if imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "src/Domain" {
		t.Errorf("service->Domain.Order should be in-module src/Domain, got %+v", imp)
	}
}
