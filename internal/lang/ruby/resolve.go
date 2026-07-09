package ruby

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// classify buckets one require reference into a core.Class and, for in-module
// targets, derives the module-relative directory of the required file.
//
//   - require_relative "path" -> resolve path against the importing file's
//     directory (appending .rb when absent). In-module if it lands on a .rb file
//     under root; external otherwise (a broken relative require can't fabricate a
//     violation).
//   - require "feature" / autoload :C, "feature" -> try to resolve on disk under
//     root first (against the project root and a conventional lib/ load path), a
//     first-party file that shadows nothing; in-module if found. Else std if the
//     feature (or its head segment) is a Ruby standard-library name; else
//     external (a gem).
//
// display is the raw feature argument shown in reports and used as the edge key.
// ok is always true: classification degrades, never fails.
func classify(ref importRef, fromDir, root string) (class core.Class, relDir, display string, ok bool) {
	display = ref.Feature

	if ref.Kind == kindRelative {
		target := filepath.Join(fromDir, filepath.FromSlash(ref.Feature))
		if dir, found := resolveRubyDir(target); found && withinRoot(root, dir) {
			return core.ClassInModule, relDirOf(root, dir), display, true
		}
		return core.ClassExternal, "", display, true
	}

	// Plain require / autoload: try to resolve as a first-party file first.
	rel := filepath.FromSlash(ref.Feature)
	for _, base := range loadBases(root) {
		target := filepath.Join(base, rel)
		if dir, found := resolveRubyDir(target); found && withinRoot(root, dir) {
			return core.ClassInModule, relDirOf(root, dir), display, true
		}
	}
	if isStdlib(ref.Feature) {
		return core.ClassStd, "", display, true
	}
	return core.ClassExternal, "", display, true
}

// loadBases lists the directories a plain `require` is resolved against, in
// priority order: the project root and a conventional lib/ load path (the two
// entries Ruby projects almost always put on $LOAD_PATH). Only bases that exist
// are returned.
func loadBases(root string) []string {
	bases := []string{root}
	if lib := filepath.Join(root, "lib"); isDir(lib) {
		bases = append(bases, lib)
	}
	return bases
}

// resolveRubyDir maps a filesystem base (a require target that may or may not
// carry a .rb suffix) to the directory that owns the resolved .rb file. It
// resolves when base.rb exists, or base already ends in .rb and exists. The
// returned dir is the graph node the edge points at — always a directory,
// mirroring the "node is a source directory" model of the other adapters.
func resolveRubyDir(base string) (dir string, ok bool) {
	if strings.HasSuffix(base, ".rb") {
		if isFile(base) {
			return filepath.Dir(base), true
		}
		return "", false
	}
	if isFile(base + ".rb") {
		return filepath.Dir(base), true
	}
	return "", false
}

// relDirOf derives the module-relative, slash-separated directory of dir,
// returning "." for the root itself — the same convention as the other adapters.
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
