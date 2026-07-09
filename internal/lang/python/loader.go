// Package python is the Python language adapter: it statically scans a
// project's .py/.pyi source files for import statements (import a.b,
// import a.b as c, from a.b import x, and relative from . / from ..pkg
// imports), resolves them against the on-disk package layout, and builds the
// same directory-keyed *core.Graph the Go and TypeScript adapters produce. It
// runs no Python interpreter and needs no installed packages — it reads enough
// to find the edges, nothing more.
package python

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// Loader loads the Python project rooted at (or above) Dir.
type Loader struct {
	Dir string
}

var _ lang.Loader = (*Loader)(nil)

// sourceExtSet is the set of source extensions the scanner reads.
var sourceExtSet = map[string]bool{
	".py": true, ".pyi": true,
}

// Load walks the project root, groups source files into one node per directory,
// scans each file for import statements, classifies them, and returns a
// deterministic *core.Graph.
func (l *Loader) Load(_ context.Context, patterns ...string) (*core.Graph, error) {
	root, err := projectRoot(l.Dir)
	if err != nil {
		return nil, err
	}
	modPath := projectName(root)

	files, err := discoverFiles(root, patterns)
	if err != nil {
		return nil, err
	}

	// impAgg accumulates the occurrences of one (node, specifier) edge.
	type impAgg struct {
		class     core.Class
		relDir    string
		prod      bool // seen in at least one non-test file
		positions []core.Position
	}
	// per node relDir -> display specifier -> aggregate.
	nodes := make(map[string]map[string]*impAgg)

	for _, abs := range files {
		relFile, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to project root: %w", abs, err)
		}
		relFile = filepath.ToSlash(relFile)
		nodeDir := relDirOf(root, filepath.Dir(abs))
		fromDir := filepath.Dir(abs)
		isTest := isTestFile(relFile)

		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("reading %s: run depdog where its source files are readable: %w", relFile, err)
		}

		byImport := nodes[nodeDir]
		if byImport == nil {
			byImport = make(map[string]*impAgg)
			nodes[nodeDir] = byImport
		}

		for _, ref := range scan(data) {
			class, relDir, display, ok := classify(ref, fromDir, root)
			if !ok {
				continue
			}
			a := byImport[display]
			if a == nil {
				a = &impAgg{class: class, relDir: relDir}
				byImport[display] = a
			}
			a.positions = append(a.positions, core.Position{File: relFile, Line: ref.Line})
			if !isTest {
				a.prod = true
			}
		}
	}

	graph := &core.Graph{ModulePath: modPath}
	for nodeDir, byImport := range nodes {
		pkg := core.Package{ImportPath: importPathOf(modPath, nodeDir), RelDir: nodeDir}

		specs := make([]string, 0, len(byImport))
		for s := range byImport {
			specs = append(specs, s)
		}
		sort.Strings(specs)

		for _, s := range specs {
			a := byImport[s]
			sort.Slice(a.positions, func(i, j int) bool {
				pi, pj := a.positions[i], a.positions[j]
				return pi.File < pj.File || (pi.File == pj.File && pi.Line < pj.Line)
			})
			pkg.Imports = append(pkg.Imports, core.Import{
				Path:      s,
				Class:     a.class,
				RelDir:    a.relDir,
				TestOnly:  !a.prod,
				Positions: a.positions,
			})
		}
		graph.Packages = append(graph.Packages, pkg)
	}
	sort.Slice(graph.Packages, func(i, j int) bool {
		return graph.Packages[i].ImportPath < graph.Packages[j].ImportPath
	})
	return graph, nil
}

// projectRoot walks up from start looking for one of the Python project
// markers, in priority order. It mirrors the adapter registry's Markers so an
// explicitly-rooted Loader and auto-detection agree on the root.
func projectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	markers := []string{"pyproject.toml", "setup.py", "setup.cfg"}
	found := make([]string, len(markers))
	dir := abs
	for {
		for i, m := range markers {
			if found[i] == "" && isFile(filepath.Join(dir, m)) {
				found[i] = dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for i := range markers {
		if found[i] != "" {
			return found[i], nil
		}
	}
	return "", fmt.Errorf("no pyproject.toml, setup.py or setup.cfg found from %s upward; "+
		"run depdog from inside a Python project (the directory that holds pyproject.toml, setup.py or setup.cfg)", abs)
}

// discoverFiles walks root and collects .py/.pyi files, skipping virtualenvs,
// caches, and dotdirs. When patterns are supplied, only files whose
// module-relative path is under one of the (slash-normalized) patterns kept.
func discoverFiles(root string, patterns []string) ([]string, error) {
	var normPatterns []string
	for _, p := range patterns {
		np := filepath.ToSlash(filepath.Clean(p))
		np = strings.TrimPrefix(np, "./")
		if np == "." || np == "" {
			normPatterns = nil // whole project
			break
		}
		normPatterns = append(normPatterns, np)
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if skipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceExtSet[filepath.Ext(name)] {
			return nil
		}
		if len(normPatterns) > 0 {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if !matchesAnyPattern(rel, normPatterns) {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files) // deterministic scan order
	return files, nil
}

// skipDir reports whether a directory should be pruned from the walk: dotdirs,
// Python caches, and the conventional virtualenv / build directories that hold
// third-party code rather than first-party source.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "__pycache__", "venv", ".venv", "env", "node_modules",
		"build", "dist", "site-packages":
		return true
	}
	return strings.HasSuffix(name, ".egg-info")
}

// matchesAnyPattern reports whether the module-relative file rel is under any of
// the directory patterns (prefix match on a path-segment boundary).
func matchesAnyPattern(rel string, patterns []string) bool {
	for _, p := range patterns {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// importPathOf renders a node's display ImportPath as <ModulePath>/<RelDir>,
// with the root node using ModulePath alone — matching the Go/TS golden shape.
func importPathOf(modPath, relDir string) string {
	if relDir == "." || relDir == "" {
		return modPath
	}
	return modPath + "/" + relDir
}

// isTestFile marks conventional Python test files: test_*.py, *_test.py, or any
// file under a tests/ (or test/) directory.
func isTestFile(relFile string) bool {
	base := filepath.Base(relFile)
	stem := strings.TrimSuffix(strings.TrimSuffix(base, ".pyi"), ".py")
	if strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test") || stem == "conftest" {
		return true
	}
	for _, seg := range strings.Split(filepath.ToSlash(relFile), "/") {
		if seg == "tests" || seg == "test" {
			return true
		}
	}
	return false
}
