package core

import "sort"

// componentCycles returns the strongly-connected components of the component
// adjacency graph that have more than one member — the sets of components that
// mutually depend on each other. Each cycle's members are sorted, and the
// cycles are sorted by first member, so output is deterministic. Uses Tarjan's
// algorithm; the number of components is small, so recursion depth is bounded.
func componentCycles(edges map[string]map[string]bool) [][]string {
	nodeSet := map[string]bool{}
	for from, tos := range edges {
		nodeSet[from] = true
		for to := range tos {
			nodeSet[to] = true
		}
	}
	nodes := make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	var (
		index   = map[string]int{}
		lowlink = map[string]int{}
		onStack = map[string]bool{}
		stack   []string
		next    int
		cycles  [][]string
		visit   func(v string)
	)
	visit = func(v string) {
		index[v] = next
		lowlink[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true

		neighbors := make([]string, 0, len(edges[v]))
		for w := range edges[v] {
			neighbors = append(neighbors, w)
		}
		sort.Strings(neighbors)
		for _, w := range neighbors {
			if _, seen := index[w]; !seen {
				visit(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if index[w] < lowlink[v] {
					lowlink[v] = index[w]
				}
			}
		}

		if lowlink[v] == index[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) >= 2 {
				sort.Strings(scc)
				cycles = append(cycles, scc)
			}
		}
	}

	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			visit(n)
		}
	}
	sort.Slice(cycles, func(i, j int) bool { return cycles[i][0] < cycles[j][0] })
	return cycles
}
