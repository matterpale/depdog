package core

import (
	"fmt"
	"slices"
	"strings"
)

// Boundary is a mutual-exclusion group: distinct members may not import each
// other. It is orthogonal to component assignment — a package keeps its
// most-specific component and, independently, belongs to every boundary member
// whose region contains it.
type Boundary struct {
	Name    string
	Members []BoundaryMember // sorted by label for determinism
	Sealed  bool             // one-way wall: nothing outside all members may import in
}

// BoundaryMember is one member of a boundary: a named component OR a path glob.
// A component member expands to its component's patterns; a glob member carries
// the glob directly. Label is the display form (the component name or the raw
// glob string).
type BoundaryMember struct {
	Component string   // set when the member is a component name; "" for a glob member
	Patterns  []string // glob patterns to match packages against
	Label     string   // display label: the component name or the raw glob string
}

// BoundaryAmbiguityError reports a package matched by equally specific members
// of the same boundary — the runtime disjointness failure. It mirrors
// AmbiguityError and, like it, is a config/usage error (exit 2), not a
// violation.
type BoundaryAmbiguityError struct {
	Boundary string
	RelDir   string
	Members  []string
}

func (e *BoundaryAmbiguityError) Error() string {
	return fmt.Sprintf("package %q matches members %s of boundary %q equally well — make one member more specific",
		e.RelDir, strings.Join(e.Members, " and "), e.Boundary)
}

// BoundaryMembership resolves, for each boundary in rs.Boundaries (in order),
// which member owns the package at relDir, returning a slice parallel to
// rs.Boundaries: element i is the index of the owning member within boundary i,
// or -1 if the package is in no member of that boundary ("ungrouped" for it).
//
// Within one boundary the most-specific matching member wins, reusing the same
// patternSpecificity / specificity.compare logic as AssignComponent. An
// equal-specificity overlap within a single boundary is the runtime
// disjointness failure and returns a *BoundaryAmbiguityError. Overlap across
// different boundaries is expected (composability) and never an error.
func (rs *RuleSet) BoundaryMembership(relDir string) ([]int, error) {
	if len(rs.Boundaries) == 0 {
		return nil, nil
	}
	out := make([]int, len(rs.Boundaries))
	for bi := range rs.Boundaries {
		b := &rs.Boundaries[bi]
		var (
			found    bool
			best     int
			bestSpec specificity
			ties     []string
		)
		for mi := range b.Members {
			m := &b.Members[mi]
			for _, pat := range m.Patterns {
				ok, err := MatchPattern(pat, relDir)
				if err != nil {
					return nil, fmt.Errorf("boundary %q member %q pattern %q: %w", b.Name, m.Label, pat, err)
				}
				if !ok {
					continue
				}
				spec := patternSpecificity(pat)
				if !found {
					found, best, bestSpec = true, mi, spec
					continue
				}
				switch cmp := spec.compare(bestSpec); {
				case cmp > 0:
					best, bestSpec, ties = mi, spec, nil
				case cmp == 0 && mi != best && !slices.Contains(ties, b.Members[mi].Label):
					ties = append(ties, b.Members[mi].Label)
				}
			}
		}
		if ties != nil {
			return nil, &BoundaryAmbiguityError{
				Boundary: b.Name,
				RelDir:   relDir,
				Members:  append([]string{b.Members[best].Label}, ties...),
			}
		}
		if found {
			out[bi] = best
		} else {
			out[bi] = -1
		}
	}
	return out, nil
}

// DecideBoundary reports whether the package at fromRelDir may import the
// package at toRelDir under the boundaries, returning the first boundary (in
// rs.Boundaries' sorted order) that denies the edge. It is the single decision
// path shared by Evaluate and explain, so the reason `check` flags and the
// reason `explain` reports never drift.
//
// allowed is true when no boundary denies. When a boundary denies, allowed is
// false and the boundary name, its sealed flag, and the reason kind are
// returned. sealed reports whether the deny came from the sealed one-way rule
// (ungrouped source → in-member target) versus a plain cross-member crossing.
func (rs *RuleSet) DecideBoundary(fromRelDir, toRelDir string) (allowed bool, boundary string, sealed bool, err error) {
	if len(rs.Boundaries) == 0 {
		return true, "", false, nil
	}
	src, err := rs.BoundaryMembership(fromRelDir)
	if err != nil {
		return false, "", false, err
	}
	tgt, err := rs.BoundaryMembership(toRelDir)
	if err != nil {
		return false, "", false, err
	}
	for bi := range rs.Boundaries {
		if deny, isSealed := crossesBoundary(rs.Boundaries[bi], src[bi], tgt[bi]); deny {
			return false, rs.Boundaries[bi].Name, isSealed, nil
		}
	}
	return true, "", false, nil
}

// crossesBoundary is the per-boundary verdict for a single edge, given the
// source and target member indices (from BoundaryMembership; -1 == ungrouped).
// deny is true when the boundary forbids the edge; sealed reports whether the
// deny is the sealed one-way rule rather than a plain cross-member crossing.
func crossesBoundary(b Boundary, srcMember, tgtMember int) (deny bool, sealed bool) {
	switch {
	case srcMember >= 0 && tgtMember >= 0 && srcMember != tgtMember:
		// Two distinct members of the same boundary: symmetric exclusion.
		return true, false
	case b.Sealed && srcMember < 0 && tgtMember >= 0:
		// Nothing outside all members may import into a member.
		return true, true
	default:
		// Same member (incl. same package), member → ungrouped, or
		// ungrouped → member on an unsealed boundary: allowed.
		return false, false
	}
}
