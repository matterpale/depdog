package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// ComponentEdge is one directed cross-component import edge in the architecture
// diff: component From depends on component To. CrossesBoundary reports whether
// this edge is forbidden by (or, for an added edge, would newly traverse) a
// boundary; Boundary names the boundary that flags it. Both are resolved under
// the current config's RuleSet, so before/after edges are judged against one
// architecture definition.
type ComponentEdge struct {
	From, To        string
	CrossesBoundary bool
	Boundary        string
}

// ArchDiff is the result of comparing two architectures: the cross-component
// edges present in the "after" graph but not the "before" (Added) and vice
// versa (Removed), plus the roll-up counts the renderers summarise. Added and
// Removed are sorted by From then To.
type ArchDiff struct {
	Added   []ComponentEdge
	Removed []ComponentEdge
	// Counts describe the delta: how many edges were added/removed and how many
	// of those cross a boundary.
	AddedCount        int
	RemovedCount      int
	BoundaryCrossings int // added edges that cross a boundary (the notable churn)
}

// Diff compares two import graphs at component granularity and returns the
// cross-component edges added and removed. Both graphs are mapped to components
// under the same RuleSet rs (the current architecture definition), so the diff
// reflects structural movement rather than a config change. Edges to std or
// external targets and intra-component edges are ignored — architecture drift is
// about first-party cross-component structure. The engine is pure: it takes
// core types and does no git or process IO (that lives in the CLI).
func Diff(before, after *core.Graph, rs *core.RuleSet) (ArchDiff, error) {
	beforeSet, beforeDirs, err := edgeSet(before, rs)
	if err != nil {
		return ArchDiff{}, err
	}
	afterSet, afterDirs, err := edgeSet(after, rs)
	if err != nil {
		return ArchDiff{}, err
	}

	// Prefer the after graph's representative dirs for boundary resolution (it
	// is the current tree), falling back to the before graph's for components
	// that only existed there.
	repDirs := afterDirs
	for comp, dir := range beforeDirs {
		if _, ok := repDirs[comp]; !ok {
			repDirs[comp] = dir
		}
	}

	flag := func(p componentPair) (ComponentEdge, error) {
		e := ComponentEdge{From: orUnassigned(p.from), To: orUnassigned(p.to)}
		allowed, boundary, _, derr := rs.DecideBoundary(repDirs[p.from], repDirs[p.to])
		if derr != nil {
			return ComponentEdge{}, derr
		}
		// DecideBoundary returns allowed=true when no boundary denies the edge;
		// a boundary crossing is the negation.
		if !allowed {
			e.CrossesBoundary = true
			e.Boundary = boundary
		}
		return e, nil
	}

	var d ArchDiff
	for _, p := range sortedComponentPairs(afterSet) {
		if beforeSet[p] {
			continue
		}
		e, err := flag(p)
		if err != nil {
			return ArchDiff{}, err
		}
		d.Added = append(d.Added, e)
		if e.CrossesBoundary {
			d.BoundaryCrossings++
		}
	}
	for _, p := range sortedComponentPairs(beforeSet) {
		if afterSet[p] {
			continue
		}
		e, err := flag(p)
		if err != nil {
			return ArchDiff{}, err
		}
		d.Removed = append(d.Removed, e)
	}
	d.AddedCount = len(d.Added)
	d.RemovedCount = len(d.Removed)
	return d, nil
}

// edgeSet builds a graph's set of cross-component edges (via the shared
// componentEdgeSet extraction) plus a representative module-relative dir for
// each component, which the boundary machinery needs to decide crossings. The
// representative dir is the lexicographically smallest rel dir assigned to the
// component, so the choice is deterministic.
func edgeSet(g *core.Graph, rs *core.RuleSet) (map[componentPair]bool, map[string]string, error) {
	views, err := core.BuildPackageViews(g, rs)
	if err != nil {
		return nil, nil, err
	}
	set := componentEdgeSet(views)

	repDirs := make(map[string]string)
	for _, p := range g.Packages {
		if rs.Skipped(p.RelDir) {
			continue
		}
		comp, err := rs.AssignComponent(p.RelDir)
		if err != nil {
			return nil, nil, err
		}
		if cur, ok := repDirs[comp]; !ok || p.RelDir < cur {
			repDirs[comp] = p.RelDir
		}
	}
	return set, repDirs, nil
}

// DiffText writes a readable architecture diff: a summary line relative to
// `since`, then the sorted added and removed cross-component edges (boundary
// crossings marked). An empty diff reads clearly. Output is deterministic.
func DiffText(w io.Writer, d ArchDiff, since string) error {
	var b []byte
	b = appendf(b, "depdog diff — since %s\n", since)

	if d.AddedCount == 0 && d.RemovedCount == 0 {
		b = appendf(b, "\n✓ no cross-component edge changes since %s\n", since)
		_, err := w.Write(b)
		return err
	}

	b = appendf(b, "\n%s added, %s removed",
		plural(d.AddedCount, "cross-component edge"), countOnly(d.RemovedCount))
	if d.BoundaryCrossings > 0 {
		b = appendf(b, "; %s a boundary", crossingPhrase(d.BoundaryCrossings))
	}
	b = append(b, '\n')

	if len(d.Added) > 0 {
		b = appendf(b, "\n+ added\n")
		b = appendEdges(b, d.Added, "+")
	}
	if len(d.Removed) > 0 {
		b = appendf(b, "\n- removed\n")
		b = appendEdges(b, d.Removed, "-")
	}

	_, err := w.Write(b)
	return err
}

// appendEdges renders one edge per line, "<sign> from → to" with a boundary
// callout when the edge crosses one.
func appendEdges(b []byte, edges []ComponentEdge, sign string) []byte {
	// Callers pass already-sorted slices, but re-sort so text output stays
	// deterministic even for an unsorted caller.
	for _, e := range sortEdges(edges) {
		if e.CrossesBoundary {
			b = appendf(b, "    %s %s → %s  (crosses boundary %q)\n", sign, e.From, e.To, e.Boundary)
		} else {
			b = appendf(b, "    %s %s → %s\n", sign, e.From, e.To)
		}
	}
	return b
}

// DiffGitHub writes a PR-comment markdown block summarising the architecture
// diff: a heading, a one-line count relative to `since`, then bulleted Added
// and Removed lists with boundary crossings called out per line. This is a
// human-readable comment (not the ::error:: workflow-command style of
// report.GitHub) — a reviewer reads it inline on the PR. An empty diff reads
// clearly. Output is deterministic.
func DiffGitHub(w io.Writer, d ArchDiff, since string) error {
	var b []byte
	b = appendf(b, "### depdog: architecture diff since `%s`\n", mdCode(since))

	if d.AddedCount == 0 && d.RemovedCount == 0 {
		b = appendf(b, "\nNo cross-component edge changes.\n")
		_, err := w.Write(b)
		return err
	}

	b = appendf(b, "\n**%s added", plural(d.AddedCount, "new cross-component edge"))
	if d.BoundaryCrossings > 0 {
		b = appendf(b, " (%s a boundary)", crossingPhrase(d.BoundaryCrossings))
	}
	b = appendf(b, ", %s removed** vs `%s`.\n", countOnly(d.RemovedCount), mdCode(since))

	if len(d.Added) > 0 {
		b = appendf(b, "\n**Added**\n")
		b = appendGitHubEdges(b, d.Added)
	}
	if len(d.Removed) > 0 {
		b = appendf(b, "\n**Removed**\n")
		b = appendGitHubEdges(b, d.Removed)
	}

	_, err := w.Write(b)
	return err
}

// appendGitHubEdges renders one markdown bullet per edge, "- `from` → `to`",
// with a boundary callout when the edge crosses one. Edges are re-sorted so a
// caller passing an unsorted slice still yields deterministic output.
func appendGitHubEdges(b []byte, edges []ComponentEdge) []byte {
	for _, e := range sortEdges(edges) {
		if e.CrossesBoundary {
			b = appendf(b, "- `%s` → `%s` — crosses boundary `%s`\n", mdCode(e.From), mdCode(e.To), mdCode(e.Boundary))
		} else {
			b = appendf(b, "- `%s` → `%s`\n", mdCode(e.From), mdCode(e.To))
		}
	}
	return b
}

// mdCode neutralises a backtick in a value rendered inside inline code spans,
// so a component or ref name containing one cannot break out of the span.
func mdCode(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}

// jsonDiff is the stable structured delta emitted by DiffJSON. Field names are
// snake_case and absent collections encode as [] rather than null, matching the
// report/json.go schema conventions — this is part of depdog's public
// interface.
type jsonDiff struct {
	Since   string         `json:"since"`
	Added   []jsonDiffEdge `json:"added"`
	Removed []jsonDiffEdge `json:"removed"`
	Stats   jsonDiffStats  `json:"stats"`
}

type jsonDiffEdge struct {
	From            string `json:"from"`
	To              string `json:"to"`
	CrossesBoundary bool   `json:"crosses_boundary"`
	Boundary        string `json:"boundary,omitempty"` // boundary name; omitted when the edge crosses none
}

type jsonDiffStats struct {
	Added             int `json:"added"`
	Removed           int `json:"removed"`
	BoundaryCrossings int `json:"boundary_crossings"`
}

// DiffJSON writes the architecture diff as a stable structured delta: the
// `since` ref, sorted added/removed edge arrays (each with a boundary-crossing
// flag), and roll-up stats. snake_case; empty arrays encode as [] not null;
// deterministic given a sorted ArchDiff (and re-sorted here to be safe).
func DiffJSON(w io.Writer, d ArchDiff, since string) error {
	out := jsonDiff{
		Since:   since,
		Added:   jsonDiffEdges(d.Added),
		Removed: jsonDiffEdges(d.Removed),
		Stats: jsonDiffStats{
			Added:             d.AddedCount,
			Removed:           d.RemovedCount,
			BoundaryCrossings: d.BoundaryCrossings,
		},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// jsonDiffEdges maps engine edges to their JSON shape, always returning a
// non-nil slice so an absent list encodes as [] rather than null.
func jsonDiffEdges(edges []ComponentEdge) []jsonDiffEdge {
	out := make([]jsonDiffEdge, 0, len(edges))
	for _, e := range sortEdges(edges) {
		out = append(out, jsonDiffEdge(e))
	}
	return out
}

// sortEdges returns a copy of edges sorted by From then To, so every renderer
// emits deterministic output even if a caller passes an unsorted slice.
func sortEdges(edges []ComponentEdge) []ComponentEdge {
	sorted := append([]ComponentEdge(nil), edges...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].From != sorted[j].From {
			return sorted[i].From < sorted[j].From
		}
		return sorted[i].To < sorted[j].To
	})
	return sorted
}

// countOnly renders just a number of removed edges ("N removed" reads as
// "cross-component edge added, N removed", so the noun is not repeated).
func countOnly(n int) string {
	return fmt.Sprintf("%d", n)
}

// crossingPhrase renders "K crosses"/"K cross" agreeing with the count, so the
// summary line reads naturally ("1 crosses a boundary" / "3 cross a boundary").
func crossingPhrase(n int) string {
	if n == 1 {
		return "1 crosses"
	}
	return fmt.Sprintf("%d cross", n)
}

func appendf(b []byte, format string, args ...any) []byte {
	return fmt.Appendf(b, format, args...)
}
