// Package shared is a well-behaved shared lib: importable by every service,
// depends on nothing under cmd/.
package shared

func Answer() int { return 42 }
