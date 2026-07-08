// Package typescript is the TypeScript/JavaScript language adapter: it
// statically scans a project's source files for module specifiers (import,
// export-from, dynamic import, and require), resolves them against on-disk
// files and tsconfig path aliases, and builds the same directory-keyed
// *core.Graph the Go adapter produces. It runs no Node process and needs no
// type-checker — it reads enough to find the edges, nothing more.
package typescript

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// Loader loads the TS/JS project rooted at (or above) Dir.
type Loader struct {
	Dir string
}

var _ lang.Loader = (*Loader)(nil)

// sourceExtSet mirrors the extensions we scan (a superset of what resolution
// appends, minus .d.ts which declares but rarely originates edges).
var sourceExtSet = map[string]bool{
	".ts": true, ".tsx": true, ".mts": true, ".cts": true,
	".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
}

// Load walks the project root, groups source files into one node per
// directory, scans each file for import specifiers, classifies them, and
// returns a deterministic *core.Graph.
func (l *Loader) Load(_ context.Context, patterns ...string) (*core.Graph, error) {
	root, err := projectRoot(l.Dir)
	if err != nil {
		return nil, err
	}
	cfg, err := loadTSConfig(root)
	if err != nil {
		return nil, err
	}
	modPath := packageName(root)

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
	// per node relDir -> specifier -> aggregate.
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
			return nil, fmt.Errorf("reading %s: %w", relFile, err)
		}

		byImport := nodes[nodeDir]
		if byImport == nil {
			byImport = make(map[string]*impAgg)
			nodes[nodeDir] = byImport
		}

		for _, spec := range scan(data) {
			class, relDir, ok := classify(spec.Raw, fromDir, root, cfg)
			if !ok {
				continue
			}
			a := byImport[spec.Raw]
			if a == nil {
				a = &impAgg{class: class, relDir: relDir}
				byImport[spec.Raw] = a
			}
			a.positions = append(a.positions, core.Position{File: relFile, Line: spec.Line})
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

// projectRoot walks up from start looking for a tsconfig.json, then (failing
// that) a package.json. The nearest tsconfig wins over a package.json at the
// same or a lower level, matching the design's marker precedence.
func projectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	firstPkgJSON := ""
	dir := abs
	for {
		if isFile(filepath.Join(dir, "tsconfig.json")) {
			return dir, nil
		}
		if firstPkgJSON == "" && isFile(filepath.Join(dir, "package.json")) {
			firstPkgJSON = dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if firstPkgJSON != "" {
		return firstPkgJSON, nil
	}
	return "", fmt.Errorf("no tsconfig.json or package.json found from %s upward; "+
		"run depdog from inside a TypeScript/JavaScript project (the directory that holds tsconfig.json or package.json)", abs)
}

// discoverFiles walks root and collects source files, skipping node_modules
// and dotdirs. When patterns are supplied, only files whose module-relative
// path is under one of the (slash-normalized) patterns are kept.
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
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking %s: %w", path, err)
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if name == "node_modules" || (len(name) > 1 && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceExtSet[strings.ToLower(filepath.Ext(name))] {
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

// matchesAnyPattern reports whether the module-relative file rel is under any
// of the directory patterns (prefix match on a path-segment boundary).
func matchesAnyPattern(rel string, patterns []string) bool {
	for _, p := range patterns {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// importPathOf renders a node's display ImportPath as <ModulePath>/<RelDir>,
// with the root node using ModulePath alone — matching the Go golden shape.
func importPathOf(modPath, relDir string) string {
	if relDir == "." || relDir == "" {
		return modPath
	}
	return modPath + "/" + relDir
}

// isTestFile marks conventional TS/JS test files: *.test.*, *.spec.*, or any
// file under a __tests__/ directory.
func isTestFile(relFile string) bool {
	base := filepath.Base(relFile)
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	for _, seg := range strings.Split(filepath.ToSlash(relFile), "/") {
		if seg == "__tests__" {
			return true
		}
	}
	return false
}
