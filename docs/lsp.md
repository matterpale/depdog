# LSP integration — inline architecture diagnostics

Status: **increments lsp-01 through lsp-05 shipped** (2026-07-10; lsp-05's
Marketplace publish is owner-gated and deliberately not done).
`depdog lsp` is a Language Server Protocol server over stdio that runs an
architecture check when the editor finishes initializing and again on every
save, publishes every rule violation as an inline diagnostic at its import
statement, clears stale diagnostics from files that went clean, and answers
`textDocument/hover` on any import line with the `depdog explain` verdict for
that edge. When the client can watch files (dynamic registration of
`workspace/didChangeWatchedFiles`), external edits to `depdog.yaml` — git
checkout, terminal edits, `depdog baseline` — trigger a re-check too. This
document is the design of record for the protocol choice, the
Violation→Diagnostic mapping, the hover design, the config-watching design and
the increment roadmap.

## Goal

Any LSP-capable editor (VS Code, Neovim, Helix, Zed, Emacs/eglot, …) should be
able to point a generic LSP client at `depdog lsp` and get squiggles on the
exact `import` lines that break `depdog.yaml` — the same violations `depdog
check` prints, at the same `file:line`, for every language adapter, with zero
editor-specific code on our side.

## Protocol choice: hand-rolled, std-only JSON-RPC 2.0 over stdio

The server implements the LSP base protocol itself: JSON-RPC 2.0 messages
framed as `Content-Length: N\r\n\r\n<json>` on stdin/stdout
(`internal/lsp/jsonrpc.go`, ~130 lines with `bufio` + `encoding/json`).
Header names are parsed case-insensitively; `Content-Type` is accepted and
ignored. Unknown **request** methods get a JSON-RPC `MethodNotFound` (-32601)
error response; unknown **notifications** are silently ignored — both per the
LSP spec.

### Rejected alternatives

- **`go.lsp.dev/protocol`** — the full typed protocol surface, but it drags a
  large transitive tree (including `go.uber.org/zap` and `segmentio/encoding`)
  into a module whose dependency posture is deliberately tight, and the
  project is semi-dormant. We need ~6 methods; the types we actually consume
  are a few dozen lines of structs.
- **`tliron/glsp`** — a full server framework with its own handler model and
  its own dependency tree. Same mismatch: far more surface than the slice of
  LSP this server speaks, in exchange for go.mod entries we'd carry forever.

The deciding facts: `internal/core` is std-only (dogfood-enforced), all seven
language adapters are pure Go, and the framing codec is ~100 LOC of
well-specified protocol. Hand-rolling keeps `go.mod` untouched and makes the
constraint machine-enforceable — `depdog.yaml` now declares
`lsp: { path: "internal/lsp/**", allow: [core, std] }`, so CI fails if the
package ever grows a third-party import, exactly like `core`.

## Architecture

```
internal/lsp            std + internal/core ONLY
├── jsonrpc.go          framing codec, message/error types, JSON-RPC codes
├── server.go           Server, Serve loop, lifecycle methods
├── diagnostics.go      core.Result → publishDiagnostics payloads
└── hover.go            file→edge index, verdictFor, hover handler

internal/cli/lsp.go     cobra command; builds the CheckFunc closure
```

The seam is an injected check function returning a full check snapshot:

```go
type Check struct {
    Result *core.Result
    Graph  *core.Graph
    Rules  *core.RuleSet
    Root   string // absolute project root Positions are relative to
}
type CheckFunc func(ctx context.Context) (*Check, error)
```

`internal/cli/lsp.go` closes over the existing `evaluateModule` (same
`--config` flag and persistent `--lang` as every other command; `Result`,
`Graph` and `Rules` straight from the evaluation, `Root` derived from
`filepath.Dir(ev.ConfigPath)`), so `internal/lsp` never imports
`internal/cli` or `internal/config` and stays trivially testable with a stub.
The snapshot was widened for lsp-03 (it used to be `(*core.Result, string,
error)`): hover needs the graph and rule set, and carrying them in **one**
snapshot — instead of injecting a second explain function — guarantees hover
and diagnostics always describe the same check round.

### Lifecycle

| Client message  | Server behavior |
|-----------------|-----------------|
| `initialize`    | capture `rootUri`/`rootPath` if provided (else the snapshot's `Check.Root` is used) and whether the client supports **dynamic registration of `workspace/didChangeWatchedFiles`**; reply with capabilities (`textDocumentSync: { openClose: true, change: 0, save: { includeText: false } }` — save interest so clients deliver `didSave`, but contents are never tracked — plus `hoverProvider: true`) and `serverInfo{name: "depdog", version: cli.Version}` |
| `initialized`   | **if** the client announced `workspace.didChangeWatchedFiles.dynamicRegistration`, send exactly one `client/registerCapability` **request** (server-issued id `"depdog-watch-1"`) registering a watcher for `**/<config basename>` — before the first round, so the watch covers the whole session; then run CheckFunc, publish the first diagnostics round, keep the snapshot for hover. Clients without the capability get no request and see byte-identical pre-lsp-04 behavior |
| response to a server→client request | a message with an **id but no method** is a *response*, not a request: it is matched against the pending registration id and **consumed, never answered** (routing it into the request branch would bounce a bogus `MethodNotFound` at the client). A refused registration is logged to stderr and the server degrades gracefully to didSave-only re-checks |
| `workspace/didChangeWatchedFiles` | the client saw the watched config file change (created/changed/deleted — outside the editor too): re-run CheckFunc and publish a fresh round via the same path as `didSave`, with identical failure semantics. However many `FileEvents` one notification carries, they **coalesce into exactly one re-check** — the check re-reads the whole project from disk anyway |
| `textDocument/didSave` | re-run CheckFunc **synchronously** (no debouncing — saves are discrete and the loop is single-goroutine) and publish a fresh round; the saved URI/text are ignored beyond a log line, since the check re-reads the whole project from disk |
| `textDocument/hover` | answer from the last successful snapshot's file→edge index (see **Hover** below); `null` when there is nothing to say |
| `textDocument/didOpen` / `didChange` / `didClose` | ignored — document contents are never tracked |
| `shutdown`      | reply `null`, mark the session done |
| `exit`          | return from `Serve` — `nil` after `shutdown`, an error otherwise (the CLI then exits non-zero, as the spec prescribes) |
| unknown request | `MethodNotFound` (-32601) error response |
| unknown notification | ignored |

**Stale clearing (lsp-02).** The serve loop keeps a session-local set of the
URIs that received non-empty diagnostics in the previous round. After each
round's check, every URI in that set that is clean now gets a
`publishDiagnostics` notification whose `diagnostics` field is an **empty JSON
array** — `[]`, never `null` (a nil Go slice would marshal to `null`, which
some clients choke on); clients only un-squiggle on an explicit empty publish.
Fresh and clearing notifications are emitted in one sorted-by-URI pass over
the union, and files clean in both rounds get nothing.

**Failure semantics.** A check that fails during a `didSave` or
`didChangeWatchedFiles` round (e.g. a transiently broken `depdog.yaml`
mid-edit) is logged to stderr and publishes
nothing — the previous diagnostics stay visible, the session stays alive, and
the previous-round URI set **and hover snapshot** are kept **unchanged**, so
hover keeps answering from the last good round and the next successful round
still clears whatever became stale in the meantime.

The serve loop is single-goroutine by design: messages are handled in arrival
order and notifications are emitted sorted, so a session transcript is
deterministic — the tests replay whole sessions byte-for-byte. All logging
goes to **stderr**; stdout carries protocol frames only. A frame whose JSON
body is unparseable gets a `ParseError` (-32700) response and the loop
continues (framing is still intact); a broken frame header is unrecoverable
and ends the session with an actionable error. A failing check (bad config,
adapter error) is logged to stderr and publishes nothing — the session stays
alive so the user can fix the config and save again.

## Violation → Diagnostic mapping

Precedent: `internal/report/sarif.go`, which emits `URI = Position.File`
(module-root-relative) and `StartLine = Position.Line` (1-based).

- **Grouping** — violations are grouped by `Position.File`; one
  `textDocument/publishDiagnostics` notification per file.
- **URI** — `file://` + `filepath.ToSlash(filepath.Join(root, p.File))`,
  where root is the client's `rootUri`/`rootPath` when announced, else the
  directory of the resolved `depdog.yaml`.
- **Range** — LSP positions are 0-based: `start = end = {line: p.Line - 1,
  character: 0}`. **No-column limitation:** `core.Position` carries file and
  line only, so the squiggle is line-level (character 0). Extending adapters
  to record the import token's column is future work, deliberately out of
  this increment.
- **Severity** — always `1` (Error), including `TestOnly` violations: if the
  `test_files` policy lets a test-only edge through it never becomes a
  `Violation`, and if it doesn't, the edge fails `depdog check` like any
  other, so downgrading it in the editor would misreport CI. (`TestOnly` is
  still visible: the message gains a ` [test]` suffix, matching the text
  report.)
- **Source** — `"depdog"`. **Code** — the fired `Violation.Rule` (e.g.
  `domain: allow [std]`).
- **Message** — `<FromComponent> imports <ImportPath> (<Target>): <Rule>`,
  reusing the text/SARIF report vocabulary.
- **Determinism** — notifications sorted by URI, diagnostics by line (then
  message).
- **Out of scope** — `core.Warnings` (unassigned packages etc.) carry no
  positions, so they are not mapped in this increment.
- **`relatedInformation`** — every diagnostic (and, in hover's markdown, a
  trailing link) points back at the resolved `depdog.yaml`, giving editors a
  clickable jump to the config — the LSP-surface analogue of the TUI config
  tab's `e` (open the config in `$EDITOR`). The location is always line 0:
  `config.Parse` discards yaml line numbers for components/rules, so this
  opens the file rather than jumping to the exact rule; the `message` field
  carries the rule text (`"rule: <Violation.Rule>"`) as a substitute for a
  precise jump target. Omitted (no `relatedInformation` key) when the server
  has no config basename to link (`configBase == ""`).

## Hover: `depdog explain`, inline (lsp-03)

Hovering **any** import line — violating or clean — shows the same verdict
`depdog explain <from> <to>` prints: which rule or boundary allows or denies
that exact edge.

- **Snapshot seam** — hover answers from the last successful round's `Check`
  snapshot. The alternative (a second injected explain function) was rejected:
  it could re-read the project mid-session and answer from a *different* check
  round than the visible diagnostics. One snapshot means hover and squiggles
  can never disagree.
- **Index** — each successful `publish` round builds a
  `module-relative file → []edge` index from
  `Graph.Packages[].Imports[].Positions`, excluding `skip`ped packages and
  edges into skipped targets (exactly the edges `Evaluate` and
  `BuildPackageViews` judge). Per-file edges are sorted by import path, so a
  line carrying several edges (possible in TypeScript) renders all verdicts in
  one hover, deterministically ordered.
- **URI mapping** — the request URI is mapped back to a module-relative file
  by stripping the session root (the client's `rootUri`/`rootPath` when
  announced, else `Check.Root`) — the exact inverse of how diagnostics URIs
  are built. The hover *character* is ignored: positions are line-granular,
  the same no-column limitation as diagnostics, and the returned range spans
  the hovered line at character 0.
- **Verdict** — `verdictFor` in `hover.go` mirrors `report.ExplainEdge`
  through the same core primitives (`AssignComponent`, then boundary-first
  `DecideBoundary` — a boundary deny wins over any component allow — then
  `Decide` for components/std or `DecideModule` for a concrete external
  module), reusing the explain vocabulary (`allowed by …` / `denied by …`,
  `denied by boundary "name" (sealed)`, unassigned sources have no governing
  rule) so the editor and the CLI never diverge in wording. `internal/lsp`
  still imports **std + internal/core only** — `internal/report` is a text
  renderer around the same primitives and stays out.
- **Payload** — `{contents: {kind: "markdown", value}, range}`; the value is
  one block per edge: a `**depdog** — from (component) → import (target)`
  header, a blank line, then the verdict, with a ` [test]` suffix on
  `TestOnly` edges (matching the diagnostics message), plus a trailing
  `[depdog.yaml](file://…)` link block (the markdown equivalent of a
  diagnostic's `relatedInformation`, above) — omitted under the same
  `configBase == ""` condition.
- **Null semantics** — `result: null` (legal for hover) for: no successful
  check round yet, a non-`file:` URI or one outside the root, a file the index
  does not know, or a line without an import. A failed re-check keeps the
  previous snapshot, so hover keeps working while `depdog.yaml` is mid-edit.

## Increment roadmap

- **lsp-01 (shipped)** — stdio server, one check per session, initial
  diagnostics.
- **lsp-02 (shipped)** — re-check on `textDocument/didSave`; clear stale
  diagnostics by publishing empty arrays (`[]`, never `null`) for files that
  were dirty last round and clean now; a failing check keeps the previous
  diagnostics and the stale set intact.
- **lsp-03 (shipped)** — `textDocument/hover` backed by the core decision
  primitives (`Decide`/`DecideModule`/`DecideBoundary`): `depdog explain`,
  inline, answered from the same snapshot as the diagnostics.
- **lsp-04 (shipped)** — re-check on external `depdog.yaml` changes via
  `workspace/didChangeWatchedFiles`. Std-only by design: no fsnotify — the
  *client* watches, which the LSP spec only offers through dynamic
  registration, so the server sends its single server→client request
  (`client/registerCapability`) when the client announces
  `workspace.didChangeWatchedFiles.dynamicRegistration` in `initialize`. The
  config basename is threaded from `internal/cli/lsp.go` into `NewServer`
  (an explicit `--config` names it; discovery only ever resolves the default
  `depdog.yaml`). **didSave-only clients remain fully supported**: without
  the capability no registration is sent and behavior is byte-identical to
  lsp-03 — there is deliberately **no polling fallback** (complexity without
  a proven client that lacks both `didSave` and file watching; VS Code,
  Neovim ≥0.10 and Helix ≥23.10 all support dynamic watcher registration).
- **lsp-05 (shipped)** — editor packaging: [docs/editors.md](editors.md), a
  per-editor setup guide (Neovim ≥0.10, Helix ≥23.10, VS Code, Zed, Emacs
  eglot) covering all seven adapter languages, every snippet explicitly
  marked validated (and how) or untested; plus `editors/vscode/`, a thin
  **unpublished** VS Code extension scaffold (~40-line `extension.js` around
  `vscode-languageclient`, document selector for the seven languages, a
  `depdog.yaml` watcher as belt-and-braces beside lsp-04's dynamic
  registration) verified to build locally (`npm install`, `node --check`,
  `npx @vscode/vsce package` → `.vsix`; artifacts gitignored).
  **Marketplace publishing is owner-gated** — it needs the owner's publisher
  account and a PAT, exactly like the Homebrew tap precedent — so the
  scaffold documents the manual `code --install-extension depdog-*.vsix`
  path instead and its `publisher` field stays a placeholder.
