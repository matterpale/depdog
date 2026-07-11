package scala

import (
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// stdPrefixes are the leading package segments treated as the platform standard
// library rather than an external dependency: the Scala stdlib and the JVM
// platform a Scala project targets.
//
//	scala.*    the Scala standard library
//	java.*     javax.*     the JVM platform
//
// The bare identifier `Predef` (Scala's auto-imported prelude, e.g.
// `import Predef.…`) is also treated as std. It is a top-level (empty-package)
// name, handled separately from the prefix list.
var stdPrefixes = []string{"scala", "java", "javax"}

// classify buckets one import reference into a core.Class and, for in-module
// targets, derives the source-root-relative directory of the imported package.
//
//   - a package declared by some source file in this project (present in
//     declared) -> in-module, keyed to that package's node directory.
//   - a package under a platform prefix (scala./java./javax.) or the bare
//     `Predef` prelude -> std.
//   - anything else -> an external dependency.
//
// declared maps a dotted package name to its source-root-relative node dir (the
// map the loader builds from every file's `package` declaration). display is the
// specifier shown in reports and used as the edge key. ok is always true:
// classification degrades, never fails.
func classify(ref importRef, declared map[string]string) (class core.Class, relDir, display string, ok bool) {
	display = ref.Display
	if dir, found := declared[ref.Pkg]; found {
		return core.ClassInModule, dir, display, true
	}
	if isStdlib(ref.Pkg) {
		return core.ClassStd, "", display, true
	}
	return core.ClassExternal, "", display, true
}

// isStdlib reports whether a dotted package sits under a platform prefix, or is
// the bare `Predef` prelude. Matching is on a segment boundary so "scalafoo.Bar"
// is not mistaken for the "scala" stdlib.
func isStdlib(pkg string) bool {
	if pkg == "Predef" {
		return true
	}
	head := pkg
	if i := strings.IndexByte(pkg, '.'); i >= 0 {
		head = pkg[:i]
	}
	for _, p := range stdPrefixes {
		if head == p {
			return true
		}
	}
	return false
}

// relDirOf derives the source-root-relative, slash-separated directory of dir,
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
