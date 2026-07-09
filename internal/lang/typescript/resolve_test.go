package typescript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

// setupProject writes a small on-disk tree and returns its root.
func setupProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}
	return root
}

func TestClassifyRelative(t *testing.T) {
	root := setupProject(t, map[string]string{
		"src/service/svc.ts":  "",
		"src/domain/order.ts": "",
		"src/domain/index.ts": "",
		"src/util/helper.tsx": "",
	})
	svcDir := filepath.Join(root, "src", "service")

	tests := []struct {
		name       string
		spec       string
		wantClass  core.Class
		wantRelDir string
		wantOK     bool
	}{
		{"file with implied ext", "../domain/order", core.ClassInModule, "src/domain", true},
		{"dir index resolution", "../domain", core.ClassInModule, "src/domain", true},
		{"tsx extension", "../util/helper", core.ClassInModule, "src/util", true},
		{"unresolved relative falls back external", "../does/not/exist", core.ClassExternal, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &tsconfig{}
			class, relDir, ok := classify(tt.spec, svcDir, root, cfg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if class != tt.wantClass {
				t.Errorf("class = %v, want %v", class, tt.wantClass)
			}
			if relDir != tt.wantRelDir {
				t.Errorf("relDir = %q, want %q", relDir, tt.wantRelDir)
			}
		})
	}
}

func TestClassifyExtensionPrecedence(t *testing.T) {
	// Both a.ts and a.js exist; .ts should win per the extension order.
	root := setupProject(t, map[string]string{
		"src/a.ts": "",
		"src/a.js": "",
	})
	fromDir := filepath.Join(root, "src")
	class, relDir, ok := classify("./a", fromDir, root, &tsconfig{})
	if !ok || class != core.ClassInModule {
		t.Fatalf("classify ./a: class=%v ok=%v", class, ok)
	}
	if relDir != "src" {
		t.Errorf("relDir = %q, want src", relDir)
	}
}

func TestClassifyRootRelDir(t *testing.T) {
	root := setupProject(t, map[string]string{
		"index.ts": "",
		"app.ts":   "",
	})
	class, relDir, ok := classify("./index", root, root, &tsconfig{})
	if !ok || class != core.ClassInModule {
		t.Fatalf("class=%v ok=%v", class, ok)
	}
	if relDir != "." {
		t.Errorf("root relDir = %q, want .", relDir)
	}
}

func TestClassifyAlias(t *testing.T) {
	root := setupProject(t, map[string]string{
		"tsconfig.json":       "",
		"src/lib/thing.ts":    "",
		"src/lib/index.ts":    "",
		"src/domain/order.ts": "",
	})
	cfg := &tsconfig{
		BaseURL: "./src",
		Paths: map[string][]string{
			"@app/*": {"*"},
			"@lib":   {"lib/index.ts"},
		},
	}
	fromDir := filepath.Join(root, "src", "domain")

	// @app/lib/thing -> src/lib/thing.ts (in-module)
	class, relDir, ok := classify("@app/lib/thing", fromDir, root, cfg)
	if !ok || class != core.ClassInModule || relDir != "src/lib" {
		t.Errorf("@app/lib/thing: class=%v relDir=%q ok=%v", class, relDir, ok)
	}

	// bare alias @lib -> src/lib/index.ts (in-module, exact non-wildcard)
	class, relDir, ok = classify("@lib", fromDir, root, cfg)
	if !ok || class != core.ClassInModule || relDir != "src/lib" {
		t.Errorf("@lib: class=%v relDir=%q ok=%v", class, relDir, ok)
	}
}

func TestClassifyUnresolvedAliasFallsBackExternal(t *testing.T) {
	root := setupProject(t, map[string]string{
		"src/domain/order.ts": "",
	})
	cfg := &tsconfig{
		BaseURL: "./src",
		Paths:   map[string][]string{"@app/*": {"*"}},
	}
	fromDir := filepath.Join(root, "src", "domain")
	// @app/missing does not exist on disk -> degrade to external, never crash.
	class, relDir, ok := classify("@app/missing/mod", fromDir, root, cfg)
	if !ok {
		t.Fatalf("ok = false, want graceful external")
	}
	if class != core.ClassExternal || relDir != "" {
		t.Errorf("unresolved alias: class=%v relDir=%q, want external/empty", class, relDir)
	}
}

func TestClassifyStd(t *testing.T) {
	root := t.TempDir()
	for _, spec := range []string{"fs", "path", "node:fs", "node:crypto", "os", "http", "util", "stream", "events"} {
		class, relDir, ok := classify(spec, root, root, &tsconfig{})
		if !ok || class != core.ClassStd || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want std", spec, class, relDir, ok)
		}
	}
}

func TestClassifyExternal(t *testing.T) {
	root := t.TempDir()
	for _, spec := range []string{"react", "lodash", "@scope/pkg", "@scope/pkg/sub", "express"} {
		class, relDir, ok := classify(spec, root, root, &tsconfig{})
		if !ok || class != core.ClassExternal || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want external", spec, class, relDir, ok)
		}
	}
}

func TestClassifyNodePrefixNonBuiltinStillStd(t *testing.T) {
	// Any node:-prefixed specifier is treated as std even if not in our set.
	root := t.TempDir()
	class, _, ok := classify("node:test", root, root, &tsconfig{})
	if !ok || class != core.ClassStd {
		t.Errorf("node:test: class=%v ok=%v, want std", class, ok)
	}
}

// Guard: unreadable / weird paths must not panic.
func TestResolveOnDiskNoCrash(t *testing.T) {
	root := t.TempDir()
	_, ok := resolveOnDisk(filepath.Join(root, "nope", "still-nope"))
	if ok {
		t.Errorf("resolveOnDisk of missing path returned ok")
	}
	// A directory with no index file should not resolve.
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveOnDisk(filepath.Join(root, "empty")); ok {
		t.Errorf("resolveOnDisk of index-less dir returned ok")
	}
}
