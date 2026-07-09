package rust

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// Path-head tokens with special meaning in a Rust use/mod path.
const (
	crateToken = "crate"
	selfToken  = "self"
	superToken = "super"
	// modToken flags a synthetic ref produced by a `mod name;` declaration so it
	// resolves relative to the declaring file's directory. It is never a real
	// path a user writes, so it can't collide with a source identifier.
	modToken = "\x00mod"
)

// stdCrates are the Rust standard-library facade crates. A path whose head is
// one of these is classified std.
var stdCrates = map[string]bool{
	"std":        true,
	"core":       true,
	"alloc":      true,
	"proc_macro": true,
}

// classify buckets one import reference into a core.Class and, for in-crate
// targets, derives the module-relative directory of the imported module.
//
//   - crate::a::b  -> resolve `a::b` under the crate's src tree; in-crate when
//     it lands on a module dir/file, external otherwise (a stale path can't
//     fabricate a violation).
//   - self::x / super::x -> resolve relative to the importing file's directory
//     (self = current dir, each `super` climbs one module up); in-crate when it
//     resolves under root.
//   - a `mod name;` declaration -> the child module in the current directory.
//   - std / core / alloc -> std.
//   - any other head -> an external Cargo dependency (external).
//
// display is the specifier shown in reports and used as the edge key. ok is
// always true: classification degrades, never fails.
func classify(ref importRef, fromDir, root string) (class core.Class, relDir, display string, ok bool) {
	segs := splitPath(ref.Path)
	if len(segs) == 0 {
		return core.ClassExternal, "", ref.Path, false
	}
	head := segs[0]

	switch head {
	case modToken:
		// mod name; -> child module `name` under the declaring file's dir.
		display = "mod " + strings.Join(segs[1:], "::")
		if dir, found := resolveModuleDir(fromDir, segs[1:]); found && withinRoot(root, dir) {
			return core.ClassInModule, relDirOf(root, dir), display, true
		}
		return core.ClassInModule, relDirOf(root, fromDir), display, true

	case crateToken:
		display = ref.Path
		base, ok := crateSrcRoot(root)
		if !ok {
			return core.ClassExternal, "", display, true
		}
		if dir, found := resolveModuleDir(base, segs[1:]); found && withinRoot(root, dir) {
			return core.ClassInModule, relDirOf(root, dir), display, true
		}
		return core.ClassExternal, "", display, true

	case selfToken, superToken:
		display = ref.Path
		base := fromDir
		i := 0
		for i < len(segs) && segs[i] == superToken {
			base = filepath.Dir(base)
			i++
		}
		if i < len(segs) && segs[i] == selfToken {
			i++ // self keeps the current base
		}
		if dir, found := resolveModuleDir(base, segs[i:]); found && withinRoot(root, dir) {
			return core.ClassInModule, relDirOf(root, dir), display, true
		}
		if withinRoot(root, base) {
			return core.ClassInModule, relDirOf(root, base), display, true
		}
		return core.ClassExternal, "", display, true

	default:
		display = ref.Path
		if stdCrates[head] {
			return core.ClassStd, "", display, true
		}
		return core.ClassExternal, "", display, true
	}
}

// crateSrcRoot returns the directory the crate root module lives in: src/ when a
// src/lib.rs or src/main.rs exists, else the crate root itself (a src-less
// layout). ok is false only when neither exists (rare; caller degrades to
// external).
func crateSrcRoot(root string) (string, bool) {
	src := filepath.Join(root, "src")
	if isFile(filepath.Join(src, "lib.rs")) || isFile(filepath.Join(src, "main.rs")) {
		return src, true
	}
	if isDir(src) && dirHasRust(src) {
		return src, true
	}
	if dirHasRust(root) {
		return root, true
	}
	return "", false
}

// resolveModuleDir walks a `::`-path of module segments from base and returns the
// directory that owns the final module. Each segment `seg` resolves to:
//
//   - base/seg/       (a directory holding .rs files or a mod.rs), descended into, OR
//   - base/seg.rs     (a leaf module file; its owning directory is returned).
//
// The returned dir is the graph node the edge points at — always a directory,
// mirroring the Go/TS/Py "node is a package directory" model. An empty segment
// list resolves to base itself when base holds Rust source.
func resolveModuleDir(base string, segs []string) (dir string, ok bool) {
	if len(segs) == 0 {
		if dirHasRust(base) {
			return base, true
		}
		return "", false
	}
	cur := base
	resolvedAny := false
	for _, seg := range segs {
		child := filepath.Join(cur, seg)
		switch {
		case isDir(child) && (isFile(filepath.Join(child, "mod.rs")) || dirHasRust(child)):
			cur = child
			resolvedAny = true
		case isFile(child + ".rs"):
			// A leaf module file: its owning directory is the node. Any deeper
			// segments are items inside that file, so we stop here.
			return cur, true
		default:
			// The segment is not a module on disk: it is a type/function/const
			// imported from the module resolved so far. Attribute the edge to
			// that module's directory (as long as we resolved at least one
			// segment); otherwise there is no such module.
			if resolvedAny {
				return cur, true
			}
			return "", false
		}
	}
	return cur, true
}

// dirHasRust reports whether dir directly contains a .rs file.
func dirHasRust(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".rs" {
			return true
		}
	}
	return false
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

// splitPath splits a "::"-joined path into its segments, dropping empty segments
// that a leading `::` (absolute external path) would produce.
func splitPath(path string) []string {
	raw := strings.Split(path, "::")
	out := raw[:0]
	for _, s := range raw {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
