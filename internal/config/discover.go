package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModuleRoot walks up from startDir to the nearest go.mod and returns its
// directory — the single module a command operates on. Workspaces are handled a
// layer up (see config.FindWorkspace and the CLI's workspace fan-out); this
// function always resolves exactly one module, which is also the module a
// workspace member is checked as. Shared by `check`, `init`, and friends.
func ModuleRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for d := dir; ; {
		if exists(filepath.Join(d, "go.mod")) {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", fmt.Errorf("no go.mod found from %s upward — depdog runs inside a Go module", dir)
}

// Unit is a discovered check unit: a directory holding a depdog.yaml.
type Unit struct {
	Dir string // absolute
	Rel string // walk-root-relative, slash-separated ("." for the root itself)
}

// discoverSkip names directories the walk prunes wholesale (never reads their
// subtree). Any component starting with "." is pruned in addition to these; see
// DiscoverUnits. testdata matters for dogfooding: depdog's own repo has many
// fixture configs under testdata/, and `check --all` at our root must find
// exactly one unit.
var discoverSkip = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"testdata":     true,
	"target":       true,
	"dist":         true,
	"build":        true,
	"out":          true,
	"__pycache__":  true,
}

// DiscoverUnits walks down from root collecting every directory that holds a
// depdog.yaml, pruning skip-listed directories, plus the marker-bearing
// directories disjoint from every unit (the advisory-skip candidates).
//
// A directory is skipped (pruned via fs.SkipDir, so its subtree is never read)
// when its name starts with "." or appears in discoverSkip. The skip list
// applies to directories below root; root itself is always entered. Symlinks
// are not followed (WalkDir default). Nested units are allowed: a config below
// another config roots its own unit.
//
// units is returned sorted lexicographically by Rel; Rel is "." when root
// itself holds a depdog.yaml. ungoverned lists the root-relative (slash) marker
// directories that are disjoint from every unit — no unit is the directory
// itself, an ancestor of it, or a descendant of it — sorted lexicographically.
// markers is the set of adapter marker file names (e.g. go.mod, Gemfile),
// passed in so this package need not depend on the CLI adapter registry.
func DiscoverUnits(root string, markers []string) (units []Unit, ungoverned []string, err error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	markerSet := make(map[string]bool, len(markers))
	for _, m := range markers {
		markerSet[m] = true
	}

	// unitRels tracks discovered units (slash paths) so we can apply the
	// containment filter to marker dirs after the walk. markerRels tracks every
	// marker-bearing directory found (slash paths).
	var unitRels []string
	var markerRels []string

	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		// Prune skip-listed and dot-prefixed directories below root. root
		// itself is always entered (path == absRoot).
		if path != absRoot {
			name := d.Name()
			if strings.HasPrefix(name, ".") || discoverSkip[name] {
				return fs.SkipDir
			}
		}

		rel, relErr := relSlash(absRoot, path)
		if relErr != nil {
			return relErr
		}
		if exists(filepath.Join(path, DefaultName)) {
			units = append(units, Unit{Dir: path, Rel: rel})
			unitRels = append(unitRels, rel)
		}
		if hasAnyMarker(path, markerSet) {
			markerRels = append(markerRels, rel)
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, fmt.Errorf("discovering units under %s: %w", absRoot, walkErr)
	}

	for _, mr := range markerRels {
		if disjointFromUnits(mr, unitRels) {
			ungoverned = append(ungoverned, mr)
		}
	}

	sort.Slice(units, func(i, j int) bool { return units[i].Rel < units[j].Rel })
	sort.Strings(ungoverned)
	return units, ungoverned, nil
}

// relSlash returns the slash-separated path of target relative to base, or "."
// when they are the same directory.
func relSlash(base, target string) (string, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// hasAnyMarker reports whether dir directly contains any of the marker files.
func hasAnyMarker(dir string, markerSet map[string]bool) bool {
	for m := range markerSet {
		if exists(filepath.Join(dir, m)) {
			return true
		}
	}
	return false
}

// disjointFromUnits reports whether the marker dir markerRel has no unit that
// is the directory itself, an ancestor of it, or a descendant of it (all slash
// paths, "." being the walk root). A disjoint marker dir is an advisory-skip
// candidate; a marker dir that is or contains or sits under a unit is governed.
func disjointFromUnits(markerRel string, unitRels []string) bool {
	for _, u := range unitRels {
		if u == markerRel || isAncestorRel(u, markerRel) || isAncestorRel(markerRel, u) {
			return false
		}
	}
	return true
}

// isAncestorRel reports whether ancestor is a strict ancestor directory of
// descendant, both slash paths ("." denoting the walk root, which is an
// ancestor of everything else).
func isAncestorRel(ancestor, descendant string) bool {
	if ancestor == descendant {
		return false
	}
	if ancestor == "." {
		return true
	}
	return strings.HasPrefix(descendant, ancestor+"/")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
