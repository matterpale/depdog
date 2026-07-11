package elm

// stdlibModules is the set of module names shipped by elm/core — Elm's standard
// library, always available without an `elm.json` dependency entry. A module
// import whose fully-qualified name is in this set (and that is not resolved as
// an in-module file) classifies as std; anything else that is not an on-disk
// module under a source directory is external (it comes from an elm.json
// dependency package).
//
// The set is the exact `exposed-modules` of elm/core (verified against
// elm/core's own elm.json). Note the deliberate exclusions: `Json.Decode` /
// `Json.Encode` ship with elm/json, and `Html` with elm/html — those are
// dependency packages, so depdog classifies them as external, not std.
//
// The set is fixed rather than probed from an installed Elm so the scanner stays
// a pure-Go, runtime-free static analysis (no elm toolchain dependency).
var stdlibModules = map[string]bool{
	// Primitives.
	"Basics":  true,
	"String":  true,
	"Char":    true,
	"Bitwise": true,
	"Tuple":   true,

	// Collections.
	"List":  true,
	"Dict":  true,
	"Set":   true,
	"Array": true,

	// Error handling.
	"Maybe":  true,
	"Result": true,

	// Debug.
	"Debug": true,

	// Effects.
	"Platform":     true,
	"Platform.Cmd": true,
	"Platform.Sub": true,
	"Process":      true,
	"Task":         true,
}

// isStdlib reports whether a fully-qualified Elm module name is part of elm/core.
// Matching is exact on the whole dotted name: `Platform.Cmd` is std, but a
// project module named `Platform.Internal` is not (it is not an elm/core module,
// so it degrades to in-module or external elsewhere).
func isStdlib(module string) bool {
	return stdlibModules[module]
}
