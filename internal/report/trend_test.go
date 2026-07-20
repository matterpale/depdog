package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestTrendPointFromMaxInstability(t *testing.T) {
	m := ArchMetrics{
		ComponentCount: 2, EdgeCount: 1,
		Components: []ComponentMetric{{Component: "a", Instability: 1.0}, {Component: "b", Instability: 0.5}},
	}
	p := TrendPointFrom("abc1234", m)
	if p.Commit != "abc1234" || p.Components != 2 || p.Edges != 1 || p.MaxInstability != 1.0 {
		t.Errorf("TrendPointFrom = %+v", p)
	}
}

func TestTrendTextDelta(t *testing.T) {
	points := []TrendPoint{
		{Commit: "aaaaaaa", Components: 3, Edges: 4, BoundaryCrossings: 0, Cycles: 0, MaxInstability: 0.50},
		{Commit: "bbbbbbb", Components: 4, Edges: 7, BoundaryCrossings: 1, Cycles: 1, MaxInstability: 0.90},
	}
	var buf bytes.Buffer
	if err := Trend(&buf, points, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "COMPONENTS") || !strings.Contains(s, "MAX-INSTAB") {
		t.Errorf("missing table header:\n%s", s)
	}
	if !strings.Contains(s, "+1 component · +3 cross-component edges · +1 boundary crossing · +1 cycle") {
		t.Errorf("missing/incorrect delta line:\n%s", s)
	}
}

func TestTrendSignedZeroAndNegative(t *testing.T) {
	points := []TrendPoint{
		{Commit: "aaaaaaa", Components: 5, Edges: 10},
		{Commit: "bbbbbbb", Components: 5, Edges: 8},
	}
	var buf bytes.Buffer
	if err := Trend(&buf, points, "x"); err != nil {
		t.Fatal(err)
	}
	if s := buf.String(); !strings.Contains(s, "±0 components") || !strings.Contains(s, "-2 cross-component edges") {
		t.Errorf("signed deltas wrong:\n%s", s)
	}
}

func TestTrendJSONShape(t *testing.T) {
	points := []TrendPoint{
		{Commit: "aaaaaaa", Components: 3, Edges: 4},
		{Commit: "bbbbbbb", Components: 4, Edges: 7},
	}
	var buf bytes.Buffer
	if err := TrendJSON(&buf, points, "v1"); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{`"since": "v1"`, `"points"`, `"max_instability"`, `"boundary_crossings"`, `"delta"`, `"edges": 3`} {
		if !strings.Contains(s, want) {
			t.Errorf("json missing %s:\n%s", want, s)
		}
	}
	// An empty series still encodes points as [] (never null).
	var eb bytes.Buffer
	if err := TrendJSON(&eb, nil, "v1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eb.String(), `"points": []`) {
		t.Errorf("empty points should encode as []:\n%s", eb.String())
	}
}
