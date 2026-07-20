# MCP integration — depdog as an in-loop agent tool

Status: **shipped** (2026-07-13). `depdog mcp` is a read-only [Model Context
Protocol](https://modelcontextprotocol.io) server over stdio so any MCP-capable
agent (Claude, Cursor, …) can consult the architecture *in the loop*, not just
as a post-hoc CI gate. It exposes three tools — `check`, `explain`,
`can_import` — and two resources — `depdog://config`, `depdog://components` —
over JSON-RPC 2.0 with `Content-Length` framing, exactly like `depdog lsp`.
Protocol version `2025-06-18`. It is read-only: no rule mutation over MCP.

Architecturally `internal/mcp` is a *pure protocol layer* (`allow [core, std]`,
dogfood-enforced, no MCP SDK), mirroring `internal/lsp`. The CLI
(`internal/cli/mcp.go`) injects closures that do config discovery, adapter
selection, graph loading, evaluation and JSON rendering, so the protocol layer
never imports `config`/`lang`/`report`. Every payload is produced by capability
that already exists — `core.RuleSet.Decide`/`DecideModule`/`DecideBoundary`, the
`evaluate*` machinery and the JSON reporter — this is a thin adapter, not new
engine work.

## Why MCP (the honest cost/benefit)

An agent can already shell out to the `depdog` CLI and parse `--format json`,
which is arguably *more* efficient than MCP: no server lifecycle, no protocol
overhead. MCP's value is **not** speed. It is:

1. **A discoverable, self-describing surface.** `tools/list` hands the agent the
   tool names, descriptions and JSON Schemas, so it can call `check`/`explain`/
   `can_import` *without being told the flags* — no prompt engineering the CLI
   invocation.
2. **A low-cost, credible signal that depdog is agent-native** — a positioning
   asset (no competitor, go-arch-lint included, has one).

Ship it because it is cheap and asserts the claim, not because it beats the CLI.
For pure throughput, shelling out to `depdog check --format json` is still fine.
This surface pairs with the procedural [`skills/depdog/SKILL.md`](../skills/depdog/SKILL.md)
and the [JSON Schema](../schema/depdog.schema.json).

## Tools

All three tools are **read-only** and return a single MCP `text` content block
whose text is a JSON document. A tool-level failure (bad params, an unresolvable
ref, a config load error) comes back as a result with `isError: true` carrying a
human-readable message — never a transport-level crash. Project resolution is
the same as `depdog lsp`: the server's working directory, or an explicit
`--config <path>`.

### `check` — violations as JSON

Inputs `{ "path"?: string, "all"?: boolean }`. With no `path`, checks the
project resolved from the server's working directory (or `--config`); `all: true`
fans out across every discovered language project. The output is the exact
`--format json` payload — the single-module report, or the workspace envelope
when the run spans multiple members (identical to `depdog check --format json` /
`--all --format json`). At a root holding a
[`depdog.work.yaml`](cross-language.md), `all: true` runs the cross-unit pass
too and the envelope carries the additive `cross_unit` block.

Single-project shape (trimmed):

```json
{
  "module": "example.test/dirty",
  "default": "deny",
  "violations": [
    {
      "from_package": "example.test/dirty/internal/domain/pricing",
      "from_component": "domain",
      "import": "example.test/dirty/internal/repository",
      "target": "repository",
      "rule": "domain: allow [std]",
      "test_only": false,
      "positions": [
        { "file": "internal/domain/pricing/discount.go", "line": 3 },
        { "file": "internal/domain/pricing/pricing.go", "line": 5 }
      ]
    }
  ],
  "warnings": [ … ],
  "components": [ … ],
  "boundaries": [],
  "cycles": [],
  "stats": { "packages": 6, "edges": 14, "duration_ms": 0 }
}
```

### `explain` — one edge, with positions

Inputs `{ "from": string, "to": string }` (both required). Mirrors `depdog
explain`: the verdict, the deciding rule or boundary, and — because `explain`
**loads the graph** — the `file:line` positions of any offending edge that
already exists in the tree. `from` must be a **package** (a module-relative dir
or its trailing path segment), exactly as `depdog explain` requires — a bare
component name is not accepted here (use `can_import` for a component-level
question). `to` may be a package, component, alias, `std`, `external`, or a
module ref.

```json
{
  "from": "example.test/dirty/internal/domain/pricing",
  "from_component": "domain",
  "to": "internal/repository",
  "target": "repository",
  "allowed": false,
  "decided_by": "rule",
  "reason": "domain: allow [std]",
  "explanation": "`example.test/dirty/internal/domain/pricing` (component `domain`) may import only `std`; `repository` (component `repository`) is not among them. Fix: add `repository` to `domain`'s allow list, or depend only on what `domain` already allows.",
  "positions": [
    { "file": "internal/domain/pricing/discount.go", "line": 3 },
    { "file": "internal/domain/pricing/pricing.go", "line": 5 }
  ]
}
```

`boundary` and `sealed` fields appear when a boundary decides the edge;
`positions` is omitted when the edge is not present in the graph. `explanation`
is a plain-English WHY-plus-fix for a **denied** edge (omitted when the edge is
allowed); the machine-readable `reason`/`decided_by` stay the fields to branch
on.

### `can_import` — cheap in-loop pre-check

Inputs `{ "from": string, "to": string }` (both required). Answers "may `from`
import `to`?" from the **compiled rule set only — no graph scan**, so it is the
cheap pre-check an agent can call before writing an import. Unlike `explain`,
`from` may be either a package (module-relative dir) **or a component name** —
a component name is classified into its boundary membership from its pattern, so
a sealed/mutual-exclusion boundary is honored for component-level questions too.
The deliberate distinction from `explain`: because no graph is loaded,
`can_import` returns **no `positions`** — it is the rule-set verdict, not a scan
of what is actually imported today.

```json
{
  "from": "internal/domain/pricing",
  "to": "internal/repository",
  "allowed": false,
  "decided_by": "rule",
  "reason": "domain: allow [std]",
  "explanation": "`internal/domain/pricing` (component `domain`) may import only `std`; `repository` (component `repository`) is not among them. Fix: add `repository` to `domain`'s allow list, or depend only on what `domain` already allows."
}
```

`decided_by` is one of `rule`, `boundary` or `policy` (an unassigned `from` that
no component governs is allowed by policy). A denied edge also carries an
`explanation` (the same plain-English WHY-plus-fix `explain` returns); an allowed
edge looks the same with `"allowed": true` and no `explanation`.

## Resources

Both resources are read-only and serve `application/json`.

- **`depdog://config`** — the compiled rule set for the resolved project (the
  machine-readable form of the `depdog config` dump): the `default` policy, the
  resolved `test_files` option and any `skip` globs, every component with its
  patterns / inferred stance / allow / deny, and any boundaries.
- **`depdog://components`** — just the component list: each component's `name`,
  inferred `stance`, path `patterns` and `allow`/`deny` refs.

`depdog://components` shape (trimmed):

```json
{
  "components": [
    {
      "name": "domain",
      "stance": "whitelist",
      "patterns": ["internal/domain/**"],
      "allow": ["std"]
    }
  ]
}
```

## Wiring `depdog mcp` into an MCP client

The transport is JSON-RPC 2.0 over stdio: **stdout carries only protocol frames,
all logs go to stderr.** Register `depdog mcp` as a stdio server — the binary
must be on the client's `PATH` (`go install
github.com/matterpale/depdog/cmd/depdog@latest`, or a release build).

Most clients (Claude Desktop, Cursor, and other `mcpServers`-style configs) take
a JSON block like:

```json
{
  "mcpServers": {
    "depdog": {
      "command": "depdog",
      "args": ["mcp"]
    }
  }
}
```

Pin a specific config with `--config` — the server otherwise resolves the
project from its working directory:

```json
{
  "mcpServers": {
    "depdog": {
      "command": "depdog",
      "args": ["mcp", "--config", "path/to/depdog.yaml"]
    }
  }
}
```

The server speaks `initialize` (advertising `protocolVersion 2025-06-18`,
`capabilities.tools`, `capabilities.resources` and `serverInfo`),
`notifications/initialized`, `tools/list`, `tools/call`, `resources/list`,
`resources/read` and `ping`, and shuts down cleanly when the client closes
stdin. Unknown methods and bad params come back as JSON-RPC error responses with
actionable messages, mirroring the CLI's exit-2 wording; the server never
crashes on bad input.

## Read-only by design

v1 is read-only: no rule mutation over MCP. An agent silently rewriting its own
guardrails is a footgun — authoring stays human, via `$EDITOR`, `depdog init`,
or the TUI's visual rule editor. An agent uses this surface to *consult* the
architecture (is this edge allowed? what does the config say?) and to *author*
`depdog.yaml` in its own editor, validating with `check`/`explain`/`config` —
not to edit the guardrails through the protocol.
