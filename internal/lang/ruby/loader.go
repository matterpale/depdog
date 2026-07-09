// Package ruby is the Ruby language adapter: it statically scans a project's
// .rb source files for require statements (require "x", require_relative
// "../x", and autoload :C, "x"), resolves them against the on-disk file layout,
// and builds the same directory-keyed *core.Graph the Go and TypeScript
// adapters produce. It runs no Ruby interpreter and needs no installed gems —
// it reads enough to find the edges, nothing more.
package ruby

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

// Loader loads the Ruby project rooted at (or above) Dir.
type Loader struct {
	Dir string
}

var _ lang.Loader = (*Loader)(nil)

// Load walks the project root, groups source files into one node per directory,
// scans each file for require statements, classifies them, and returns a
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

	// impAgg accumulates the occurrences of one (node, feature) edge.
	type impAgg struct {
		class     core.Class
		relDir    string
		prod      bool // seen in at least one non-test file
		positions []core.Position
	}
	// per node relDir -> display feature -> aggregate.
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

// projectRoot walks up from start looking for one of the Ruby project markers,
// in priority order. It mirrors the adapter registry's Markers so an
// explicitly-rooted Loader and auto-detection agree on the root. A `*.gemspec`
// in a directory also marks it as a root, matching the registry's intent.
func projectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	markers := []string{"Gemfile", ".ruby-version", "Rakefile"}
	found := make([]string, len(markers))
	gemspecDir := ""
	dir := abs
	for {
		for i, m := range markers {
			if found[i] == "" && isFile(filepath.Join(dir, m)) {
				found[i] = dir
			}
		}
		if gemspecDir == "" && dirHasGemspec(dir) {
			gemspecDir = dir
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
	if gemspecDir != "" {
		return gemspecDir, nil
	}
	return "", fmt.Errorf("no Gemfile, .ruby-version, Rakefile or *.gemspec found from %s upward; "+
		"run depdog from inside a Ruby project (the directory that holds a Gemfile, .ruby-version, Rakefile or *.gemspec)", abs)
}

// dirHasGemspec reports whether dir directly contains a *.gemspec file.
func dirHasGemspec(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".gemspec") {
			return true
		}
	}
	return false
}

// discoverFiles walks root and collects .rb files, skipping vendored code,
// dotdirs, and scratch directories. When patterns are supplied, only files
// whose module-relative path is under one of the (slash-normalized) patterns are
// kept.
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
		if filepath.Ext(name) != ".rb" {
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

// skipDir reports whether a directory should be pruned from the walk: dotdirs
// and the conventional vendor / scratch directories that hold third-party or
// throwaway code rather than first-party source.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "vendor", "tmp", "node_modules", "log", "coverage":
		return true
	}
	return false
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

// isTestFile marks conventional Ruby test/spec files: *_test.rb, *_spec.rb, or
// any file under a spec/ or test/ directory.
func isTestFile(relFile string) bool {
	base := filepath.Base(relFile)
	stem := strings.TrimSuffix(base, ".rb")
	if strings.HasSuffix(stem, "_test") || strings.HasSuffix(stem, "_spec") {
		return true
	}
	for _, seg := range strings.Split(filepath.ToSlash(relFile), "/") {
		if seg == "spec" || seg == "test" {
			return true
		}
	}
	return false
}
