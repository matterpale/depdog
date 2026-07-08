package core

import "sort"

// ImportView is one outgoing edge, resolved for display: its class and, for an
// in-module edge, the component the target belongs to.
type ImportView struct {
	Path      string
	Class     Class
	Component string // target component for in-module imports; "" otherwise
	TestOnly  bool
}

// PackageView is the per-package navigation data the TUI's Packages screen and
// (later) `explain` present: a package's component, its classified outgoing
// imports, and the in-module packages that import it.
type PackageView struct {
	ImportPath string
	Component  string // "" means unassigned
	Imports    []ImportView
	Importers  []string
	Boundaries []PackageBoundary // boundary members this package belongs to; sorted by boundary
}

// PackageBoundary records one boundary a package participates in: which member
// (if any) claims it and whether that boundary is sealed. Member is "" when the
// package sits in no member of the boundary (ungrouped for it).
type PackageBoundary struct {
	Boundary string
	Member   string // member label (component name or glob); "" if not a member
	Sealed   bool
}

// BuildPackageViews turns the graph into per-package views, resolving each
// package and each in-module import to its component and inverting the graph to
// find importers. Skipped packages (and edges into them) are omitted, matching
// what Evaluate judges. Output is deterministic: packages sorted by import
// path, importers sorted.
func BuildPackageViews(g *Graph, rs *RuleSet) ([]PackageView, error) {
	cache := make(map[string]string, len(g.Packages))
	assign := func(relDir string) (string, error) {
		if c, ok := cache[relDir]; ok {
			return c, nil
		}
		c, err := rs.AssignComponent(relDir)
		if err != nil {
			return "", err
		}
		cache[relDir] = c
		return c, nil
	}

	importers := make(map[string]map[string]bool)
	views := make([]PackageView, 0, len(g.Packages))
	for _, p := range g.Packages {
		if rs.Skipped(p.RelDir) {
			continue
		}
		comp, err := assign(p.RelDir)
		if err != nil {
			return nil, err
		}
		pv := PackageView{ImportPath: p.ImportPath, Component: comp}
		if len(rs.Boundaries) > 0 {
			mem, merr := rs.BoundaryMembership(p.RelDir)
			if merr != nil {
				return nil, merr
			}
			for bi, mi := range mem {
				if mi < 0 {
					continue // not a member of this boundary
				}
				b := &rs.Boundaries[bi]
				pv.Boundaries = append(pv.Boundaries, PackageBoundary{
					Boundary: b.Name, Member: b.Members[mi].Label, Sealed: b.Sealed,
				})
			}
		}
		for _, imp := range p.Imports {
			if imp.Class == ClassInModule && rs.Skipped(imp.RelDir) {
				continue
			}
			iv := ImportView{Path: imp.Path, Class: imp.Class, TestOnly: imp.TestOnly}
			if imp.Class == ClassInModule {
				if iv.Component, err = assign(imp.RelDir); err != nil {
					return nil, err
				}
				if importers[imp.Path] == nil {
					importers[imp.Path] = make(map[string]bool)
				}
				importers[imp.Path][p.ImportPath] = true
			}
			pv.Imports = append(pv.Imports, iv)
		}
		views = append(views, pv)
	}

	for i := range views {
		set := importers[views[i].ImportPath]
		if len(set) == 0 {
			continue
		}
		imps := make([]string, 0, len(set))
		for s := range set {
			imps = append(imps, s)
		}
		sort.Strings(imps)
		views[i].Importers = imps
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ImportPath < views[j].ImportPath })
	return views, nil
}
