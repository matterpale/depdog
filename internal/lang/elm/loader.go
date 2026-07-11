// Package elm is the Elm language adapter: it statically scans a project's .elm
// source files for their `module`/`port module`/`effect module` declaration and
// `import` statements, resolves each import against the modules the project
// itself ships under its elm.json source-directories, and builds the same
// directory-keyed *core.Graph the Go, TypeScript, Python, Rust, Java, Kotlin and
// Scala adapters produce.
//
// Elm resolves by MODULE NAME, not by import path: a module `Foo.Bar` is a
// project module when a file `<srcDir>/Foo/Bar.elm` exists under one of the
// source-directories, and the graph node for a file is the DIRECTORY that file
// lives in. elm.json is read with encoding/json only — depdog runs no elm
// toolchain and needs no installed packages.
package elm

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

// Loader loads the Elm project rooted at (or above) Dir.
type Loader struct {
	Dir string
}

var _ lang.Loader = (*Loader)(nil)

// marker is the project-root marker file — mirroring the adapter registry so an
// explicitly-rooted Loader and auto-detection agree on the root.
const marker = "elm.json"

// Load walks the project root, groups source files into one node per directory,
// scans each file for import statements, classifies them against the modules the
// project ships under its source-directories, and returns a deterministic
// *core.Graph.
func (l *Loader) Load(_ context.Context, patterns ...string) (*core.Graph, error) {
	root, err := projectRoot(l.Dir)
	if err != nil {
		return nil, err
	}
	sourceDirs, modPath, err := readProject(root)
	if err != nil {
		return nil, err
	}

	files, err := discoverFiles(root, sourceDirs, patterns)
	if err != nil {
		return nil, err
	}

	// First pass: scan every file once, remembering its imports and node dir, and
	// build the declared-module -> node-dir map that classification consults. A
	// module's name is derived from its file path under a source directory
	// (src/Foo/Bar.elm under src -> module Foo.Bar), which is exactly Elm's own
	// module-resolution rule.
	type scanned struct {
		relFile string
		nodeDir string
		res     scanResult
	}
	all := make([]scanned, 0, len(files))
	declared := make(map[string]string) // dotted module name -> node dir

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
		all = append(all, scanned{relFile: relFile, nodeDir: nodeDir, res: res})

		// Resolve the module name from the file's location under a source dir. The
		// on-disk path is authoritative (Elm resolves by path); the file's own
		// `module` header is not needed for resolution and a mismatched header would
		// not compile in real Elm anyway.
		for _, sd := range sourceDirs {
			if name, ok := moduleFromFile(sd, relFile); ok {
				if _, seen := declared[name]; !seen {
					declared[name] = nodeDir
				}
				break // a file belongs to exactly one source dir (first match wins)
			}
		}
	}

	// Second pass: classify each file's imports against the declared map and
	// aggregate them per node.

	// impAgg accumulates the occurrences of one (node, module) edge.
	type impAgg struct {
		class     core.Class
		relDir    string
		positions []core.Position
	}
	// per node relDir -> imported module -> aggregate.
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
			// A self-edge (an import resolving to the importing file's own
			// directory) carries no direction, so drop it.
			if class == core.ClassInModule && relDir == sc.nodeDir {
				continue
			}
			a := byImport[display]
			if a == nil {
				a = &impAgg{class: class, relDir: relDir}
				byImport[display] = a
			}
			a.positions = append(a.positions, core.Position{File: sc.relFile, Line: ref.Line})
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

// projectRoot walks up from start looking for the Elm project marker (elm.json).
// It mirrors the adapter registry's Markers so an explicitly-rooted Loader and
// auto-detection agree on the root.
func projectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if isFile(filepath.Join(dir, marker)) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no %s found from %s upward; "+
		"run depdog from inside an Elm project (the directory that holds %s)", marker, abs, marker)
}

// discoverFiles walks each source directory under root and collects .elm files,
// skipping build output and dotdirs. When patterns are supplied, only files
// whose project-relative path is under one of the (slash-normalized) patterns
// are kept.
func discoverFiles(root string, sourceDirs, patterns []string) ([]string, error) {
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

	seen := make(map[string]bool)
	var files []string
	for _, sd := range sourceDirs {
		base := root
		if sd != "." {
			base = filepath.Join(root, filepath.FromSlash(sd))
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				// A configured source dir that does not exist on disk is not fatal —
				// the project may simply not have created it yet.
				if os.IsNotExist(walkErr) {
					return filepath.SkipDir
				}
				return walkErr
			}
			name := d.Name()
			if d.IsDir() {
				if path == base {
					return nil
				}
				if skipDir(name) {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(name) != ".elm" {
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
			if !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files) // deterministic scan order
	return files, nil
}

// skipDir reports whether a directory should be pruned from the walk: dotdirs
// and the conventional Elm build-output / dependency directories, which hold
// compiled artifacts or third-party code rather than first-party source.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "elm-stuff", "node_modules":
		// elm-stuff/ holds the compiled artifacts and fetched dependency sources
		// the elm toolchain writes; node_modules is a JS toolchain artifact.
		return true
	}
	return false
}

// matchesAnyPattern reports whether the project-relative file rel is under any of
// the directory patterns (prefix match on a path-segment boundary).
func matchesAnyPattern(rel string, patterns []string) bool {
	for _, p := range patterns {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// importPathOf renders a node's display ImportPath as <ModulePath>/<RelDir>, with
// the root node using ModulePath alone — matching the shared golden shape.
func importPathOf(modPath, relDir string) string {
	if relDir == "." || relDir == "" {
		return modPath
	}
	return modPath + "/" + relDir
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
