package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

// coupleGraph: a imports b and c; b imports c; c imports nothing. Cross-component
// edges: a→b, a→c, b→c. So fan-out a=2,b=1,c=0; fan-in a=0,b=1,c=2.
func coupleGraph() (*core.Graph, *core.RuleSet) {
	return graph(
		pkg("a/x", "b/y", "c/z"),
		pkg("b/y", "c/z"),
		pkg("c/z"),
	), ruleSet("a", "b", "c")
}

func TestMetricsCoupling(t *testing.T) {
	g, rs := coupleGraph()
	m, err := Metrics(g, rs, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.ComponentCount != 3 || m.EdgeCount != 3 || m.BoundaryCrossings != 0 || m.Cycles != 0 {
		t.Fatalf("totals: comps=%d edges=%d cross=%d cycles=%d, want 3/3/0/0",
			m.ComponentCount, m.EdgeCount, m.BoundaryCrossings, m.Cycles)
	}
	want := map[string]ComponentMetric{
		"a": {Component: "a", FanIn: 0, FanOut: 2, Instability: 1.00},
		"b": {Component: "b", FanIn: 1, FanOut: 1, Instability: 0.50},
		"c": {Component: "c", FanIn: 2, FanOut: 0, Instability: 0.00},
	}
	if len(m.Components) != 3 {
		t.Fatalf("got %d component rows, want 3: %+v", len(m.Components), m.Components)
	}
	for _, got := range m.Components {
		if w, ok := want[got.Component]; !ok || got != w {
			t.Errorf("%s: got %+v, want %+v (ok=%v)", got.Component, got, w, ok)
		}
	}
	// Rows are sorted by (display) component name.
	if m.Components[0].Component != "a" || m.Components[1].Component != "b" || m.Components[2].Component != "c" {
		t.Errorf("components not sorted by name: %+v", m.Components)
	}
}

func TestMetricsBoundaryCrossings(t *testing.T) {
	g, rs := coupleGraph()
	// A boundary making a and b mutually exclusive: the a→b edge crosses it
	// (a→c and b→c do not).
	rs.Boundaries = []core.Boundary{{
		Name: "layers",
		Members: []core.BoundaryMember{
			{Component: "a", Patterns: []string{"a/**"}, Label: "a"},
			{Component: "b", Patterns: []string{"b/**"}, Label: "b"},
		},
	}}
	m, err := Metrics(g, rs, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.BoundaryCrossings != 1 {
		t.Errorf("BoundaryCrossings = %d, want 1 (only a→b crosses)", m.BoundaryCrossings)
	}
}

func TestMetricsCyclesPassedThrough(t *testing.T) {
	m, err := Metrics(graph(pkg("a/x")), ruleSet("a"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if m.Cycles != 5 {
		t.Errorf("cycles = %d, want 5 (echoed from the caller, not recomputed)", m.Cycles)
	}
}

func TestMetricsText(t *testing.T) {
	g, rs := coupleGraph()
	m, _ := Metrics(g, rs, 0)
	var buf bytes.Buffer
	if err := MetricsText(&buf, m, "m"); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "COMPONENT") || !strings.Contains(s, "INSTABILITY") {
		t.Errorf("missing table header:\n%s", s)
	}
	if !strings.Contains(s, "3 components · 3 cross-component edges · 0 boundary crossings · 0 cycles") {
		t.Errorf("missing/incorrect summary line:\n%s", s)
	}
}

func TestMetricsJSONShape(t *testing.T) {
	g, rs := coupleGraph()
	m, _ := Metrics(g, rs, 0)
	var buf bytes.Buffer
	if err := MetricsJSON(&buf, m, "m"); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{
		`"module": "m"`, `"fan_in"`, `"fan_out"`, `"instability"`,
		`"boundary_crossings"`, `"cycles"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("json missing %s:\n%s", want, s)
		}
	}

	// An empty module still encodes components as [] (never null) — the schema
	// convention shared with report/json.go and report/diff.go.
	empty, err := Metrics(graph(), ruleSet(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var eb bytes.Buffer
	if err := MetricsJSON(&eb, empty, "m"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eb.String(), `"components": []`) {
		t.Errorf("empty components should encode as []:\n%s", eb.String())
	}
}

// TestMetricsUnassignedRow: a package no component claims shows up as an
// "unassigned" row (and sorts among the u's, not first).
func TestMetricsUnassignedRow(t *testing.T) {
	// a/x imports z/x; only "a" is declared, so z is unassigned.
	g := graph(pkg("a/x", "z/x"), pkg("z/x"))
	rs := ruleSet("a")
	m, err := Metrics(g, rs, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.ComponentCount != 2 || m.EdgeCount != 1 {
		t.Fatalf("totals: comps=%d edges=%d, want 2/1", m.ComponentCount, m.EdgeCount)
	}
	byName := map[string]ComponentMetric{}
	for _, c := range m.Components {
		byName[c.Component] = c
	}
	if u, ok := byName["unassigned"]; !ok || u.FanIn != 1 || u.FanOut != 0 {
		t.Errorf("unassigned row = %+v ok=%v, want fan_in 1 fan_out 0", u, ok)
	}
	if a := byName["a"]; a.FanOut != 1 {
		t.Errorf("a fan_out = %d, want 1 (a → unassigned)", a.FanOut)
	}
	// Sorted by display name: "a" before "unassigned".
	if m.Components[0].Component != "a" || m.Components[1].Component != "unassigned" {
		t.Errorf("rows not sorted by display name: %+v", m.Components)
	}
}

// TestMetricsIsolatedComponent: a component that owns a package but has no
// cross-component edges reports fan-in/out 0 and instability 0.00.
func TestMetricsIsolatedComponent(t *testing.T) {
	g := graph(pkg("iso/x")) // imports nothing
	rs := ruleSet("iso")
	m, err := Metrics(g, rs, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.EdgeCount != 0 || len(m.Components) != 1 {
		t.Fatalf("want one isolated component and no edges, got %d components / %d edges", len(m.Components), m.EdgeCount)
	}
	if got := m.Components[0]; got != (ComponentMetric{Component: "iso", FanIn: 0, FanOut: 0, Instability: 0.0}) {
		t.Errorf("isolated component = %+v, want {iso 0 0 0.00}", got)
	}
}
