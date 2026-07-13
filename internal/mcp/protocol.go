package mcp

import "encoding/json"

// protocolVersion is the MCP revision depdog implements. On initialize the
// server echoes the client's requested version when it can serve it, else
// falls back to this one (see negotiateVersion).
const protocolVersion = "2025-06-18"

// initializeResult is the subset of the MCP InitializeResult depdog serves.
// capabilities advertises the two feature groups this server supports —
// tools and resources — as empty objects (no sub-capabilities like
// listChanged, since the sets are static for a session).
type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
}

// serverCapabilities advertises the feature groups. Empty objects are
// intentional and must serialize as `{}`, not be omitted: the presence of the
// key is what signals the capability.
type serverCapabilities struct {
	Tools     map[string]any `json:"tools"`
	Resources map[string]any `json:"resources"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeParams is the slice of the client's initialize payload the server
// reads: the protocol version it wants to speak. Everything else is ignored.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

// tool describes one callable tool for tools/list. InputSchema is a JSON
// Schema object describing the tool's arguments.
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// toolsListResult is the tools/list response payload.
type toolsListResult struct {
	Tools []tool `json:"tools"`
}

// resource describes one readable resource for resources/list.
type resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// resourcesListResult is the resources/list response payload.
type resourcesListResult struct {
	Resources []resource `json:"resources"`
}

// callToolParams is the tools/call request payload.
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// textContent is one content block; MCP tool results are a list of these.
// depdog only ever returns text blocks carrying JSON.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// callToolResult is the tools/call response payload. IsError flags a
// tool-level failure (bad params, unresolvable ref, check error) — the
// content then carries the human-readable message rather than a JSON result.
type callToolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// readResourceParams is the resources/read request payload.
type readResourceParams struct {
	URI string `json:"uri"`
}

// resourceContents is one resource content block returned by resources/read.
type resourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// readResourceResult is the resources/read response payload.
type readResourceResult struct {
	Contents []resourceContents `json:"contents"`
}

// Resource URIs advertised by resources/list and accepted by resources/read.
const (
	uriConfig     = "depdog://config"
	uriComponents = "depdog://components"
	mimeJSON      = "application/json"
)

// negotiateVersion picks the protocol version to advertise: the client's
// requested version when the server can serve it, else the server's own. The
// server serves exactly one version, so "can serve it" means an exact match.
func negotiateVersion(requested string) string {
	if requested == protocolVersion {
		return requested
	}
	return protocolVersion
}

// serverTools is the static tool catalogue advertised by tools/list and
// accepted by tools/call. Schemas are hand-written JSON so the required-field
// shape matches the MCP spec exactly.
var serverTools = []tool{
	{
		Name:        "check",
		Description: "Check a project's import edges against its depdog.yaml architecture rules and return the violations as JSON. With no path, checks the project resolved from the server's working directory (or --config). Set all to fan out across every discovered language project.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"project path to check (default: the project at the server's working directory)"},"all":{"type":"boolean","description":"check every discovered language project instead of a single one"}},"additionalProperties":false}`),
	},
	{
		Name:        "explain",
		Description: "Explain the verdict for one import edge (from -> to): whether it is allowed, the deciding rule or boundary, and the file:line when the edge exists in the graph. Mirrors `depdog explain`; from must be a package.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"from":{"type":"string","description":"the importing package: a module-relative dir or its trailing path segment (a component name is not accepted here — use can_import for that)"},"to":{"type":"string","description":"the imported package, component, group, std, external, or module ref"}},"required":["from","to"],"additionalProperties":false}`),
	},
	{
		Name:        "can_import",
		Description: "Cheap in-loop pre-check: may `from` import `to`? Answered from the compiled rule set only (no graph scan), returning the verdict and the deciding rule/boundary.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"from":{"type":"string","description":"the importing package (module-relative dir) or a component name"},"to":{"type":"string","description":"the imported package, component, group, std, external, or module ref"}},"required":["from","to"],"additionalProperties":false}`),
	},
}

// serverResources is the static resource catalogue advertised by
// resources/list and accepted by resources/read.
var serverResources = []resource{
	{
		URI:         uriConfig,
		Name:        "config",
		Description: "The compiled depdog rule set for the resolved project (the `depdog config` data) as JSON.",
		MimeType:    mimeJSON,
	},
	{
		URI:         uriComponents,
		Name:        "components",
		Description: "The component list with its path patterns and inferred stance, as JSON.",
		MimeType:    mimeJSON,
	},
}
