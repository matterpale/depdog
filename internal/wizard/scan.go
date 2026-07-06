package wizard

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Scan is the result of walking a module: the module-relative directories that
// contain Go source, sorted for determinism.
type Scan struct {
	Dirs []string
}

// skipDir names directories that never hold analyzable first-party packages.
// Matched on the base name at any depth.
var skipDir = map[string]bool{
	"testdata":     true,
	"vendor":       true,
	"node_modules": true,
}

// ScanModule walks root and records every directory that directly contains a
// non-test .go file, as a module-relative slash path. The module root itself
// ("." ) is never returned as a component candidate. Hidden dirs (a leading
// dot), underscore dirs, and vendor/testdata/node_modules trees are skipped.
func ScanModule(root string) (Scan, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			base := d.Name()
			if skipDir[base] || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
				return fs.SkipDir
			}
			return nil
		}
		if !isGoSource(d.Name()) {
			return nil
		}
		rel, rerr := filepath.Rel(root, filepath.Dir(path))
		if rerr != nil {
			return rerr
		}
		if rel != "." {
			seen[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		return Scan{}, err
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return Scan{Dirs: dirs}, nil
}

// isGoSource reports whether name is a non-test Go source file. Test files are
// excluded so a directory holding only _test.go (an external test package)
// does not masquerade as a component.
func isGoSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}
