package elm

import (
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// classify buckets one imported module name into a core.Class and, for in-module
// targets, derives the project-relative directory the module's file lives in.
//
// Elm resolves by MODULE NAME, not by path. A module `Foo.Bar`:
//
//   - is in-module if a file `<srcDir>/Foo/Bar.elm` exists under one of the
//     project's source-directories — the graph node is the DIRECTORY that file
//     sits in, `<srcDir>/Foo` (mirroring every other adapter's "node is a
//     directory" model). The declared map, built by the loader from every
//     source module it found, is the lookup.
//   - else, if it is an elm/core module -> std.
//   - else -> external (it comes from an elm.json dependency package).
//
// declared maps a fully-qualified module name to its project-relative node dir.
// display is the module name shown in reports and used as the edge key. ok is
// always true: classification degrades, never fails.
func classify(ref importRef, declared map[string]string) (class core.Class, relDir, display string, ok bool) {
	display = ref.Module
	if dir, found := declared[ref.Module]; found {
		return core.ClassInModule, dir, display, true
	}
	if isStdlib(ref.Module) {
		return core.ClassStd, "", display, true
	}
	return core.ClassExternal, "", display, true
}

// moduleNodeDir maps a module name and the source directory it was found under
// to the project-relative directory of its file. `Foo.Bar` under srcDir `src`
// lives at `src/Foo/Bar.elm`, so its node dir is `src/Foo`; a top-level module
// `Main` under `src` lives at `src/Main.elm`, so its node dir is `src`.
func moduleNodeDir(srcDir, module string) string {
	parts := strings.Split(module, ".")
	// The last segment is the file basename; the segments before it are nested
	// directories under the source dir.
	dirParts := parts[:len(parts)-1]
	dir := srcDir
	if len(dirParts) > 0 {
		dir = filepath.ToSlash(filepath.Join(append([]string{srcDir}, dirParts...)...))
	}
	if dir == "" {
		return "."
	}
	return dir
}

// moduleFromFile derives an Elm module name from a project-relative source file
// path and the source directory it lives under. `src/Foo/Bar.elm` under srcDir
// `src` is the module `Foo.Bar`; `src/Main.elm` is `Main`. ok is false when the
// file is not actually under srcDir or is not a .elm file.
func moduleFromFile(srcDir, relFile string) (module string, ok bool) {
	rel := filepath.ToSlash(relFile)
	if !strings.HasSuffix(rel, ".elm") {
		return "", false
	}
	stem := strings.TrimSuffix(rel, ".elm")
	prefix := srcDir
	if prefix == "." {
		prefix = ""
	}
	switch {
	case prefix == "":
		// Source dir is the project root: the whole stem is the dotted module.
	case stem == prefix:
		return "", false // the source dir itself, not a file under it
	case strings.HasPrefix(stem, prefix+"/"):
		stem = strings.TrimPrefix(stem, prefix+"/")
	default:
		return "", false // not under this source dir
	}
	if stem == "" {
		return "", false
	}
	return strings.ReplaceAll(stem, "/", "."), true
}

// relDirOf derives the project-relative, slash-separated directory of dir,
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
