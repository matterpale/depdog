package python

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// classify buckets one import reference into a core.Class and, for in-module
// targets, derives the module-relative directory of the imported package.
//
//   - relative (`from .` / `from ..pkg`) -> resolve against the importing
//     file's package by walking up `Level` directories, then descending into
//     the dotted module. In-module if it lands on a package/module under root;
//     external otherwise (a broken relative import can't fabricate a violation).
//   - absolute dotted name -> try to resolve under root first (a first-party
//     package that shadows nothing); in-module if found. Else std if the head
//     segment is a standard-library module; else external.
//
// display is the raw specifier shown in reports and used as the edge key. ok is
// always true: classification degrades, never fails.
func classify(ref importRef, fromDir, root string) (class core.Class, relDir, display string, ok bool) {
	if ref.Level > 0 {
		display = relativeDisplay(ref)
		base := fromDir
		// Level 1 (`.`) means the current package dir; each extra dot goes up
		// one more directory.
		for i := 1; i < ref.Level; i++ {
			base = filepath.Dir(base)
		}
		target := base
		if ref.Module != "" {
			target = filepath.Join(base, filepath.FromSlash(strings.ReplaceAll(ref.Module, ".", "/")))
		}
		if dir, found := resolvePackageDir(target); found && withinRoot(root, dir) {
			return core.ClassInModule, relDirOf(root, dir), display, true
		}
		return core.ClassExternal, "", display, true
	}

	// Absolute import.
	display = ref.Module
	target := filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(ref.Module, ".", "/")))
	if dir, found := resolvePackageDir(target); found && withinRoot(root, dir) {
		return core.ClassInModule, relDirOf(root, dir), display, true
	}
	if isStdlib(ref.Module) {
		return core.ClassStd, "", display, true
	}
	return core.ClassExternal, "", display, true
}

// relativeDisplay renders a relative import back to its source form for reports:
// a leading run of dots (Level) followed by the dotted module (if any).
func relativeDisplay(ref importRef) string {
	dots := strings.Repeat(".", ref.Level)
	if ref.Module == "" {
		return dots
	}
	return dots + ref.Module
}

// resolvePackageDir maps a filesystem base (a would-be module path without an
// extension) to the directory that owns it. A target resolves when it is:
//
//   - a package directory (dir containing __init__.py or __init__.pyi), OR
//   - a plain directory that holds any .py/.pyi files (namespace-style pkg), OR
//   - a module file base.py / base.pyi (the owning directory is returned).
//
// The returned dir is the graph node the edge points at — always a directory,
// mirroring the Go/TS "node is a package directory" model.
func resolvePackageDir(base string) (dir string, ok bool) {
	if isDir(base) {
		if isFile(filepath.Join(base, "__init__.py")) || isFile(filepath.Join(base, "__init__.pyi")) {
			return base, true
		}
		if dirHasPython(base) {
			return base, true
		}
	}
	for _, ext := range []string{".py", ".pyi"} {
		if isFile(base + ext) {
			return filepath.Dir(base), true
		}
	}
	return "", false
}

// dirHasPython reports whether dir directly contains a .py or .pyi file.
func dirHasPython(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".py" || ext == ".pyi" {
			return true
		}
	}
	return false
}

// relDirOf derives the module-relative, slash-separated directory of dir,
// returning "." for the root itself — the same convention as the Go/TS adapters.
func relDirOf(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "."
	}
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." {
		return "."
	}
	return rel
}

// withinRoot reports whether dir is root or lives under it.
func withinRoot(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (!strings.HasPrefix(rel, "../") && rel != "..")
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
