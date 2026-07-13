package core

import (
	"path"
	"sort"
	"strings"
)

// Cross-unit governance: a depdog.work.yaml at the repo root declares named
// units (root-relative directory subtrees, each usually a language project of
// its own) and governs the dependency edges *between* them with the same
// allow/deny + boundary vocabulary components use — units are the members of a
// coarse super-graph. Within one language an edge is import-path →
// import-path; across languages there is no shared symbol space, so cross-unit
// edges are detected structurally, from each unit's own scanned graph:
//
//   - path channel: an in-module import whose resolved directory lands inside
//     another unit's tree (relative or alias imports crossing subtrees);
//   - identity channel: an external import whose path matches another unit's
//     declared import identity (its go.mod module path, package.json name, …)
//     by segment prefix.
//
// The verdicts reuse Evaluate on a synthetic rule set (component per unit,
// pattern = the unit's dir), then carry cross-unit reason kinds so every
// surface can render them distinctly.

// Cross-unit reason kinds. They parallel the intra-unit kinds: ReasonCrossUnit
// is an ordinary unit rule/stance denial, the boundary pair mirrors
// ReasonBoundary/ReasonBoundarySealed with units as members, and
// ReasonCrossUnitSurface is an edge allowed at unit level whose target
// sub-path violates the target unit's declared surface.
const (
	ReasonCrossUnit               ReasonKind = "cross-unit"
	ReasonCrossUnitBoundary       ReasonKind = "cross-unit-boundary"
	ReasonCrossUnitBoundarySealed ReasonKind = "cross-unit-boundary-sealed"
	ReasonCrossUnitSurface        ReasonKind = "cross-unit-surface"
)

// WorkUnit is one declared unit of a work file.
type WorkUnit struct {
	Name string
	Dir  string // root-relative slash dir; "." for the root itself
	Lang string // optional adapter pin; "" = auto-detect
	// Config optionally overrides the unit's own config path (root-relative);
	// "" means <dir>/depdog.yaml.
	Config string
	// Identities are the import-path identities the unit exposes (go.mod module
	// path, package.json name, …), matched by segment prefix against external
	// imports of other units. Filled by the loader, not the parser.
	Identities []string
}

// Surface declares which sub-paths of a unit other units may reach. Globs are
// unit-relative (they move with the unit). A target sub-path matching Internal
// is denied even when the unit edge is allowed; when Exports is non-empty, a
// non-matching non-empty sub-path is denied too (whitelist). The empty
// sub-path — importing the unit's public root, e.g. a bare package name —
// always passes the exports check.
type Surface struct {
	Exports  []string
	Internal []string
}

// WorkRules is the compiled form of depdog.work.yaml. Rules is a synthetic
// rule set whose components are the units (one exact-dir pattern each), so the
// unit-level verdict is the ordinary Evaluate with units as members.
type WorkRules struct {
	Units    []WorkUnit // sorted by name
	Rules    *RuleSet
	Surfaces map[string]Surface
}

// Unit returns the declared unit with the given name, or nil.
func (w *WorkRules) Unit(name string) *WorkUnit {
	for i := range w.Units {
		if w.Units[i].Name == name {
			return &w.Units[i]
		}
	}
	return nil
}

// Owner resolves which unit owns the root-relative dir, longest unit dir
// wins (most-specific-wins, the component doctrine at unit granularity).
// subPath is the remainder relative to the owning unit's dir ("" for the unit
// dir itself). ok is false when no declared unit contains the dir.
func (w *WorkRules) Owner(relDir string) (name, subPath string, ok bool) {
	relDir = path.Clean(relDir)
	best := -1
	for i := range w.Units {
		d := w.Units[i].Dir
		if d == "." {
			if best < 0 {
				best = i
			}
			continue
		}
		if relDir == d || strings.HasPrefix(relDir, d+"/") {
			if best < 0 || len(d) > len(w.Units[best].Dir) || w.Units[best].Dir == "." {
				best = i
			}
		}
	}
	if best < 0 {
		return "", "", false
	}
	u := w.Units[best]
	switch {
	case relDir == u.Dir:
		return u.Name, "", true
	case u.Dir == ".":
		if relDir == "." {
			return u.Name, "", true
		}
		return u.Name, relDir, true
	default:
		return u.Name, relDir[len(u.Dir)+1:], true
	}
}

// identityOwner matches an external import path against the units' declared
// identities, longest identity wins. subPath is the import-path remainder
// after the identity ("" for the identity itself).
func (w *WorkRules) identityOwner(importPath string) (name, subPath string, ok bool) {
	bestLen := -1
	for i := range w.Units {
		for _, id := range w.Units[i].Identities {
			if id == "" {
				continue
			}
			if importPath == id || strings.HasPrefix(importPath, id+"/") {
				if len(id) > bestLen {
					bestLen = len(id)
					name = w.Units[i].Name
					subPath = strings.TrimPrefix(strings.TrimPrefix(importPath, id), "/")
				}
			}
		}
	}
	return name, subPath, bestLen >= 0
}

// UnitGraph pairs one declared unit with its scanned graph for the cross pass.
type UnitGraph struct {
	Unit  string // declared unit name
	Graph *Graph
}

// crossRef is one contributing import statement of a cross-unit edge: the
// display path a violation should show, the target sub-path the surface check
// judges, and root-relative positions.
type crossRef struct {
	display   string // identity channel: the raw import path; path channel: the root-relative target dir
	subPath   string // target-unit-relative dir/path remainder; "" = the unit's public root
	testOnly  bool
	positions []Position
}

// crossEdge aggregates every detected reference from one unit to another.
type crossEdge struct {
	from, to string
	refs     []crossRef // sorted by display then subPath
	testOnly bool       // true only when every contributing import is test-only
}

// EvaluateWork derives the cross-unit edges from the scanned unit graphs,
// evaluates them against the work rules, and returns one Result: violations
// carry the cross-unit reason kinds, Components are per-unit stats, Cycles are
// unit cycles (advisory, as ever). Positions are root-relative. Sources inside
// a unit's subtree that a more specific nested unit owns are attributed to the
// nested unit's own scan (and skipped here), so an outer unit scanning its
// whole subtree does not fabricate edges on behalf of the inner one.
func EvaluateWork(inputs []UnitGraph, w *WorkRules) (*Result, error) {
	edges := deriveEdges(inputs, w)

	super := &Graph{ModulePath: "cross-unit"}
	byFrom := make(map[string][]crossEdge)
	for _, e := range edges {
		byFrom[e.from] = append(byFrom[e.from], e)
	}
	for _, u := range w.Units { // sorted by name, so the graph is sorted
		p := Package{ImportPath: u.Name, RelDir: u.Dir}
		for _, e := range byFrom[u.Name] {
			imp := Import{
				Path:     e.to,
				Class:    ClassInModule,
				RelDir:   w.Unit(e.to).Dir,
				TestOnly: e.testOnly,
			}
			for _, r := range e.refs {
				imp.Positions = append(imp.Positions, r.positions...)
			}
			sortPositions(imp.Positions)
			p.Imports = append(p.Imports, imp)
		}
		sort.Slice(p.Imports, func(i, j int) bool { return p.Imports[i].Path < p.Imports[j].Path })
		super.Packages = append(super.Packages, p)
	}

	res, err := Evaluate(super, w.Rules)
	if err != nil {
		return nil, err
	}

	// Remap the intra-unit kinds onto their cross-unit counterparts and track
	// which unit edges were denied, so the surface pass keeps the one-violation-
	// per-unit-edge discipline boundaries established.
	denied := make(map[string]bool, len(res.Violations))
	for i := range res.Violations {
		v := &res.Violations[i]
		switch v.Reason {
		case ReasonBoundary:
			v.Reason = ReasonCrossUnitBoundary
		case ReasonBoundarySealed:
			v.Reason = ReasonCrossUnitBoundarySealed
		default:
			v.Reason = ReasonCrossUnit
		}
		denied[v.FromPackage+"\x00"+v.ImportPath] = true
	}

	res.Violations = append(res.Violations, surfaceViolations(edges, w, denied)...)
	sortCrossViolations(res.Violations)
	return res, nil
}

// deriveEdges walks every scanned unit graph and collects the cross-unit
// references, aggregated per (from, to) pair and deterministically sorted.
func deriveEdges(inputs []UnitGraph, w *WorkRules) []crossEdge {
	type refKey struct{ display, subPath string }
	type edgeAcc struct {
		refs    map[refKey]*crossRef
		allTest bool
	}
	acc := make(map[[2]string]*edgeAcc)

	add := func(from, to, display, subPath string, imp Import, srcDir string) {
		key := [2]string{from, to}
		e := acc[key]
		if e == nil {
			e = &edgeAcc{refs: make(map[refKey]*crossRef), allTest: true}
			acc[key] = e
		}
		rk := refKey{display, subPath}
		r := e.refs[rk]
		if r == nil {
			r = &crossRef{display: display, subPath: subPath, testOnly: true}
			e.refs[rk] = r
		}
		for _, pos := range imp.Positions {
			r.positions = append(r.positions, Position{File: joinRel(srcDir, pos.File), Line: pos.Line})
		}
		if !imp.TestOnly {
			r.testOnly = false
			e.allTest = false
		}
	}

	for _, in := range inputs {
		u := w.Unit(in.Unit)
		if u == nil || in.Graph == nil {
			continue
		}
		for _, p := range in.Graph.Packages {
			// A package inside a nested, more specific unit belongs to that
			// unit's own scan; judging it here would double-report its edges
			// under the outer unit's name.
			srcRoot := joinRel(u.Dir, p.RelDir)
			if owner, _, ok := w.Owner(srcRoot); !ok || owner != u.Name {
				continue
			}
			for _, imp := range p.Imports {
				switch imp.Class {
				case ClassInModule:
					target := joinRel(u.Dir, imp.RelDir)
					if strings.HasPrefix(target, "../") || target == ".." {
						continue // escapes the walk root: not ours to govern
					}
					owner, sub, ok := w.Owner(target)
					if !ok || owner == u.Name {
						continue
					}
					add(u.Name, owner, target, sub, imp, u.Dir)
				case ClassExternal:
					owner, sub, ok := w.identityOwner(imp.Path)
					if !ok || owner == u.Name {
						continue
					}
					add(u.Name, owner, imp.Path, sub, imp, u.Dir)
				}
			}
		}
	}

	keys := make([][2]string, 0, len(acc))
	for k := range acc {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	edges := make([]crossEdge, 0, len(keys))
	for _, k := range keys {
		e := acc[k]
		refs := make([]crossRef, 0, len(e.refs))
		for _, r := range e.refs {
			sortPositions(r.positions)
			refs = append(refs, *r)
		}
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].display != refs[j].display {
				return refs[i].display < refs[j].display
			}
			return refs[i].subPath < refs[j].subPath
		})
		edges = append(edges, crossEdge{from: k[0], to: k[1], refs: refs, testOnly: e.allTest})
	}
	return edges
}

// surfaceViolations checks every unit-level-allowed edge against the target
// unit's declared surface: a ref whose sub-path matches an internal glob is
// denied; with a non-empty exports list, a non-matching non-empty sub-path is
// denied too. One violation per offending ref, so the report can name the
// concrete path that crossed the line.
func surfaceViolations(edges []crossEdge, w *WorkRules, denied map[string]bool) []Violation {
	var out []Violation
	for _, e := range edges {
		if denied[e.from+"\x00"+e.to] {
			continue
		}
		s, ok := w.Surfaces[e.to]
		if !ok {
			continue
		}
		for _, r := range e.refs {
			if r.subPath == "" {
				continue // the unit's public root always passes
			}
			if matchSurface(s.Internal, r.subPath) {
				out = append(out, Violation{
					FromPackage:   e.from,
					FromComponent: e.from,
					ImportPath:    r.display,
					Target:        e.to,
					Rule:          e.to + ": internal [" + strings.Join(s.Internal, ", ") + "]",
					TestOnly:      r.testOnly,
					Positions:     r.positions,
					Reason:        ReasonCrossUnitSurface,
				})
				continue
			}
			if len(s.Exports) > 0 {
				if !matchSurface(s.Exports, r.subPath) {
					out = append(out, Violation{
						FromPackage:   e.from,
						FromComponent: e.from,
						ImportPath:    r.display,
						Target:        e.to,
						Rule:          e.to + ": exports [" + strings.Join(s.Exports, ", ") + "]",
						TestOnly:      r.testOnly,
						Positions:     r.positions,
						Reason:        ReasonCrossUnitSurface,
					})
				}
			}
		}
	}
	return out
}

// matchSurface reports whether any glob in the list matches the sub-path.
// Globs are validated at parse time, so a match error cannot fire here; it is
// treated as no-match for safety.
func matchSurface(globs []string, subPath string) bool {
	for _, g := range globs {
		if ok, err := MatchPattern(g, subPath); err == nil && ok {
			return true
		}
	}
	return false
}

// joinRel joins a root-relative base dir with a further relative path,
// keeping the "." conventions of both sides.
func joinRel(base, rel string) string {
	if base == "" || base == "." {
		return path.Clean(rel)
	}
	return path.Join(base, rel)
}

func sortPositions(ps []Position) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].File != ps[j].File {
			return ps[i].File < ps[j].File
		}
		return ps[i].Line < ps[j].Line
	})
}

// sortCrossViolations orders the cross-unit result deterministically: by
// source unit, target unit, then the display path (surface violations sort
// after the unit-level violation of the same edge because their reason kind is
// greater).
func sortCrossViolations(vs []Violation) {
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].FromPackage != vs[j].FromPackage {
			return vs[i].FromPackage < vs[j].FromPackage
		}
		if vs[i].Target != vs[j].Target {
			return vs[i].Target < vs[j].Target
		}
		if vs[i].Reason != vs[j].Reason {
			return vs[i].Reason < vs[j].Reason
		}
		return vs[i].ImportPath < vs[j].ImportPath
	})
}
