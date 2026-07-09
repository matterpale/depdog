package rust

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
	src := filepath.Join(root, "src")
	for _, path := range []string{"std::io", "std::collections::HashMap", "core::mem", "alloc::vec::Vec", "proc_macro::TokenStream"} {
		class, relDir, _, ok := classify(importRef{Path: path}, src, root)
		if !ok || class != core.ClassStd || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want std", path, class, relDir, ok)
		}
	}
}

func TestClassifyExternal(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	for _, path := range []string{"serde::Deserialize", "tokio::spawn", "anyhow::Result", "clap::Parser"} {
		class, relDir, _, ok := classify(importRef{Path: path}, src, root)
		if !ok || class != core.ClassExternal || relDir != "" {
			t.Errorf("classify(%q): class=%v relDir=%q ok=%v, want external", path, class, relDir, ok)
		}
	}
}

func TestClassifyCrateInModule(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Cargo.toml":         "[package]\nname = \"ex\"\n",
		"src/lib.rs":         "pub mod domain;\npub mod service;\n",
		"src/domain/mod.rs":  "pub struct Order;\n",
		"src/domain/item.rs": "pub struct Item;\n",
		"src/service.rs":     "pub fn run() {}\n",
	})
	fromDir := filepath.Join(root, "src")
	tests := []struct {
		name    string
		path    string
		wantDir string
	}{
		{"module dir via mod.rs", "crate::domain::Order", "src/domain"},
		{"leaf file inside dir", "crate::domain::item::Item", "src/domain"},
		{"module as sibling file", "crate::service::run", "src"},
		{"module dir root", "crate::domain", "src/domain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, relDir, display, ok := classify(importRef{Path: tc.path}, fromDir, root)
			if !ok || class != core.ClassInModule {
				t.Fatalf("classify(%q): class=%v ok=%v, want in-module", tc.path, class, ok)
			}
			if relDir != tc.wantDir {
				t.Errorf("classify(%q) relDir = %q, want %q", tc.path, relDir, tc.wantDir)
			}
			if display != tc.path {
				t.Errorf("classify(%q) display = %q, want %q", tc.path, display, tc.path)
			}
		})
	}
}

func TestClassifyCrateUnresolvedIsExternal(t *testing.T) {
	// A crate:: path that resolves to nothing on disk degrades to external
	// rather than fabricating an in-crate edge.
	root := setupProject(t, map[string]string{
		"Cargo.toml":        "[package]\nname = \"ex\"\n",
		"src/lib.rs":        "pub mod domain;\n",
		"src/domain/mod.rs": "",
	})
	class, relDir, _, ok := classify(importRef{Path: "crate::nowhere::thing"}, filepath.Join(root, "src"), root)
	if !ok || class != core.ClassExternal || relDir != "" {
		t.Errorf("unresolved crate path: class=%v relDir=%q, want external", class, relDir)
	}
}

func TestClassifySelfAndSuper(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Cargo.toml":            "[package]\nname = \"ex\"\n",
		"src/lib.rs":            "pub mod domain;\n",
		"src/domain/mod.rs":     "pub mod order;\npub mod ext;\n",
		"src/domain/order.rs":   "",
		"src/domain/ext.rs":     "",
		"src/domain/sub/mod.rs": "",
	})
	domainDir := filepath.Join(root, "src", "domain")
	subDir := filepath.Join(root, "src", "domain", "sub")
	tests := []struct {
		name    string
		path    string
		from    string
		wantDir string
	}{
		{"self current dir", "self::order", domainDir, "src/domain"},
		{"super from sub", "super::ext", subDir, "src/domain"},
		{"self bare", "self", domainDir, "src/domain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, relDir, _, ok := classify(importRef{Path: tc.path}, tc.from, root)
			if !ok || class != core.ClassInModule || relDir != tc.wantDir {
				t.Errorf("classify(%q from %s): class=%v relDir=%q, want in-module %q", tc.path, tc.from, class, relDir, tc.wantDir)
			}
		})
	}
}

func TestClassifyModDeclaration(t *testing.T) {
	root := setupProject(t, map[string]string{
		"Cargo.toml":        "[package]\nname = \"ex\"\n",
		"src/lib.rs":        "pub mod domain;\n",
		"src/domain/mod.rs": "",
	})
	// `mod domain;` declared in src/lib.rs -> child module dir src/domain.
	ref := importRef{Path: modToken + "::domain"}
	class, relDir, display, ok := classify(ref, filepath.Join(root, "src"), root)
	if !ok || class != core.ClassInModule || relDir != "src/domain" {
		t.Errorf("mod domain: class=%v relDir=%q, want in-module src/domain", class, relDir)
	}
	if display != "mod domain" {
		t.Errorf("mod domain display = %q, want \"mod domain\"", display)
	}
}

func TestResolveModuleDirNoCrash(t *testing.T) {
	root := t.TempDir()
	if _, ok := resolveModuleDir(filepath.Join(root, "nope"), []string{"still", "nope"}); ok {
		t.Errorf("resolveModuleDir of missing path returned ok")
	}
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveModuleDir(root, []string{"empty"}); ok {
		t.Errorf("resolveModuleDir of empty dir returned ok")
	}
}
