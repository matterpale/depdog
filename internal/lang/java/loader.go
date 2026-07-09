// Package java is the Java language adapter: it statically scans a project's
// .java source files for their `package` declaration and `import` statements
// (import a.b.C;, import static a.b.C.m;, and on-demand import a.b.*;), resolves
// each import against the set of packages the project itself declares, and
// builds the same directory-keyed *core.Graph the Go, TypeScript, Python and
// Rust adapters produce. It runs no JVM and needs no build tool — it reads
// enough to find the edges, nothing more.
package java

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

// Loader loads the Java project rooted at (or above) Dir.
type Loader struct {
	Dir string
}

var _ lang.Loader = (*Loader)(nil)

// markers are the project-root marker files, in priority order — mirroring the
// adapter registry so an explicitly-rooted Loader and auto-detection agree.
var markers = []string{"pom.xml", "build.gradle", "build.gradle.kts"}

// Load walks the project root, groups source files into one node per package
// directory, scans each file for import statements, classifies them against the
// project's declared packages, and returns a deterministic *core.Graph.
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

	// First pass: scan every file once, remembering its package declaration and
	// imports, and build the declared-package -> node-dir map that
	// classification consults. A node dir is a file's source-root-relative
	// directory (e.g. src/main/java/com/example/domain).
	type scanned struct {
		relFile string
		nodeDir string
		isTest  bool
		res     scanResult
	}
	all := make([]scanned, 0, len(files))
	declared := make(map[string]string) // dotted package -> node dir

	for _, abs := range files {
		relFile, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to project root: %w", abs, err)
		}
		relFile = filepath.ToSlash(relFile)

		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("reading %s: run depdog where its source files are readable: %w", relFile, err)
		}

		nodeDir := relDirOf(root, filepath.Dir(abs))
		res := scan(data)
		all = append(all, scanned{relFile: relFile, nodeDir: nodeDir, isTest: isTestFile(relFile), res: res})
		if res.pkg != "" {
			// A package can be split across source roots (src/main + src/test).
			// The first node dir seen (files are sorted) wins deterministically;
			// production source sorts before test source under the standard
			// layout, so edges resolve to the main directory.
			if _, seen := declared[res.pkg]; !seen {
				declared[res.pkg] = nodeDir
			}
		}
	}

	// Second pass: classify each file's imports against the declared map and
	// aggregate them per node.

	// impAgg accumulates the occurrences of one (node, specifier) edge.
	type impAgg struct {
		class     core.Class
		relDir    string
		prod      bool // seen in at least one non-test file
		positions []core.Position
	}
	// per node relDir -> display specifier -> aggregate.
	nodes := make(map[string]map[string]*impAgg)

	for _, sc := range all {
		byImport := nodes[sc.nodeDir]
		if byImport == nil {
			byImport = make(map[string]*impAgg)
			nodes[sc.nodeDir] = byImport
		}

		for _, ref := range sc.res.imports {
			class, relDir, display, ok := classify(ref, declared)
			if !ok {
				continue
			}
			// A self-edge (an import of the importing package itself, e.g. a
			// wildcard or a sibling type) carries no direction, so drop it.
			if class == core.ClassInModule && relDir == sc.nodeDir {
				continue
			}
			a := byImport[display]
			if a == nil {
				a = &impAgg{class: class, relDir: relDir}
				byImport[display] = a
			}
			a.positions = append(a.positions, core.Position{File: sc.relFile, Line: ref.Line})
			if !sc.isTest {
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

// projectRoot walks up from start looking for one of the Java project markers,
// in priority order. It mirrors the adapter registry's Markers so an
// explicitly-rooted Loader and auto-detection agree on the root.
func projectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
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
	return "", fmt.Errorf("no %s found from %s upward; "+
		"run depdog from inside a Java project (the directory that holds %s)",
		strings.Join(markers, ", "), abs, strings.Join(markers, ", "))
}

// discoverFiles walks root and collects .java files, skipping build output and
// dotdirs. When patterns are supplied, only files whose module-relative path is
// under one of the (slash-normalized) patterns are kept.
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
		if filepath.Ext(name) != ".java" {
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
// and the conventional Maven/Gradle build-output directories, which hold
// compiled artifacts rather than first-party source.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "target", "build", "out", "bin", "node_modules":
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
// with the root node using ModulePath alone — matching the Go/TS/Py/Rust golden
// shape.
func importPathOf(modPath, relDir string) string {
	if relDir == "." || relDir == "" {
		return modPath
	}
	return modPath + "/" + relDir
}

// isTestFile marks conventional Java test sources: any file under a src/test/
// tree (the standard Maven/Gradle test source root).
func isTestFile(relFile string) bool {
	rel := filepath.ToSlash(relFile)
	return rel == "src/test" ||
		strings.HasPrefix(rel, "src/test/") ||
		strings.Contains(rel, "/src/test/")
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
