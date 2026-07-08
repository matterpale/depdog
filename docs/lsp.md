# depdog LSP — inline architecture diagnostics

Status: **designed, ready to implement** — decisions settled with the owner
2026-07-08. Not yet built. This is the last open item on the M0–M5 backlog.

## Goal

Surface depdog's architecture violations as **inline editor diagnostics** — a
squiggle on the offending `import` line, updated as you work — instead of only
in the CLI/CI. One editor-agnostic Language Server, many editors.

## Where this fits in the architecture

An LSP server is just another **frontend** that consumes the engine, exactly
like the CLI and the TUI (and philosophically like the `internal/lang` seam:
"add a consumer, not engine logic"). It lives in a new `internal/lsp` package
plus a `depdog lsp` subcommand, imports `core` / `config` / `lang` / `report`,
and **never touches `internal/core`**. Dogfood: `depdog.yaml` gains an `lsp`
component (`allow: [core, config, lang, golang, report, std, external]`, like
`cli`) so the self-check keeps passing.

## Decisions (settled 2026-07-08)

1. **Protocol library:** `go.lsp.dev/protocol` (+ `go.lsp.dev/jsonrpc2`) — typed
   LSP types over stdio. An external dependency confined to the `lsp`/CLI layer.
2. **Re-check trigger:** on `textDocument/didSave` **and** on a `depdog.yaml`
   change, debounced (~250 ms). *Not* per-keystroke — a full load is ~50–165 ms
   (per the loader benchmarks), so save-time is the right granularity and mirrors
   the TUI's existing `r` refresh model.
3. **Editor targets:** ship the editor-agnostic server **+ setup docs** for
   Neovim, Helix, VS Code, and JetBrains/GoLand. No bundled/published extension —
   depdog stays a single Go binary with no JS/TS toolchain.
4. **v1 scope:** **diagnostics only** (`textDocument/publishDiagnostics`). Hovers
   and code actions are explicit non-goals for v1 (see below).
5. **Diagnostic granularity (default):** the whole offending import **line** for
   v1; an exact import-token range is a later refinement.

## Behaviour

`depdog lsp` speaks LSP over stdio.

- **initialize:** advertise `textDocumentSync` (open/close + save) and push
  diagnostics; no completion/hover/codeAction capabilities in v1.
- **On `didOpen`/`didSave`** of a file inside the module, and on a `depdog.yaml`
  change: run the pipeline (`config.Find` → `lang.Loader` → `core.Evaluate`),
  map every violation to a `Diagnostic`, and `publishDiagnostics` per affected
  file. Files whose violations cleared get an empty array (squiggles disappear).
- **Severity mapping:** hard violations → `Error`; the never-fatal advisories
  (unmapped packages, dead patterns, component cycles) → `Warning`/`Information`,
  reflecting that they don't fail the build.
- **Message/source:** the human rule string depdog already emits (e.g.
  `denied by boundary "cmd-services" (sealed)`), `source: "depdog"`.
- **Config errors:** a `depdog.yaml` parse failure surfaces via
  `window/showMessage` (and/or a diagnostic on `depdog.yaml`) and the last-good
  diagnostics are kept — the same fail-soft behaviour as the TUI's refresh path.

## Mapping violations → diagnostics

depdog violations are import-edge level and already carry a `Position`
(file + line of the offending import) — the same data the text/JSON/SARIF
renderers use. Group violations by file, publish one diagnostic array per file.
Range = the whole line for v1 (`character 0`..end); a precise token range needs
the import span and is deferred.

## Performance / lifecycle

- Debounce and coalesce re-checks (~250 ms after a save).
- v1 re-runs the loader on save — no caching. If large modules prove slow, a
  later optimisation can cache the graph and re-scan only changed files (out of
  scope for v1).
- Watch `depdog.yaml` via `didChangeWatchedFiles` (or re-read on any save).

## File-level plan

- `internal/cli`: a new `depdog lsp` cobra subcommand that starts the server on
  stdio (no TTY, unlike `tui`).
- `internal/lsp/` (new): `server.go` (jsonrpc2 wiring + lifecycle:
  initialize/initialized/shutdown/exit), `diagnostics.go` (run the pipeline, map
  violations → `protocol.Diagnostic`, debounce, publish/clear), `document.go`
  (track open docs + the module root). Imports `core`/`config`/`lang`/`report`.
- Factor the CLI's load+check path (`evaluateModule`) into a helper both `cli`
  and `lsp` call, rather than duplicating it.
- `depdog.yaml`: add the `lsp` component carve-out; keep the self-check green.
- `go.mod`: adds `go.lsp.dev/protocol` + `go.lsp.dev/jsonrpc2` as direct deps.
- **Docs — an "Editor integration" README section** with per-editor setup. Note
  the client landscape honestly:
  - **Neovim** — built-in LSP client (`vim.lsp.start` / a custom `nvim-lspconfig`
    server), points at `depdog lsp`. No extension needed.
  - **Helix** — a `languages.toml` `language-server` entry. No extension needed.
  - **VS Code** — VS Code cannot attach to an arbitrary LSP without an extension;
    document using a generic-LSP bridge extension configured to launch
    `depdog lsp` (we deliberately don't ship our own extension in v1).
  - **JetBrains / GoLand** — register `depdog lsp` as an external server via the
    **LSP4IJ** plugin (JetBrains has no built-in "attach arbitrary LSP" for end
    users; LSP4IJ is the standard bridge).

## Testing

- **Unit:** violation → `Diagnostic` mapping (range/severity/message), grouping
  by file, clearing resolved diagnostics, advisory severity, debounce logic.
- **Integration:** drive the server with a scripted JSON-RPC stdio session
  against the `dirty` fixture — initialize → didOpen → assert `publishDiagnostics`
  carries the expected violations; edit-to-clean → didSave → assert the array
  clears. A golden of the emitted diagnostics JSON keeps it deterministic.

## Non-goals (v1)

- Hovers ("this package is component X; may import Y") and code actions
  ("add this violation to the baseline") — natural v2 additions.
- Exact import-token ranges (whole line for now).
- A published VS Code extension or JetBrains plugin (use generic LSP clients /
  LSP4IJ).
- Incremental/partial re-analysis (full re-run on save).

## Effort

L. A new frontend plus protocol plumbing and editor setup docs, with **zero
engine changes** — the payoff of the frontend/engine split.
