// Package extlib stands in for a third-party dependency; the fixture
// modules reference it through a local replace directive so tests run
// offline.
package extlib

func Answer() int { return 42 }
