package java

import (
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// stdPrefixes are the leading package segments of the Java platform / JDK, whose
// imports classify as std rather than an external dependency.
//
//	java.*    javax.*   the core platform
//	jakarta.* the Jakarta EE (renamed javax) namespace shipped with the platform
//	jdk.*     sun.*     JDK-internal modules
var stdPrefixes = []string{"java", "javax", "jakarta", "jdk", "sun"}

// classify buckets one import reference into a core.Class and, for in-module
// targets, derives the source-root-relative directory of the imported package.
//
//   - a package declared by some source file in this project (present in
//     declared) -> in-module, keyed to that package's node directory.
//   - a package under a Java-platform prefix (java./javax./jakarta./jdk./sun.)
//     -> std.
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

// isStdlib reports whether a dotted package sits under a Java-platform prefix.
// Matching is on a segment boundary so "javafoo.Bar" is not mistaken for the
// "java" platform.
func isStdlib(pkg string) bool {
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
