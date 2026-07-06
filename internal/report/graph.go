package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// Graph renders the module's in-module dependency graph in dot or mermaid, at
// component or package level, with violation edges highlighted. Standard-library
// and external edges are omitted — this is the architecture view. Package-level
// output shows module-relative labels and, in dot, clusters packages by
// component. Output is deterministic given sorted package views.
// GraphOptions configures Graph.
type GraphOptions struct {
	Format         string // "dot" or "mermaid"
	Level          string // "component" or "package"
	ViolationsOnly bool   // keep only violation edges and their endpoints
	Focus          string // keep only a component and its direct neighbours
}

func Graph(w io.Writer, module string, views []core.PackageView, violations []core.Violation, opts GraphOptions) error {
	switch opts.Level {
	case "component", "package":
	default:
		return fmt.Errorf("unknown graph --level %q (component or package)", opts.Level)
	}
	if opts.Focus != "" && !hasComponent(views, opts.Focus) {
		return fmt.Errorf("no component %q to focus on", opts.Focus)
	}

	nodes, edges := graphElements(module, views, violations, opts.Level, opts.Focus, opts.ViolationsOnly)
	cluster := opts.Level == "package"
	switch opts.Format {
	case "dot":
		return writeDOT(w, nodes, edges, cluster)
	case "mermaid":
		return writeMermaid(w, nodes, edges)
	default:
		return fmt.Errorf("unknown graph --format %q (dot or mermaid)", opts.Format)
	}
}

type graphNode struct {
	id        string // unique: import path (package level) or component name
	label     string // display label
	component string // owning component, for package-level clustering
}

type graphEdge struct {
	from, to  string // node ids
	violation bool
}

// hasComponent reports whether any package maps to the named component (with
// "unassigned" standing in for the empty component).
func hasComponent(views []core.PackageView, name string) bool {
	for _, pv := range views {
		if orUnassigned(pv.Component) == name {
			return true
		}
	}
	return false
}

func graphElements(module string, views []core.PackageView, violations []core.Violation, level, focus string, violationsOnly bool) ([]graphNode, []graphEdge) {
	violSet := make(map[[2]string]bool, len(violations))
	for _, v := range violations {
		violSet[[2]string{v.FromPackage, v.ImportPath}] = true
	}

	nodeInfo := map[string]graphNode{}
	ensure := func(gn graphNode) {
		if _, ok := nodeInfo[gn.id]; !ok {
			nodeInfo[gn.id] = gn
		}
	}
	edgeViol := map[[2]string]bool{}

	node := func(component, importPath string) graphNode {
		if level == "component" {
			c := orUnassigned(component)
			return graphNode{id: c, label: c}
		}
		return graphNode{id: importPath, label: shortLabel(importPath, module), component: orUnassigned(component)}
	}

	for _, pv := range views {
		src := node(pv.Component, pv.ImportPath)
		ensure(src)
		for _, iv := range pv.Imports {
			if iv.Class != core.ClassInModule {
				continue
			}
			dst := node(iv.Component, iv.Path)
			ensure(dst)
			if dst.id == src.id {
				continue
			}
			k := [2]string{src.id, dst.id}
			edgeViol[k] = edgeViol[k] || violSet[[2]string{pv.ImportPath, iv.Path}]
		}
	}

	nodes := make([]graphNode, 0, len(nodeInfo))
	for _, gn := range nodeInfo {
		nodes = append(nodes, gn)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].id < nodes[j].id })

	edges := make([]graphEdge, 0, len(edgeViol))
	for k, viol := range edgeViol {
		edges = append(edges, graphEdge{from: k[0], to: k[1], violation: viol})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		return edges[i].to < edges[j].to
	})

	if focus != "" {
		nodes, edges = keepFocus(nodes, edges, focus, level)
	}
	if violationsOnly {
		nodes, edges = keepViolations(nodes, edges)
	}
	return nodes, edges
}

// keepFocus narrows the graph to the focus component and its direct neighbours:
// every edge touching a focus node, plus the nodes at either end. At component
// level the focus is the node itself; at package level it is every package in
// that component.
func keepFocus(nodes []graphNode, edges []graphEdge, focus, level string) ([]graphNode, []graphEdge) {
	inFocus := map[string]bool{}
	for _, n := range nodes {
		if (level == "component" && n.id == focus) || (level == "package" && n.component == focus) {
			inFocus[n.id] = true
		}
	}
	used := map[string]bool{}
	ke := edges[:0]
	for _, e := range edges {
		if inFocus[e.from] || inFocus[e.to] {
			ke = append(ke, e)
			used[e.from] = true
			used[e.to] = true
		}
	}
	kn := nodes[:0]
	for _, n := range nodes {
		if inFocus[n.id] || used[n.id] {
			kn = append(kn, n)
		}
	}
	return kn, ke
}

// keepViolations drops every edge that is not a violation and every node left
// with no violation edge, preserving order.
func keepViolations(nodes []graphNode, edges []graphEdge) ([]graphNode, []graphEdge) {
	used := map[string]bool{}
	ve := edges[:0]
	for _, e := range edges {
		if e.violation {
			ve = append(ve, e)
			used[e.from] = true
			used[e.to] = true
		}
	}
	vn := nodes[:0]
	for _, n := range nodes {
		if used[n.id] {
			vn = append(vn, n)
		}
	}
	return vn, ve
}

func shortLabel(path, module string) string {
	switch {
	case module == "":
		return path
	case path == module:
		return "."
	case strings.HasPrefix(path, module+"/"):
		return strings.TrimPrefix(path, module+"/")
	default:
		return path
	}
}

func writeDOT(w io.Writer, nodes []graphNode, edges []graphEdge, cluster bool) error {
	var b strings.Builder
	b.WriteString("digraph depdog {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box];\n")

	if cluster {
		byComp := map[string][]graphNode{}
		var comps []string
		for _, n := range nodes {
			if _, ok := byComp[n.component]; !ok {
				comps = append(comps, n.component)
			}
			byComp[n.component] = append(byComp[n.component], n)
		}
		sort.Strings(comps)
		for _, comp := range comps {
			fmt.Fprintf(&b, "  subgraph %q {\n", "cluster_"+comp)
			fmt.Fprintf(&b, "    label=%q;\n", comp)
			for _, n := range byComp[comp] {
				fmt.Fprintf(&b, "    %q [label=%q];\n", n.id, n.label)
			}
			b.WriteString("  }\n")
		}
	} else {
		for _, n := range nodes {
			if n.label == n.id {
				fmt.Fprintf(&b, "  %q;\n", n.id)
			} else {
				fmt.Fprintf(&b, "  %q [label=%q];\n", n.id, n.label)
			}
		}
	}

	for _, e := range edges {
		if e.violation {
			fmt.Fprintf(&b, "  %q -> %q [color=\"red\", penwidth=2];\n", e.from, e.to)
		} else {
			fmt.Fprintf(&b, "  %q -> %q;\n", e.from, e.to)
		}
	}
	b.WriteString("}\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeMermaid(w io.Writer, nodes []graphNode, edges []graphEdge) error {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.id
	}
	mid := mermaidIDs(ids)

	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s[%q]\n", mid[n.id], n.label)
	}
	for _, e := range edges {
		if e.violation {
			fmt.Fprintf(&b, "  %s -->|✗| %s\n", mid[e.from], mid[e.to])
		} else {
			fmt.Fprintf(&b, "  %s --> %s\n", mid[e.from], mid[e.to])
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func orUnassigned(comp string) string {
	if comp == "" {
		return "unassigned"
	}
	return comp
}

// mermaidIDs maps each node id to a unique identifier safe for mermaid, keeping
// the mapping deterministic over the sorted node list.
func mermaidIDs(ids []string) map[string]string {
	used := map[string]bool{}
	out := make(map[string]string, len(ids))
	for _, n := range ids {
		base := sanitizeID(n)
		id := base
		for i := 1; used[id]; i++ {
			id = fmt.Sprintf("%s_%d", base, i)
		}
		used[id] = true
		out[n] = id
	}
	return out
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "n"
	}
	return b.String()
}
