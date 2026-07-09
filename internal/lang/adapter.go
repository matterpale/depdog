package lang

// Adapter is the registration descriptor for one language: everything the CLI
// needs to detect a project, locate its root, and construct a Loader. The
// analysis itself lives behind Loader (New returns one); this struct is only the
// wiring that lets the CLI treat every language uniformly.
//
// To add a language, three edits and nothing else:
//
//  1. Implement Loader in a sibling package internal/lang/<name>.
//  2. Add one Adapter entry to the registry in internal/cli/languages.go.
//  3. Add a self-check rule for the new package to depdog.yaml, and ship the
//     usual tests + fixtures.
//
// internal/core, internal/config, and the reporters never change: they operate
// on the language-neutral core.Graph the Loader produces.
type Adapter struct {
	// Name is the language's identifier — the --lang value and the label used in
	// errors, e.g. "go" or "ts".
	Name string

	// Markers are the project-root marker files, in priority order. The nearest
	// directory (walking up from the working directory) that holds one of these
	// identifies the project; auto-detection compares markers across adapters and
	// the nearest wins.
	Markers []string

	// Root optionally resolves the project root with language-specific rules
	// (e.g. Go's single-module / no-workspace refusal). When nil, the nearest
	// ancestor directory containing a Marker (in priority order) is the root.
	Root func(startDir string) (string, error)

	// New constructs the Loader rooted at the resolved project directory.
	New func(root string) Loader
}
