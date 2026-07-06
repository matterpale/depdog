package core

import "testing"

func TestBaselineFromAndFilter(t *testing.T) {
	res := &Result{
		Violations: []Violation{
			{FromPackage: "m/a", ImportPath: "m/b"},
			{FromPackage: "m/a", ImportPath: "m/c"},
			{FromPackage: "m/d", ImportPath: "m/b"},
		},
		Warnings: []Warning{{Package: "m/w", RelDir: "w"}},
		Stats:    Stats{Packages: 3, Edges: 9},
	}

	b := BaselineFrom(res)
	if len(b.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(b.Entries))
	}
	if b.Entries[0] != (BaselineEntry{FromPackage: "m/a", Import: "m/b"}) {
		t.Errorf("entries not sorted: first = %+v", b.Entries[0])
	}

	// A full baseline suppresses every violation but keeps warnings and stats.
	filtered, suppressed := b.Filter(res)
	if suppressed != 3 || len(filtered.Violations) != 0 {
		t.Errorf("full filter: suppressed=%d remaining=%d", suppressed, len(filtered.Violations))
	}
	if len(filtered.Warnings) != 1 || filtered.Stats.Packages != 3 {
		t.Errorf("warnings/stats not preserved: %+v", filtered)
	}

	// A partial baseline leaves the rest as new violations.
	partial := &Baseline{Entries: []BaselineEntry{{FromPackage: "m/a", Import: "m/b"}}}
	f2, s2 := partial.Filter(res)
	if s2 != 1 || len(f2.Violations) != 2 {
		t.Errorf("partial filter: suppressed=%d remaining=%d", s2, len(f2.Violations))
	}
}

func TestBaselineFromDeduplicates(t *testing.T) {
	// Two violations sharing (from, import) collapse to one entry.
	res := &Result{Violations: []Violation{
		{FromPackage: "m/a", ImportPath: "m/b", Rule: "x"},
		{FromPackage: "m/a", ImportPath: "m/b", Rule: "y"},
	}}
	if b := BaselineFrom(res); len(b.Entries) != 1 {
		t.Errorf("entries = %d, want 1", len(b.Entries))
	}
}
