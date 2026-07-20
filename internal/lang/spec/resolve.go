package spec

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// resolve.go classifies a captured specifier into a core.Class and, for
// in-module targets, the module-relative directory of the resolved file — the
// spec-driven equivalent of a hand-written adapter's resolve.go. This file
// implements the path-resolution family (Ruby, Lua): a specifier is a file path
// resolved against roots + extensions + index files, with relative kinds
// resolved against the importing file's directory. The name-index family (C#,
// Elm) is added in M5.

// classified is the outcome of classifying one captured ref.
type classified struct {
	class   core.Class
	relDir  string // module-relative dir of the target; ClassInModule only
	display string // the edge key shown in reports (the specifier as written)
	ok      bool   // classification degrades, never fails, so this is always true
}

// resolver holds the precomputed, root-relative resolution state for one Load, so
// per-ref classification is allocation-light and deterministic.
type resolver struct {
	spec     *Spec
	root     string
	sep      string
	exts     []string        // extensions to try (resolve.extensions or top-level)
	roots    []string        // absolute base dirs for non-relative specifiers
	idxFiles []string        // index-file basenames (dir imports)
	relKinds map[string]bool // surface kinds resolved against the importing dir
	stdSet   map[string]bool // stdlib table
}

// newResolver precomputes the path-mode resolution state rooted at root. Roots
// are the spec's Resolve.Roots (default ["."]) plus any Resolve.RootsIfExist that
// exist on disk — matching Ruby's [root, lib] load bases, resolved once.
func newResolver(sp *Spec, root string) *resolver {
	r := &sp.Resolve
	rs := &resolver{
		spec:     sp,
		root:     root,
		sep:      r.sep(),
		exts:     r.extensions(sp),
		idxFiles: r.IndexFiles,
		relKinds: sliceSet(r.RelativeKinds),
		stdSet:   sliceSet(sp.Stdlib.Modules),
	}

	roots := r.Roots
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for _, base := range roots {
		rs.roots = append(rs.roots, joinRoot(root, base))
	}
	for _, base := range r.RootsIfExist {
		abs := joinRoot(root, base)
		if isDir(abs) {
			rs.roots = append(rs.roots, abs)
		}
	}
	return rs
}

// classify buckets one ref. A relative-kind ref resolves against the importing
// file's directory; any other ref resolves against the roots, then falls back to
// the stdlib table, then external — the same order as Ruby's classify.
func (rs *resolver) classify(r ref, fromDir string) classified {
	display := r.Specifier
	relPath := specToPath(r.Specifier, rs.sep)

	if rs.relKinds[r.Kind] {
		target := filepath.Join(fromDir, relPath)
		if dir, found := rs.resolveFile(target); found && withinRoot(rs.root, dir) {
			return classified{core.ClassInModule, relDirOf(rs.root, dir), display, true}
		}
		return classified{core.ClassExternal, "", display, true}
	}

	for _, base := range rs.roots {
		target := filepath.Join(base, relPath)
		if dir, found := rs.resolveFile(target); found && withinRoot(rs.root, dir) {
			return classified{core.ClassInModule, relDirOf(rs.root, dir), display, true}
		}
	}
	if rs.isStd(r.Specifier) {
		return classified{core.ClassStd, "", display, true}
	}
	return classified{core.ClassExternal, "", display, true}
}

// resolveFile maps a filesystem target (a specifier joined onto a base, which may
// or may not already carry an extension) to the directory that owns the resolved
// file. It mirrors Ruby's resolveRubyDir, generalized over the spec's extensions
// and index files:
//
//   - target already ends in a configured extension: it resolves iff that exact
//     file exists (no second guess).
//   - else: try target+ext for each extension.
//   - else: an index file (Python __init__.py, Lua init.lua) makes target's own
//     directory the node.
func (rs *resolver) resolveFile(target string) (dir string, ok bool) {
	for _, ext := range rs.exts {
		if strings.HasSuffix(target, ext) {
			if isFile(target) {
				return filepath.Dir(target), true
			}
			return "", false
		}
	}
	for _, ext := range rs.exts {
		if isFile(target + ext) {
			return filepath.Dir(target + ext), true
		}
	}
	for _, idx := range rs.idxFiles {
		if isFile(filepath.Join(target, idx)) {
			return target, true
		}
	}
	return "", false
}

// isStd reports whether a specifier is standard-library. It matches the whole
// specifier, then (Match=head) the head segment before the first separator (Ruby
// net/http -> net), then any configured namespace prefix (C# System -> System.Text).
func (rs *resolver) isStd(specifier string) bool {
	if rs.stdSet[specifier] {
		return true
	}
	st := &rs.spec.Stdlib
	if st.Match == MatchHead && st.Separator != "" {
		head := specifier
		if i := strings.Index(specifier, st.Separator); i >= 0 {
			head = specifier[:i]
		}
		if rs.stdSet[head] {
			return true
		}
	}
	for _, p := range st.Prefixes {
		if specifier == p || strings.HasPrefix(specifier, p+prefixSep(st.Separator)) {
			return true
		}
	}
	return false
}

// specToPath maps a specifier written with the spec's separator to a
// filesystem-relative path (a.b -> a/b for a "." separator; identity for "/").
func specToPath(specifier, sep string) string {
	p := specifier
	if sep != "" && sep != "/" {
		p = strings.ReplaceAll(specifier, sep, "/")
	}
	return filepath.FromSlash(p)
}

// extensions returns the resolution extensions: Resolve.Extensions when set, else
// the spec's top-level Extensions.
func (r *Resolve) extensions(sp *Spec) []string {
	if len(r.Extensions) > 0 {
		return r.Extensions
	}
	return sp.Extensions
}

// joinRoot joins a project-relative base (slash-separated, "." for the root) onto
// the absolute root.
func joinRoot(root, base string) string {
	base = strings.TrimSpace(base)
	if base == "" || base == "." {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(base))
}

// prefixSep returns the separator used to test a namespace prefix boundary,
// defaulting to "." when the stdlib table sets none.
func prefixSep(sep string) string {
	if sep == "" {
		return "."
	}
	return sep
}

// relDirOf derives the module-relative, slash-separated directory of dir,
// returning "." for the root itself — the convention every adapter shares.
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

// sliceSet turns a slice into a membership set.
func sliceSet(xs []string) map[string]bool {
	if len(xs) == 0 {
		return nil
	}
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
