package report

import (
	"sort"

	"github.com/matterpale/depdog/internal/core"
)

// componentPair is a directed component→component edge, keyed by the two
// component names (from, to). It is the atom the component-level graph and the
// architecture diff both work in: an in-module import collapsed to the
// components its endpoints belong to.
type componentPair struct {
	from, to string
}

// componentEdgeSet collapses the package-level import graph (as PackageViews)
// into the set of directed component→component edges. Only in-module imports
// contribute (std/external targets are architecture-irrelevant here), and
// intra-component edges (from == to) are dropped. This is the single extraction
// shared by the component-level `graph` rendering and the `diff` engine so the
// two never drift on what a "cross-component edge" is.
//
// Component names are taken as-is (the empty component "" stands for
// unassigned); callers that display them apply orUnassigned.
func componentEdgeSet(views []core.PackageView) map[componentPair]bool {
	edges := make(map[componentPair]bool)
	for _, pv := range views {
		for _, iv := range pv.Imports {
			if iv.Class != core.ClassInModule {
				continue
			}
			if iv.Component == pv.Component {
				continue
			}
			edges[componentPair{from: pv.Component, to: iv.Component}] = true
		}
	}
	return edges
}

// sortedComponentPairs returns the pairs of an edge set sorted by from then to,
// for deterministic output.
func sortedComponentPairs(set map[componentPair]bool) []componentPair {
	out := make([]componentPair, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].from != out[j].from {
			return out[i].from < out[j].from
		}
		return out[i].to < out[j].to
	})
	return out
}
