// Package goodlib stands in for a permitted third-party dependency — the
// counterpart to extlib. The extdeny fixture allows any external module but
// blocks extlib by name, so goodlib passes while extlib is flagged. Like
// extlib it is referenced through a local replace directive so tests run
// offline.
package goodlib

func OK() bool { return true }
