package ruby

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
	for _, feature := range []string{"json", "set", "uri", "net/http", "fileutils", "logger", "digest", "date", "time", "securerandom"} {
		class, relDir, _, ok := classify(importRef{Feature: feature, Kind: kindRequire}, root, root)
		if !ok || class != core.ClassStd || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want std", feature, class, relDir, ok)
		}
	}
}

func TestClassifyExternal(t *testing.T) {
	root := t.TempDir()
	for _, feature := range []string{"sinatra", "rails", "sinatra/base", "rspec/core", "activerecord"} {
		class, relDir, _, ok := classify(importRef{Feature: feature, Kind: kindRequire}, root, root)
		if !ok || class != core.ClassExternal || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want external", feature, class, relDir, ok)
		}
	}
}

func TestClassifyRequireResolvesToInModule(t *testing.T) {
	// A plain require of a first-party file (resolved against root and lib/)
	// classifies as in-module, not external.
	root := setupProject(t, map[string]string{
		"domain/order.rb":   "",
		"lib/util/thing.rb": "",
	})
	tests := []struct {
		name    string
		feature string
		wantDir string
	}{
		{"root-relative file", "domain/order", "domain"},
		{"root-relative with suffix", "domain/order.rb", "domain"},
		{"lib load path", "util/thing", "lib/util"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, relDir, display, ok := classify(importRef{Feature: tc.feature, Kind: kindRequire}, root, root)
			if !ok || class != core.ClassInModule {
				t.Fatalf("classify(%q): class=%v ok=%v, want in-module", tc.feature, class, ok)
			}
			if relDir != tc.wantDir {
				t.Errorf("classify(%q) relDir = %q, want %q", tc.feature, relDir, tc.wantDir)
			}
			if display != tc.feature {
				t.Errorf("classify(%q) display = %q, want %q", tc.feature, display, tc.feature)
			}
		})
	}
}

func TestClassifyRequireShadowsStdlib(t *testing.T) {
	// A first-party file that happens to share a stdlib feature name resolves to
	// in-module (it exists on disk under root), not std.
	root := setupProject(t, map[string]string{
		"json.rb": "",
	})
	class, relDir, _, ok := classify(importRef{Feature: "json", Kind: kindRequire}, root, root)
	if !ok || class != core.ClassInModule || relDir != "." {
		t.Errorf("first-party json: class=%v relDir=%q, want in-module .", class, relDir)
	}
}

func TestClassifyRelative(t *testing.T) {
	root := setupProject(t, map[string]string{
		"pkg/sub/mod.rb":   "",
		"pkg/sub/other.rb": "",
		"pkg/parent.rb":    "",
		"pkg/other/x.rb":   "",
	})
	subDir := filepath.Join(root, "pkg", "sub")

	tests := []struct {
		name       string
		feature    string
		wantClass  core.Class
		wantRelDir string
	}{
		{"sibling file", "other", core.ClassInModule, "pkg/sub"},
		{"sibling file with suffix", "other.rb", core.ClassInModule, "pkg/sub"},
		{"up one dir file", "../parent", core.ClassInModule, "pkg"},
		{"up into sibling dir", "../other/x", core.ClassInModule, "pkg/other"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, relDir, display, ok := classify(importRef{Feature: tc.feature, Kind: kindRelative}, subDir, root)
			if !ok || class != tc.wantClass || relDir != tc.wantRelDir {
				t.Errorf("classify(%q): class=%v relDir=%q, want %v %q", tc.feature, class, relDir, tc.wantClass, tc.wantRelDir)
			}
			if display != tc.feature {
				t.Errorf("classify(%q) display = %q, want %q", tc.feature, display, tc.feature)
			}
		})
	}
}

func TestClassifyRelativeUnresolvedIsExternal(t *testing.T) {
	// A relative require that resolves to nothing on disk degrades to external
	// rather than fabricating an in-module edge.
	root := setupProject(t, map[string]string{
		"pkg/a.rb": "",
	})
	class, relDir, _, ok := classify(importRef{Feature: "missing", Kind: kindRelative}, filepath.Join(root, "pkg"), root)
	if !ok || class != core.ClassExternal || relDir != "" {
		t.Errorf("unresolved relative: class=%v relDir=%q, want external", class, relDir)
	}
}

func TestClassifyAutoloadResolvesLikeRequire(t *testing.T) {
	root := setupProject(t, map[string]string{
		"domain/order.rb": "",
	})
	class, relDir, _, ok := classify(importRef{Feature: "domain/order", Kind: kindAutoload}, root, root)
	if !ok || class != core.ClassInModule || relDir != "domain" {
		t.Errorf("autoload in-module: class=%v relDir=%q, want in-module domain", class, relDir)
	}
	// An autoload of a std feature is std.
	stdClass, _, _, _ := classify(importRef{Feature: "set", Kind: kindAutoload}, root, root)
	if stdClass != core.ClassStd {
		t.Errorf("autoload of std feature: class=%v, want std", stdClass)
	}
}

func TestResolveRubyDirNoCrash(t *testing.T) {
	root := t.TempDir()
	if _, ok := resolveRubyDir(filepath.Join(root, "nope", "still-nope")); ok {
		t.Errorf("resolveRubyDir of missing path returned ok")
	}
	if _, ok := resolveRubyDir(filepath.Join(root, "nope.rb")); ok {
		t.Errorf("resolveRubyDir of missing .rb returned ok")
	}
}
