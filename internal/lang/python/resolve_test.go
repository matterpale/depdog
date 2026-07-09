package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

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

// setupProject writes a small on-disk tree and returns its root.
func setupProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}
	return root
}

func TestClassifyStd(t *testing.T) {
	root := t.TempDir()
	for _, mod := range []string{"os", "sys", "json", "typing", "collections", "asyncio", "pathlib", "os.path", "collections.abc"} {
		class, relDir, _, ok := classify(importRef{Module: mod}, root, root)
		if !ok || class != core.ClassStd || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want std", mod, class, relDir, ok)
		}
	}
}

func TestClassifyExternal(t *testing.T) {
	root := t.TempDir()
	for _, mod := range []string{"requests", "numpy", "django.db", "flask", "pydantic.v1"} {
		class, relDir, _, ok := classify(importRef{Module: mod}, root, root)
		if !ok || class != core.ClassExternal || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want external", mod, class, relDir, ok)
		}
	}
}

func TestClassifyInModuleAbsolute(t *testing.T) {
	root := setupProject(t, map[string]string{
		"domain/__init__.py":  "",
		"domain/order.py":     "",
		"service/__init__.py": "",
	})
	tests := []struct {
		name    string
		module  string
		wantDir string
	}{
		{"package by init", "domain", "domain"},
		{"module file", "domain.order", "domain"},
		{"another package", "service", "service"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, relDir, display, ok := classify(importRef{Module: tc.module}, filepath.Join(root, "service"), root)
			if !ok || class != core.ClassInModule {
				t.Fatalf("classify(%q): class=%v ok=%v, want in-module", tc.module, class, ok)
			}
			if relDir != tc.wantDir {
				t.Errorf("classify(%q) relDir = %q, want %q", tc.module, relDir, tc.wantDir)
			}
			if display != tc.module {
				t.Errorf("classify(%q) display = %q, want %q", tc.module, display, tc.module)
			}
		})
	}
}

func TestClassifyInModuleShadowsStdlib(t *testing.T) {
	// A first-party package that happens to share a stdlib name resolves to
	// in-module (it exists on disk under root), not std.
	root := setupProject(t, map[string]string{
		"json/__init__.py": "",
	})
	class, relDir, _, ok := classify(importRef{Module: "json"}, root, root)
	if !ok || class != core.ClassInModule || relDir != "json" {
		t.Errorf("first-party json: class=%v relDir=%q, want in-module json", class, relDir)
	}
}

func TestClassifyRelative(t *testing.T) {
	root := setupProject(t, map[string]string{
		"pkg/__init__.py":       "",
		"pkg/sub/__init__.py":   "",
		"pkg/sub/mod.py":        "",
		"pkg/other/__init__.py": "",
	})
	subDir := filepath.Join(root, "pkg", "sub")

	tests := []struct {
		name       string
		ref        importRef
		wantClass  core.Class
		wantRelDir string
		wantDisp   string
	}{
		{"from . import (current pkg)", importRef{Level: 1}, core.ClassInModule, "pkg/sub", "."},
		{"from .mod import x", importRef{Level: 1, Module: "mod"}, core.ClassInModule, "pkg/sub", ".mod"},
		{"from .. import y (parent pkg)", importRef{Level: 2}, core.ClassInModule, "pkg", ".."},
		{"from ..other import z", importRef{Level: 2, Module: "other"}, core.ClassInModule, "pkg/other", "..other"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, relDir, display, ok := classify(tc.ref, subDir, root)
			if !ok || class != tc.wantClass || relDir != tc.wantRelDir {
				t.Errorf("classify(%+v): class=%v relDir=%q, want %v %q", tc.ref, class, relDir, tc.wantClass, tc.wantRelDir)
			}
			if display != tc.wantDisp {
				t.Errorf("classify(%+v) display = %q, want %q", tc.ref, display, tc.wantDisp)
			}
		})
	}
}

func TestClassifyRelativeUnresolvedIsExternal(t *testing.T) {
	// A relative import that resolves to nothing on disk degrades to external
	// rather than fabricating an in-module edge.
	root := setupProject(t, map[string]string{
		"pkg/__init__.py": "",
	})
	class, relDir, _, ok := classify(importRef{Level: 1, Module: "missing"}, filepath.Join(root, "pkg"), root)
	if !ok || class != core.ClassExternal || relDir != "" {
		t.Errorf("unresolved relative: class=%v relDir=%q, want external", class, relDir)
	}
}

func TestClassifyNamespacePackage(t *testing.T) {
	// A directory holding .py files but no __init__.py is still a resolvable
	// (namespace-style) package.
	root := setupProject(t, map[string]string{
		"ns/thing.py": "",
	})
	class, relDir, _, ok := classify(importRef{Module: "ns"}, root, root)
	if !ok || class != core.ClassInModule || relDir != "ns" {
		t.Errorf("namespace package: class=%v relDir=%q, want in-module ns", class, relDir)
	}
}

func TestResolvePackageDirNoCrash(t *testing.T) {
	root := t.TempDir()
	if _, ok := resolvePackageDir(filepath.Join(root, "nope", "still-nope")); ok {
		t.Errorf("resolvePackageDir of missing path returned ok")
	}
	// An empty directory (no __init__ and no .py) does not resolve.
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolvePackageDir(filepath.Join(root, "empty")); ok {
		t.Errorf("resolvePackageDir of empty dir returned ok")
	}
}
