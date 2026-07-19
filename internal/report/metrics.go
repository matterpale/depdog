package report

import (
	"encoding/json"
	"io"
	"math"
	"sort"

	"github.com/matterpale/depdog/internal/core"
)

// ComponentMetric holds the coupling numbers for one component: its afferent
// coupling (FanIn — how many other components import it), efferent coupling
// (FanOut — how many it imports), and Robert Martin's instability
// I = FanOut / (FanIn + FanOut) in [0,1] (0 = maximally stable / depended-upon,
// 1 = maximally unstable / depends-on-others). Isolated components (no
// cross-component edges) report I = 0. Instability is rounded to two decimals so
// text and JSON agree and the JSON stays clean.
type ComponentMetric struct {
	Component   string
	FanIn       int
	FanOut      int
	Instability float64
}

// ArchMetrics is the architecture-health snapshot metrics reports: per-component
// coupling (sorted by component display name) plus repo-level roll-ups. Edges
// counts distinct directed cross-component edges (the same atom `graph` and
// `diff` use); Cycles is the component-cycle count from the check's Tarjan SCC.
type ArchMetrics struct {
	Components        []ComponentMetric
	ComponentCount    int
	EdgeCount         int
	BoundaryCrossings int
	Cycles            int
}

// Metrics computes the architecture-health snapshot from an import graph and its
// RuleSet: per-component fan-in/fan-out/instability over the distinct
// cross-component edges (via the shared componentEdgeSet extraction), plus
// roll-up totals. cycles is the component-cycle count the caller already has from
// core.Evaluate (metrics does not re-run Tarjan). The engine is pure — it takes
// core types and does no IO — mirroring report.Diff.
func Metrics(g *core.Graph, rs *core.RuleSet, cycles int) (ArchMetrics, error) {
	// edgeSet gives the distinct cross-component edges plus a representative dir
	// per owning component (for boundary resolution) — the same extraction
	// report.Diff uses, so metrics never drifts from it. repDirs has one entry
	// per component that owns a package, so isolated components still get a row.
	edges, repDirs, err := edgeSet(g, rs)
	if err != nil {
		return ArchMetrics{}, err
	}

	fanIn := make(map[string]int)
	fanOut := make(map[string]int)
	boundaryCrossings := 0
	for p := range edges {
		fanOut[p.from]++
		fanIn[p.to]++
		allowed, _, _, derr := rs.DecideBoundary(repDirs[p.from], repDirs[p.to])
		if derr != nil {
			return ArchMetrics{}, derr
		}
		if !allowed {
			boundaryCrossings++
		}
	}

	names := make([]string, 0, len(repDirs))
	for c := range repDirs {
		names = append(names, c)
	}
	// Sort by display name so an "unassigned" row sorts among the u's rather
	// than first (the raw empty component would sort ahead of everything), with
	// the raw name as a stable tiebreak.
	sort.Slice(names, func(i, j int) bool {
		if a, b := orUnassigned(names[i]), orUnassigned(names[j]); a != b {
			return a < b
		}
		return names[i] < names[j]
	})

	m := ArchMetrics{
		ComponentCount:    len(repDirs),
		EdgeCount:         len(edges),
		BoundaryCrossings: boundaryCrossings,
		Cycles:            cycles,
	}
	for _, c := range names {
		ci, co := fanIn[c], fanOut[c]
		inst := 0.0
		if ci+co > 0 {
			inst = math.Round(float64(co)/float64(ci+co)*100) / 100
		}
		m.Components = append(m.Components, ComponentMetric{
			Component:   orUnassigned(c),
			FanIn:       ci,
			FanOut:      co,
			Instability: inst,
		})
	}
	return m, nil
}

// MetricsText writes a readable, aligned per-component coupling table followed by
// a one-line repo summary. Output is deterministic given a sorted ArchMetrics.
func MetricsText(w io.Writer, m ArchMetrics, module string) error {
	var b []byte
	b = appendf(b, "depdog metrics — %s\n\n", module)

	const (
		hName = "COMPONENT"
		hIn   = "FAN-IN"
		hOut  = "FAN-OUT"
		hInst = "INSTABILITY"
	)
	nameW := len(hName)
	for _, c := range m.Components {
		if len(c.Component) > nameW {
			nameW = len(c.Component)
		}
	}

	b = appendf(b, "%-*s  %s  %s  %s\n", nameW, hName, hIn, hOut, hInst)
	for _, c := range m.Components {
		b = appendf(b, "%-*s  %*d  %*d  %*.2f\n",
			nameW, c.Component, len(hIn), c.FanIn, len(hOut), c.FanOut, len(hInst), c.Instability)
	}

	b = appendf(b, "\n%s · %s · %s · %s\n",
		plural(m.ComponentCount, "component"),
		plural(m.EdgeCount, "cross-component edge"),
		plural(m.BoundaryCrossings, "boundary crossing"),
		plural(m.Cycles, "cycle"))

	_, err := w.Write(b)
	return err
}

// jsonMetrics is the stable structured metrics report emitted by MetricsJSON.
// Field names are snake_case and absent collections encode as [] rather than
// null, matching the report/json.go schema conventions — part of depdog's
// public interface.
type jsonMetrics struct {
	Module     string                `json:"module"`
	Components []jsonComponentMetric `json:"components"`
	Totals     jsonMetricsTotals     `json:"totals"`
}

type jsonComponentMetric struct {
	Component   string  `json:"component"`
	FanIn       int     `json:"fan_in"`
	FanOut      int     `json:"fan_out"`
	Instability float64 `json:"instability"`
}

type jsonMetricsTotals struct {
	Components        int `json:"components"`
	Edges             int `json:"edges"`
	BoundaryCrossings int `json:"boundary_crossings"`
	Cycles            int `json:"cycles"`
}

// MetricsJSON writes the architecture-health snapshot as a stable structured
// report: the module path, a sorted per-component array, and roll-up totals.
// snake_case; the components array encodes as [] not null; deterministic given a
// sorted ArchMetrics.
func MetricsJSON(w io.Writer, m ArchMetrics, module string) error {
	comps := make([]jsonComponentMetric, 0, len(m.Components))
	for _, c := range m.Components {
		comps = append(comps, jsonComponentMetric(c))
	}
	out := jsonMetrics{
		Module:     module,
		Components: comps,
		Totals: jsonMetricsTotals{
			Components:        m.ComponentCount,
			Edges:             m.EdgeCount,
			BoundaryCrossings: m.BoundaryCrossings,
			Cycles:            m.Cycles,
		},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
