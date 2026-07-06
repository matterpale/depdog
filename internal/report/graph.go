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
// and external edges are omitted — this is the architecture view. Output is
// deterministic given sorted package views.
func Graph(w io.Writer, views []core.PackageView, violations []core.Violation, format, level string) error {
	switch level {
	case "component", "package":
	default:
		return fmt.Errorf("unknown graph --level %q (component or package)", level)
	}

	nodes, edges := graphElements(views, violations, level)
	switch format {
	case "dot":
		return writeDOT(w, nodes, edges)
	case "mermaid":
		return writeMermaid(w, nodes, edges)
	default:
		return fmt.Errorf("unknown graph --format %q (dot or mermaid)", format)
	}
}

type graphEdge struct {
	from, to  string
	violation bool
}

func graphElements(views []core.PackageView, violations []core.Violation, level string) ([]string, []graphEdge) {
	violSet := make(map[[2]string]bool, len(violations))
	for _, v := range violations {
		violSet[[2]string{v.FromPackage, v.ImportPath}] = true
	}

	nodeSet := map[string]bool{}
	edgeViol := map[[2]string]bool{}
	add := func(from, to string, viol bool) {
		nodeSet[from] = true
		nodeSet[to] = true
		k := [2]string{from, to}
		edgeViol[k] = edgeViol[k] || viol
	}

	for _, pv := range views {
		src := pv.ImportPath
		if level == "component" {
			src = orUnassigned(pv.Component)
		}
		nodeSet[src] = true
		for _, iv := range pv.Imports {
			if iv.Class != core.ClassInModule {
				continue
			}
			dst := iv.Path
			if level == "component" {
				dst = orUnassigned(iv.Component)
			}
			if dst == src {
				continue
			}
			add(src, dst, violSet[[2]string{pv.ImportPath, iv.Path}])
		}
	}

	nodes := make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

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
	return nodes, edges
}

func orUnassigned(comp string) string {
	if comp == "" {
		return "unassigned"
	}
	return comp
}

func writeDOT(w io.Writer, nodes []string, edges []graphEdge) error {
	var b strings.Builder
	b.WriteString("digraph depdog {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box];\n")
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %q;\n", n)
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

func writeMermaid(w io.Writer, nodes []string, edges []graphEdge) error {
	ids := mermaidIDs(nodes)
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s[%q]\n", ids[n], n)
	}
	for _, e := range edges {
		if e.violation {
			fmt.Fprintf(&b, "  %s -->|✗| %s\n", ids[e.from], ids[e.to])
		} else {
			fmt.Fprintf(&b, "  %s --> %s\n", ids[e.from], ids[e.to])
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// mermaidIDs maps each node to a unique identifier safe for mermaid, keeping
// the mapping deterministic over the sorted node list.
func mermaidIDs(nodes []string) map[string]string {
	used := map[string]bool{}
	out := make(map[string]string, len(nodes))
	for _, n := range nodes {
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
