// Package core holds depdog's language-agnostic domain model: the import
// graph produced by language adapters, the rule set compiled from the
// project config, and the evaluation that turns both into violations.
//
// This package must depend on the standard library only.
package core

// Position is a source location of an import statement. File is relative to
// the module root.
type Position struct {
	File string
	Line int
}

// Class says what kind of dependency an import resolves to.
type Class int

const (
	ClassStd Class = iota
	ClassExternal
	ClassInModule
)

func (c Class) String() string {
	switch c {
	case ClassStd:
		return "std"
	case ClassExternal:
		return "external"
	default:
		return "in-module"
	}
}

// Import is one outgoing edge of a package.
type Import struct {
	Path     string
	Class    Class
	RelDir   string // module-relative dir of the imported package; ClassInModule only
	TestOnly bool   // the import appears exclusively in _test.go files
	// Positions of the import statement, sorted by file then line.
	Positions []Position
}

// Package is a node of the import graph.
type Package struct {
	ImportPath string
	RelDir     string // module-relative dir, "." for the module root
	Imports    []Import
}

// Graph is what a language adapter hands to Evaluate. Packages and their
// Imports must be sorted (by ImportPath and Path respectively) so that
// evaluation output is deterministic.
type Graph struct {
	ModulePath string
	Packages   []Package
	// LoadWarnings are advisory notes from the adapter's load step — e.g. the Go
	// adapter degraded to approximate classification because `go list` could not
	// resolve every import (a dependency isn't downloaded, or the code is
	// mid-refactor). They never fail a check; the CLI surfaces them to stderr so
	// machine output on stdout stays clean.
	LoadWarnings []string
}
