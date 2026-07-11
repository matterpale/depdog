package elm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// elmJSON is the subset of an elm.json we read. `source-directories` is present
// only in an application elm.json (an array of project-relative dirs); a package
// elm.json omits it and always uses `src`. `type` distinguishes the two and
// `name` (packages only, e.g. "author/project") gives a friendlier module label.
type elmJSON struct {
	Type       string   `json:"type"`
	Name       string   `json:"name"`
	SourceDirs []string `json:"source-directories"`
}

// readProject parses the elm.json at root and returns the project's
// source-directories (slash-normalized, project-relative) and its module label.
// It is a pure encoding/json read — depdog never invokes the elm toolchain.
//
// The source-directories default to ["src"] when the file is an application with
// no explicit list, or a package (which always builds from src/). An invalid or
// unreadable elm.json is an error: the marker exists but the project is
// malformed, and silently guessing a layout would hide the real problem.
func readProject(root string) (sourceDirs []string, modLabel string, err error) {
	path := filepath.Join(root, "elm.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %w", path, err)
	}

	var ej elmJSON
	if err := json.Unmarshal(data, &ej); err != nil {
		return nil, "", fmt.Errorf("parsing %s: %w", path, err)
	}

	dirs := normalizeSourceDirs(ej.SourceDirs)
	return dirs, moduleLabel(ej, root), nil
}

// normalizeSourceDirs cleans, slash-normalizes and de-duplicates the raw
// source-directories, dropping empties. An empty result falls back to ["src"] —
// the default for a package (no list) and the conventional default for an
// application whose list is absent.
func normalizeSourceDirs(raw []string) []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, d := range raw {
		nd := filepath.ToSlash(filepath.Clean(strings.TrimSpace(d)))
		nd = strings.TrimPrefix(nd, "./")
		if nd == "" || nd == "." {
			// "." (the project root itself) is a legal source directory; keep it.
			nd = "."
		}
		if seen[nd] {
			continue
		}
		seen[nd] = true
		dirs = append(dirs, nd)
	}
	if len(dirs) == 0 {
		return []string{"src"}
	}
	return dirs
}

// moduleLabel derives the graph's ModulePath / display prefix: a package's
// `name` (e.g. "author/project") when present, else the root directory basename
// (applications have no name in elm.json).
func moduleLabel(ej elmJSON, root string) string {
	if name := strings.TrimSpace(ej.Name); name != "" {
		return name
	}
	return filepath.Base(root)
}
