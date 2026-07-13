package mcp

import "context"

// Handler is the injected read-only work the server dispatches tools and
// resources to. internal/cli supplies it (M2) with closures over config
// discovery, adapter selection, graph loading, evaluation, and JSON
// rendering; this package never learns about those concerns. Every method
// deals in plain types only — strings and byte payloads — so internal/mcp
// stays a pure protocol layer (allow [core, std], dogfood-enforced).
//
// Each method returns the JSON payload (a []byte) an MCP client receives as
// the text of a content block, plus an error. A returned error becomes a
// tool/resource error result, never a transport-level crash: the server keeps
// running. A nil Handler (or a nil method wiring, signalled by returning
// ErrNotWired) makes tools/call and resources/read answer "not wired yet",
// which is the M1 stub state.
type Handler interface {
	// Check runs an architecture check. path is the project path to check
	// ("" resolves from the server cwd/--config); all fans out across every
	// discovered language project. The payload is the `--format json` output.
	Check(ctx context.Context, path string, all bool) ([]byte, error)
	// Explain returns the verdict deciding whether from may import to,
	// matching `depdog explain` (the deciding rule/boundary, file:line when
	// the edge is in the graph).
	Explain(ctx context.Context, from, to string) ([]byte, error)
	// CanImport is the cheap in-loop pre-check: the verdict from the compiled
	// rule set only (no full graph scan) for whether from may import to.
	CanImport(ctx context.Context, from, to string) ([]byte, error)
	// Config returns the compiled rule set as JSON (the `depdog config` data).
	Config(ctx context.Context) ([]byte, error)
	// Components returns the component list (patterns + inferred stance) as JSON.
	Components(ctx context.Context) ([]byte, error)
}
