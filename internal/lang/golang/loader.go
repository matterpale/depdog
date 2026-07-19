// Package golang is the Go language adapter: it loads a module's package
// graph via go/packages metadata (no type-checking) and resolves import
// positions with a lightweight parse of the import declarations.
package golang

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// Loader loads the Go module rooted at Dir.
type Loader struct {
	Dir string
}

var _ lang.Loader = (*Loader)(nil)

func (l *Loader) Load(ctx context.Context, patterns ...string) (*core.Graph, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	modPath, err := modulePath(filepath.Join(l.Dir, "go.mod"))
	if err != nil {
		return nil, err
	}

	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps | packages.NeedModule,
		Dir:     l.Dir,
		Tests:   true,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}
	// Per-package load errors (an unresolved import, a missing dependency, code
	// that is mid-refactor) do NOT abort: `go list` still returns the packages it
	// could resolve, and imports it could not resolve fall through to
	// classifyFallback's path heuristic below. We degrade to a best-effort graph
	// and record a warning so the "works on code that doesn't compile yet"
	// property holds for Go too, matching the pure-static adapters. A hard error
	// from packages.Load itself (not-a-module, no toolchain) is still fatal above.
	var loadErrs []string
	totalErrs := 0
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			totalErrs++ // the true count, so the warning doesn't report the cap as the total
			if len(loadErrs) < 5 {
				loadErrs = append(loadErrs, e.Error())
			}
		}
	})

	// Classification index over everything reachable, so imports can be
	// bucketed without a second load.
	type target struct {
		class  core.Class
		relDir string
	}
	index := make(map[string]target)
	packages.Visit(pkgs, func(p *packages.Package) bool {
		if _, ok := index[p.PkgPath]; ok {
			return true
		}
		t := target{class: core.ClassExternal}
		switch {
		case p.Module != nil:
			if p.Module.Path == modPath {
				t.class = core.ClassInModule
				t.relDir = relDir(modPath, p.PkgPath)
			}
			// else: a dependency module — external (the init default).
		default:
			// Module-less: the standard library, or — in degraded mode, when
			// go list could not attribute the package — an unresolved import.
			// classifyFallback's path heuristic keeps an unresolved in-module
			// path from being mislabeled std (std is never under the module
			// path) while still classifying real std packages as std.
			t.class, t.relDir = classifyFallback(modPath, p.PkgPath)
		}
		index[p.PkgPath] = t
		return true
	}, nil)

	// Merge test variants: the plain package, its test-augmented variant
	// and the external _test package all describe the same directory.
	type entry struct {
		importPath string
		plain      bool // importPath came from the non-test variant
		relDir     string
		files      map[string]bool // abs file path -> is a _test.go file
	}
	entries := make(map[string]*entry) // keyed by relDir
	modDir := ""
	for _, p := range pkgs {
		if strings.HasSuffix(p.PkgPath, ".test") {
			continue // synthesized test-main package, lives in the build cache
		}
		if p.Module == nil || p.Module.Path != modPath {
			continue
		}
		if modDir == "" && p.Module.Dir != "" {
			modDir = p.Module.Dir
		}
		base := p.PkgPath
		variant := p.ID != p.PkgPath
		if variant && strings.HasSuffix(base, "_test") {
			base = strings.TrimSuffix(base, "_test")
		}
		dir := relDir(modPath, base)
		e := entries[dir]
		if e == nil {
			e = &entry{relDir: dir, files: make(map[string]bool)}
			entries[dir] = e
		}
		if !variant {
			e.importPath, e.plain = base, true
		} else if !e.plain && e.importPath == "" {
			e.importPath = base
		}
		for _, f := range p.GoFiles {
			e.files[f] = strings.HasSuffix(filepath.Base(f), "_test.go")
		}
	}
	if modDir == "" {
		if modDir, err = filepath.Abs(l.Dir); err != nil {
			return nil, err
		}
	}

	type impAgg struct {
		prod      bool
		positions []core.Position
	}
	fset := token.NewFileSet()
	graph := &core.Graph{ModulePath: modPath}
	if totalErrs > 0 {
		graph.LoadWarnings = []string{degradedWarning(totalErrs, loadErrs)}
	}
	for _, e := range entries {
		imports := make(map[string]*impAgg)
		for file, isTest := range e.files {
			f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
			if err != nil {
				return nil, fmt.Errorf("parsing %s: %w", file, err)
			}
			relFile := file
			if r, err := filepath.Rel(modDir, file); err == nil {
				relFile = filepath.ToSlash(r)
			}
			for _, spec := range f.Imports {
				ip, err := strconv.Unquote(spec.Path.Value)
				if err != nil || ip == "C" { // "C" is cgo's pseudo-import
					continue
				}
				a := imports[ip]
				if a == nil {
					a = &impAgg{}
					imports[ip] = a
				}
				a.positions = append(a.positions, core.Position{File: relFile, Line: fset.Position(spec.Pos()).Line})
				if !isTest {
					a.prod = true
				}
			}
		}

		pkg := core.Package{ImportPath: e.importPath, RelDir: e.relDir}
		paths := make([]string, 0, len(imports))
		for ip := range imports {
			paths = append(paths, ip)
		}
		sort.Strings(paths)
		for _, ip := range paths {
			a := imports[ip]
			t, ok := index[ip]
			if !ok {
				t.class, t.relDir = classifyFallback(modPath, ip)
			}
			sort.Slice(a.positions, func(i, j int) bool {
				pi, pj := a.positions[i], a.positions[j]
				return pi.File < pj.File || (pi.File == pj.File && pi.Line < pj.Line)
			})
			pkg.Imports = append(pkg.Imports, core.Import{
				Path:      ip,
				Class:     t.class,
				RelDir:    t.relDir,
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

func relDir(modPath, pkgPath string) string {
	if pkgPath == modPath {
		return "."
	}
	return strings.TrimPrefix(pkgPath, modPath+"/")
}

// degradedWarning describes the best-effort fallback the loader takes when
// `go list` cannot fully resolve the module: packages it failed to resolve are
// bucketed by classifyFallback's path heuristic (std vs external) instead of the
// toolchain's exact module metadata, so build-tag / replace / vendor resolution
// may be off. It is human-actionable — it names the fix.
func degradedWarning(total int, sample []string) string {
	noun := "error"
	if total != 1 {
		noun = "errors"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "go list could not fully resolve this module (%d package load %s); "+
		"import classification is approximate — unresolved packages are bucketed by a path "+
		"heuristic (std vs external) and build-tag/replace/vendor resolution may be off. "+
		"Fix: run `go mod download` (or `go mod tidy`) and re-run for exact results.",
		total, noun)
	if len(sample) < total {
		fmt.Fprintf(&b, "\n  first %d load errors:", len(sample))
	} else {
		b.WriteString("\n  load errors:")
	}
	for _, e := range sample {
		b.WriteString("\n    " + e)
	}
	return b.String()
}

// classifyFallback covers import paths the metadata load did not resolve;
// std-lib paths have no dot in their first segment.
func classifyFallback(modPath, ip string) (core.Class, string) {
	switch {
	case ip == modPath || strings.HasPrefix(ip, modPath+"/"):
		return core.ClassInModule, relDir(modPath, ip)
	case strings.Contains(strings.SplitN(ip, "/", 2)[0], "."):
		return core.ClassExternal, ""
	default:
		return core.ClassStd, ""
	}
}

func modulePath(gomod string) (string, error) {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return "", fmt.Errorf("reading module info: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	return "", fmt.Errorf("no module directive in %s", gomod)
}
