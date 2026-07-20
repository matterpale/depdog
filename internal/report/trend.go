package report

import (
	"encoding/json"
	"fmt"
	"io"
)

// TrendPoint is one commit's repo-level architecture metrics — the row a trend
// plots over git history. Derived from a full ArchMetrics via TrendPointFrom.
type TrendPoint struct {
	Commit            string // short git sha (or ref label)
	Components        int
	Edges             int
	BoundaryCrossings int
	Cycles            int
	MaxInstability    float64 // the worst (max) component instability — a coupling-quality signal
}

// TrendPointFrom reduces a full ArchMetrics to the repo-level row a trend shows,
// carrying the worst component instability as the single coupling-quality number.
func TrendPointFrom(commit string, m ArchMetrics) TrendPoint {
	max := 0.0
	for _, c := range m.Components {
		if c.Instability > max {
			max = c.Instability
		}
	}
	return TrendPoint{
		Commit:            commit,
		Components:        m.ComponentCount,
		Edges:             m.EdgeCount,
		BoundaryCrossings: m.BoundaryCrossings,
		Cycles:            m.Cycles,
		MaxInstability:    max,
	}
}

// Trend writes the architecture trend as an aligned table (one row per sampled
// commit, oldest first) followed by a delta summary from the first point to the
// last — so a reviewer sees whether the architecture is drifting. Deterministic
// given the input points.
func Trend(w io.Writer, points []TrendPoint, since string) error {
	var b []byte
	b = appendf(b, "depdog trend — since %s\n\n", since)
	if len(points) == 0 {
		b = appendf(b, "no commits in range\n")
		_, err := w.Write(b)
		return err
	}

	const (
		hCommit = "COMMIT"
		hComp   = "COMPONENTS"
		hEdges  = "EDGES"
		hBound  = "BOUNDARY-X"
		hCycles = "CYCLES"
		hInst   = "MAX-INSTAB"
	)
	commitW := len(hCommit)
	for _, p := range points {
		if len(p.Commit) > commitW {
			commitW = len(p.Commit)
		}
	}

	b = appendf(b, "%-*s  %s  %s  %s  %s  %s\n", commitW, hCommit, hComp, hEdges, hBound, hCycles, hInst)
	for _, p := range points {
		b = appendf(b, "%-*s  %*d  %*d  %*d  %*d  %*.2f\n",
			commitW, p.Commit,
			len(hComp), p.Components,
			len(hEdges), p.Edges,
			len(hBound), p.BoundaryCrossings,
			len(hCycles), p.Cycles,
			len(hInst), p.MaxInstability)
	}

	first, last := points[0], points[len(points)-1]
	b = appendf(b, "\n%s → %s: %s · %s · %s · %s\n",
		first.Commit, last.Commit,
		signed(last.Components-first.Components, "component"),
		signed(last.Edges-first.Edges, "cross-component edge"),
		signed(last.BoundaryCrossings-first.BoundaryCrossings, "boundary crossing"),
		signed(last.Cycles-first.Cycles, "cycle"))

	_, err := w.Write(b)
	return err
}

// signed renders a delta with an explicit sign and pluralized noun, e.g.
// "+2 components", "-1 cycle", "±0 edges".
func signed(n int, noun string) string {
	sign := "+"
	switch {
	case n < 0:
		sign = "-"
	case n == 0:
		sign = "±"
	}
	mag := n
	if mag < 0 {
		mag = -mag
	}
	word := noun
	if mag != 1 {
		word += "s"
	}
	return fmt.Sprintf("%s%d %s", sign, mag, word)
}

// jsonTrend is the stable structured trend emitted by TrendJSON. snake_case;
// points are oldest-first and the delta is first→last.
type jsonTrend struct {
	Since  string           `json:"since"`
	Points []jsonTrendPoint `json:"points"`
	Delta  jsonTrendDelta   `json:"delta"`
}

type jsonTrendPoint struct {
	Commit            string  `json:"commit"`
	Components        int     `json:"components"`
	Edges             int     `json:"edges"`
	BoundaryCrossings int     `json:"boundary_crossings"`
	Cycles            int     `json:"cycles"`
	MaxInstability    float64 `json:"max_instability"`
}

type jsonTrendDelta struct {
	Components        int `json:"components"`
	Edges             int `json:"edges"`
	BoundaryCrossings int `json:"boundary_crossings"`
	Cycles            int `json:"cycles"`
}

// TrendJSON writes the trend as a stable structured series: the since ref, a
// sorted (oldest-first) points array, and the first→last delta. snake_case;
// points always a non-nil array.
func TrendJSON(w io.Writer, points []TrendPoint, since string) error {
	pts := make([]jsonTrendPoint, 0, len(points))
	for _, p := range points {
		pts = append(pts, jsonTrendPoint(p))
	}
	out := jsonTrend{Since: since, Points: pts}
	if len(points) > 0 {
		first, last := points[0], points[len(points)-1]
		out.Delta = jsonTrendDelta{
			Components:        last.Components - first.Components,
			Edges:             last.Edges - first.Edges,
			BoundaryCrossings: last.BoundaryCrossings - first.BoundaryCrossings,
			Cycles:            last.Cycles - first.Cycles,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
