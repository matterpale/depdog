package core

import (
	"errors"
	"testing"
)

// bnd builds a boundary with component or glob members. A member label
// containing a "/" or "*" is treated as a glob; otherwise it names a component
// whose single pattern is the label mapped through patternsOf.
func compMember(name string, patterns ...string) BoundaryMember {
	return BoundaryMember{Component: name, Patterns: patterns, Label: name}
}

func globMember(pattern string) BoundaryMember {
	return BoundaryMember{Patterns: []string{pattern}, Label: pattern}
}

func TestBoundaryMembershipComponentMembers(t *testing.T) {
	rs := &RuleSet{Boundaries: []Boundary{{
		Name: "services",
		Members: []BoundaryMember{
			compMember("comparator", "cmd/comparator/**"),
			compMember("query-ce", "cmd/query-ce/**"),
		},
	}}}
	cases := []struct {
		relDir string
		want   int
	}{
		{"cmd/query-ce/x", 1},
		{"cmd/comparator/y", 0},
		{"internal/shared", -1}, // ungrouped
	}
	for _, c := range cases {
		m, err := rs.BoundaryMembership(c.relDir)
		if err != nil {
			t.Fatalf("BoundaryMembership(%q): %v", c.relDir, err)
		}
		if m[0] != c.want {
			t.Errorf("membership(%q) = %d, want %d", c.relDir, m[0], c.want)
		}
	}
}

func TestBoundaryMembershipGlobAndMixed(t *testing.T) {
	rs := &RuleSet{Boundaries: []Boundary{{
		Name: "b",
		Members: []BoundaryMember{
			globMember("cmd/comparator/**"),
			compMember("query-ce", "cmd/query-ce/**"),
		},
	}}}
	m, err := rs.BoundaryMembership("cmd/comparator/z")
	if err != nil {
		t.Fatal(err)
	}
	if m[0] != 0 {
		t.Errorf("glob member should own cmd/comparator/z: %d", m[0])
	}
	m, err = rs.BoundaryMembership("cmd/query-ce/z")
	if err != nil {
		t.Fatal(err)
	}
	if m[0] != 1 {
		t.Errorf("component member should own cmd/query-ce/z: %d", m[0])
	}
}

func TestBoundaryMembershipMostSpecificWins(t *testing.T) {
	// A broad member and a narrow member overlap; the narrower wins (no error).
	rs := &RuleSet{Boundaries: []Boundary{{
		Name: "b",
		Members: []BoundaryMember{
			globMember("cmd/**"),
			globMember("cmd/query-ce/**"),
		},
	}}}
	m, err := rs.BoundaryMembership("cmd/query-ce/x")
	if err != nil {
		t.Fatalf("most-specific-wins should resolve, not error: %v", err)
	}
	if m[0] != 1 {
		t.Errorf("narrower member should win: got %d, want 1", m[0])
	}
}

func TestBoundaryMembershipEqualSpecificityAmbiguous(t *testing.T) {
	// Two members match with equal specificity — the runtime disjointness rule.
	rs := &RuleSet{Boundaries: []Boundary{{
		Name: "b",
		Members: []BoundaryMember{
			globMember("cmd/*/svc"),
			globMember("cmd/foo/*"),
		},
	}}}
	_, err := rs.BoundaryMembership("cmd/foo/svc")
	if err == nil {
		t.Fatal("equal-specificity overlap must be an ambiguity error")
	}
	var amb *BoundaryAmbiguityError
	if !errors.As(err, &amb) {
		t.Fatalf("want *BoundaryAmbiguityError, got %T: %v", err, err)
	}
	if amb.Boundary != "b" || len(amb.Members) != 2 {
		t.Errorf("ambiguity error = %+v, want boundary b with two members", amb)
	}
}

func TestBoundaryMembershipComposableAcrossBoundaries(t *testing.T) {
	// Overlap ACROSS boundaries is fine: the same package is a member of both.
	rs := &RuleSet{Boundaries: []Boundary{
		{Name: "a", Members: []BoundaryMember{globMember("cmd/query-ce/**"), globMember("cmd/other/**")}},
		{Name: "b", Members: []BoundaryMember{globMember("cmd/**")}},
	}}
	m, err := rs.BoundaryMembership("cmd/query-ce/x")
	if err != nil {
		t.Fatalf("cross-boundary overlap must not error: %v", err)
	}
	if m[0] != 0 || m[1] != 0 {
		t.Errorf("package should be a member of both boundaries: %v", m)
	}
}

func TestDecideBoundary(t *testing.T) {
	sealed := &RuleSet{Boundaries: []Boundary{{
		Name:   "cmd-services",
		Sealed: true,
		Members: []BoundaryMember{
			globMember("cmd/comparator/**"),
			globMember("cmd/query-ce/**"),
		},
	}}}
	cases := []struct {
		name        string
		from, to    string
		wantAllowed bool
		wantSealed  bool
	}{
		{"cross-member", "cmd/comparator/x", "cmd/query-ce/y", false, false},
		{"in-member", "cmd/query-ce/a", "cmd/query-ce/b", true, false},
		{"member-to-ungrouped", "cmd/query-ce/a", "internal/shared", true, false},
		{"ungrouped-to-member-sealed", "internal/shared", "cmd/query-ce/a", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			allowed, _, isSealed, err := sealed.DecideBoundary(c.from, c.to)
			if err != nil {
				t.Fatal(err)
			}
			if allowed != c.wantAllowed || isSealed != c.wantSealed {
				t.Errorf("DecideBoundary(%q,%q) = allowed %v sealed %v, want %v/%v",
					c.from, c.to, allowed, isSealed, c.wantAllowed, c.wantSealed)
			}
		})
	}
}
