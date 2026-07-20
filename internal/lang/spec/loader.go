package spec

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

// Loader is the declarative adapter: it runs a Spec's lexer + surface extractor +
// resolver over a project's source files and builds the same directory-keyed
// *core.Graph a hand-written adapter produces. Nothing language-specific reaches
// core — the graph is language-neutral, so core and the reporters are untouched.
type Loader struct {
	Spec *Spec
	Dir  string
}

var _ lang.Loader = (*Loader)(nil)

// Load resolves the project root, discovers source files, scans and classifies
// each file's imports, and returns a deterministic (sorted) graph.
func (l *Loader) Load(_ context.Context, patterns ...string) (*core.Graph, error) {
	root, err := l.projectRoot()
	if err != nil {
		return nil, err
	}
	modPath := l.moduleLabel(root)

	files, err := l.discoverFiles(root, patterns)
	if err != nil {
		return nil, err
	}

	switch l.Spec.Resolve.mode() {
	case ModePath:
		return l.loadPath(root, modPath, files)
	default:
		// Name-index resolution (C#, Elm) lands in M5; path mode is the only
		// resolver shipped so far.
		return nil, fmt.Errorf("adapter %q: resolve.mode %q is not yet supported", l.Spec.Name, l.Spec.Resolve.mode())
	}
}

// loadPath builds the graph using path-mode resolution: each file's imports are
// classified independently against the on-disk layout.
func (l *Loader) loadPath(root, modPath string, files []string) (*core.Graph, error) {
	rs := newResolver(l.Spec, root)
	gb := newGraphBuilder()
	dropSelf := l.Spec.Resolve.DropSelfEdges

	for _, abs := range files {
		relFile, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to project root: %w", abs, err)
		}
		relFile = filepath.ToSlash(relFile)
		nodeDir := relDirOf(root, filepath.Dir(abs))
		fromDir := filepath.Dir(abs)
		isTest := l.isTestFile(relFile)

		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("reading %s: run depdog where its source files are readable: %w", relFile, err)
		}

		for _, r := range extract(l.Spec, data).imports {
			c := rs.classify(r, fromDir)
			if !c.ok {
				continue
			}
			if dropSelf && c.class == core.ClassInModule && c.relDir == nodeDir {
				continue // a self-edge carries no direction
			}
			gb.add(nodeDir, c, core.Position{File: relFile, Line: r.Line}, isTest)
		}
		gb.touch(nodeDir) // a file with no imports still contributes its node
	}

	return gb.graph(modPath), nil
}

// projectRoot walks up from Dir to the nearest directory holding one of the
// spec's markers, in priority order — an earlier marker found anywhere beats a
// later one found nearer. A marker containing '*' is matched as a glob against a
// directory's entries (e.g. "*.gemspec").
func (l *Loader) projectRoot() (string, error) {
	abs, err := filepath.Abs(l.Dir)
	if err != nil {
		return "", err
	}
	markers := l.Spec.Markers
	found := make([]string, len(markers))
	for d := abs; ; {
		for i, m := range markers {
			if found[i] == "" && markerMatches(d, m) {
				found[i] = d
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	for i := range markers {
		if found[i] != "" {
			return found[i], nil
		}
	}
	return "", fmt.Errorf("no %s found from %s upward; run depdog from inside a %s project (a directory holding one of those markers)",
		strings.Join(markers, ", "), abs, l.Spec.Name)
}

// markerMatches reports whether dir holds marker: an exact file when marker is a
// plain name, or any non-directory entry matching the glob when marker contains '*'.
func markerMatches(dir, marker string) bool {
	if strings.Contains(marker, "*") {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if ok, _ := filepath.Match(marker, e.Name()); ok {
				return true
			}
		}
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, marker))
	return err == nil && !fi.IsDir()
}

// discoverFiles walks root and collects files with one of the spec's extensions,
// pruning dotdirs and the spec's skipDirs. With patterns, only files whose
// module-relative path is under one of the (slash-normalized) patterns are kept.
func (l *Loader) discoverFiles(root string, patterns []string) ([]string, error) {
	var normPatterns []string
	for _, p := range patterns {
		np := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(p)), "./")
		if np == "." || np == "" {
			normPatterns = nil // whole project
			break
		}
		normPatterns = append(normPatterns, np)
	}

	skip := sliceSet(l.Spec.SkipDirs)
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
			if strings.HasPrefix(name, ".") || skip[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if !l.hasExtension(name) {
			return nil
		}
		if len(normPatterns) > 0 {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil
			}
			if !matchesAnyPattern(filepath.ToSlash(rel), normPatterns) {
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

func (l *Loader) hasExtension(name string) bool {
	ext := filepath.Ext(name)
	for _, e := range l.Spec.Extensions {
		if ext == e {
			return true
		}
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

// isTestFile reports whether a module-relative file is a test file per the spec's
// tests config: a stem suffix (_test, _spec) or a path segment (spec, test).
func (l *Loader) isTestFile(relFile string) bool {
	base := filepath.Base(relFile)
	stem := base
	if ext := filepath.Ext(base); ext != "" {
		stem = strings.TrimSuffix(base, ext)
	}
	for _, suf := range l.Spec.Tests.StemSuffixes {
		if strings.HasSuffix(stem, suf) {
			return true
		}
	}
	if len(l.Spec.Tests.Dirs) > 0 {
		dirSet := sliceSet(l.Spec.Tests.Dirs)
		for _, seg := range strings.Split(filepath.ToSlash(relFile), "/") {
			if dirSet[seg] {
				return true
			}
		}
	}
	return false
}

// moduleLabel derives the graph's ModulePath: a name read from a manifest
// (Module.FromFile, e.g. a gemspec's spec.name) when configured and found, else
// the project root's directory basename.
func (l *Loader) moduleLabel(root string) string {
	if ff := l.Spec.Module.FromFile; ff != nil {
		if name := nameFromManifest(root, ff); name != "" {
			return name
		}
	}
	return filepath.Base(root)
}

// nameFromManifest finds the first file in root matching ff.Glob and scans it for
// a `<recv>.<key> = "value"` assignment, ignoring comment lines. It is a tiny
// hand-rolled key lookup — enough to find one name= assignment without evaluating
// the manifest language (mirrors ruby's nameFromGemspec).
func nameFromManifest(root string, ff *ModuleFromFile) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var manifests []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ok, _ := filepath.Match(ff.Glob, e.Name()); ok {
			manifests = append(manifests, e.Name())
		}
	}
	sort.Strings(manifests) // deterministic when a project has more than one
	comment := ff.CommentPrefix
	if comment == "" {
		comment = "#"
	}
	for _, m := range manifests {
		if name := nameFromManifestFile(filepath.Join(root, m), ff.Key, comment); name != "" {
			return name
		}
	}
	return ""
}

// nameFromManifestFile scans one manifest for a `<recv>.<key> = "..."` line,
// ignoring comment lines.
func nameFromManifestFile(path, key, comment string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, comment) {
			continue
		}
		lhs, rhs, ok := splitAssign(line)
		if !ok {
			continue
		}
		if lastSegment(lhs) == key {
			if v := unquote(rhs); v != "" {
				return v
			}
		}
	}
	return ""
}

// splitAssign splits `lhs = rhs` on the first top-level `=`, ignoring ==, =>, <=,
// >=, != so only a real assignment matches.
func splitAssign(line string) (lhs, rhs string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] != '=' {
			continue
		}
		if i+1 < len(line) && (line[i+1] == '=' || line[i+1] == '>') {
			i++
			continue
		}
		if i > 0 && (line[i-1] == '=' || line[i-1] == '<' || line[i-1] == '>' || line[i-1] == '!') {
			continue
		}
		return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
	}
	return "", "", false
}

// lastSegment returns the text after the final dot of a dotted key (spec.name -> name).
func lastSegment(key string) string {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// unquote strips one pair of surrounding quotes and any trailing content (a
// `.freeze` chain, a comment) after the closing quote.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return ""
	}
	q := s[0]
	if q != '"' && q != '\'' {
		return ""
	}
	if end := strings.IndexByte(s[1:], q); end >= 0 {
		return s[1 : 1+end]
	}
	return ""
}

// importPathOf renders a node's display ImportPath as <ModulePath>/<RelDir>, with
// the root node using ModulePath alone — matching every adapter's golden shape.
func importPathOf(modPath, relDir string) string {
	if relDir == "." || relDir == "" {
		return modPath
	}
	return modPath + "/" + relDir
}

// --- graph assembly (shared by every resolution mode) ---

// edgeAgg accumulates the occurrences of one (node, display) edge.
type edgeAgg struct {
	class     core.Class
	relDir    string
	prod      bool // seen in at least one non-test file
	positions []core.Position
}

// graphBuilder accumulates edges per node and emits a deterministic graph.
type graphBuilder struct {
	nodes map[string]map[string]*edgeAgg
}

func newGraphBuilder() *graphBuilder {
	return &graphBuilder{nodes: make(map[string]map[string]*edgeAgg)}
}

// touch registers a node directory even if it has no edges, so a source file with
// no imports still appears as a graph node.
func (gb *graphBuilder) touch(nodeDir string) {
	if gb.nodes[nodeDir] == nil {
		gb.nodes[nodeDir] = make(map[string]*edgeAgg)
	}
}

// add records one classified edge occurrence from a (test or production) file.
func (gb *graphBuilder) add(nodeDir string, c classified, pos core.Position, isTest bool) {
	byImport := gb.nodes[nodeDir]
	if byImport == nil {
		byImport = make(map[string]*edgeAgg)
		gb.nodes[nodeDir] = byImport
	}
	a := byImport[c.display]
	if a == nil {
		a = &edgeAgg{class: c.class, relDir: c.relDir}
		byImport[c.display] = a
	}
	a.positions = append(a.positions, pos)
	if !isTest {
		a.prod = true
	}
}

// graph emits the accumulated nodes as a sorted *core.Graph.
func (gb *graphBuilder) graph(modPath string) *core.Graph {
	g := &core.Graph{ModulePath: modPath}
	for nodeDir, byImport := range gb.nodes {
		pkg := core.Package{ImportPath: importPathOf(modPath, nodeDir), RelDir: nodeDir}

		displays := make([]string, 0, len(byImport))
		for s := range byImport {
			displays = append(displays, s)
		}
		sort.Strings(displays)

		for _, s := range displays {
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
		g.Packages = append(g.Packages, pkg)
	}
	sort.Slice(g.Packages, func(i, j int) bool {
		return g.Packages[i].ImportPath < g.Packages[j].ImportPath
	})
	return g
}
