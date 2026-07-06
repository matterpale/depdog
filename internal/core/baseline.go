package core

import "sort"

// BaselineEntry identifies a tolerated violation by its source package and the
// path it imports. Line numbers are deliberately excluded so that moving code
// around does not invalidate a baseline.
type BaselineEntry struct {
	FromPackage string
	Import      string
}

// Baseline is a set of grandfathered violations. `check --fail-on new` fails
// only on violations absent from it; `baseline` records the current set.
// Entries are kept sorted for deterministic serialization.
type Baseline struct {
	Entries []BaselineEntry
}

// BaselineFrom captures the violations of a result as a baseline, de-duplicated
// by (source package, import) and sorted.
func BaselineFrom(res *Result) *Baseline {
	seen := make(map[BaselineEntry]bool, len(res.Violations))
	b := &Baseline{}
	for _, v := range res.Violations {
		e := BaselineEntry{FromPackage: v.FromPackage, Import: v.ImportPath}
		if seen[e] {
			continue
		}
		seen[e] = true
		b.Entries = append(b.Entries, e)
	}
	b.Sort()
	return b
}

// Sort orders entries by source package then import, so serialized baselines
// diff cleanly.
func (b *Baseline) Sort() {
	sort.Slice(b.Entries, func(i, j int) bool {
		if b.Entries[i].FromPackage != b.Entries[j].FromPackage {
			return b.Entries[i].FromPackage < b.Entries[j].FromPackage
		}
		return b.Entries[i].Import < b.Entries[j].Import
	})
}

// Filter returns a copy of res that keeps only violations absent from the
// baseline, together with the number suppressed. Warnings and stats are
// carried over unchanged.
func (b *Baseline) Filter(res *Result) (*Result, int) {
	set := make(map[BaselineEntry]bool, len(b.Entries))
	for _, e := range b.Entries {
		set[e] = true
	}
	out := *res
	out.Violations = nil
	suppressed := 0
	for _, v := range res.Violations {
		if set[BaselineEntry{FromPackage: v.FromPackage, Import: v.ImportPath}] {
			suppressed++
			continue
		}
		out.Violations = append(out.Violations, v)
	}
	return &out, suppressed
}
