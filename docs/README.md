# depdog documentation

The [main README](../README.md) is the tour: install, quick start, an
annotated config, CI recipes. This page is the layer below it — the mid-level
reference that doesn't fit there — and the pages beside it each go deep on one
topic.

## Topic guides

| Page | Covers |
|------|--------|
| [configuration.md](configuration.md)   | The complete `depdog.yaml` reference: component matching and precedence, the full `allow`/`deny` vocabulary, aliases, the non-blocking signals, test-file handling |
| [boundaries.md](boundaries.md)         | The orthogonal mutual-exclusion axis: members (components or path globs), the verdict table, `sealed` one-way walls |
| [languages.md](languages.md)           | Adapter auto-detection from marker files, the `--lang` override, the `lang:` config key, two-language ambiguity |
| [monorepo.md](monorepo.md)             | The per-unit fan-out: `go.work` mode, `--all` polyglot discovery, `--unit`/`--module` narrowing, advisory skips |
| [cross-language.md](cross-language.md) | `depdog.work.yaml`: governing dependency edges *between* units, across languages — units, rules, boundaries, surfaces |
| [editors.md](editors.md)               | Wiring `depdog lsp` into Neovim, Helix, VS Code, Zed, GoLand/JetBrains, Emacs |
| [lsp.md](lsp.md)                       | The LSP server's design of record: protocol choice, architecture, violation→diagnostic mapping, hover |
| [mcp.md](mcp.md)                       | The read-only MCP server: `check`/`explain`/`can_import` tools, resources, client wiring |
| [compatibility.md](compatibility.md)   | What 1.0 promises: the stable machine contract vs the freely-evolving presentation |

## How depdog reads your code

Every adapter is a pure-Go static import scanner: it reads source text
(comment- and string-aware) and extracts the import statements — **no language
toolchain, no build**. That is why depdog works mid-refactor, on code that
doesn't compile yet, and on many languages from one binary — where
package-loader-based linters (e.g.
[go-arch-lint](https://github.com/fe3dback/go-arch-lint)) need the language's
own toolchain to load your packages first. The one exception is the Go adapter
itself, which resolves the package graph through `go list` metadata (no
type-checking) — a Go project has the Go toolchain by definition. When `go list`
can't resolve every import (a missing dependency, or code mid-refactor) it
degrades to the same best-effort static scan the other adapters use — parsed
imports plus a path heuristic, with a `depdog: warning:` on stderr — rather than
failing, so the "works mid-refactor" property holds for Go too.

The same language-neutral core is what enables the
[cross-language layer](cross-language.md): one `depdog.work.yaml` at the repo
root governing the edges **between** the units of a mixed monorepo — a `web/`
TS app, a `services/api` Go module, an `ml/` Python package — in a single pass
with one exit code. No other architecture linter (go-arch-lint, deptrac,
dependency-cruiser, import-linter, ArchUnit) governs edges *across* languages.

## CLI flags at a glance

Every command takes `--config <path>` (default: found next to the project
marker) and `--lang go|ts|py|rb|rs|java|kt|scala|elm` (default: auto-detect
from marker files). `depdog <command> --help` is the authoritative list.

| Command  | Flags |
|----------|-------|
| `init`   | `--preset ddd\|hexagonal\|layered\|flat` · `--default deny\|allow` · `--yes` (non-interactive) · `--force` (overwrite) · `--merge` (extend an existing file, preserving comments and formatting) |
| `check`  | `--format text\|json\|github\|sarif` · `--fail-on any\|new` · `--all` (polyglot discovery) · `--unit <dir>` (narrow `--all`) · `--module <path-or-dir>` (narrow a `go.work` fan-out) · `--color auto\|always\|never` |
| `graph`  | `--format dot\|mermaid` · `--level component\|package` · `--violations-only` · `--focus <component>` |
| `diff`   | `--since <ref>` (required) · `--format text\|github\|json` |
| `config` | `--color auto\|always\|never` |

`explain`, `baseline`, `tui`, `lsp` and `mcp` take only the shared flags.

## TUI keys

<kbd>1</kbd>–<kbd>4</kbd> (or <kbd>tab</kbd>) switch between the Dashboard,
Violations, Packages and Config screens; <kbd>?</kbd> shows the full key
legend. The Violations and Packages lists scroll and filter with <kbd>/</kbd>;
<kbd>e</kbd> opens the selection in `$EDITOR` at its file:line and <kbd>r</kbd>
re-runs the check in place.

The Config tab (<kbd>4</kbd>) shows the active config path and the compiled
rule set (the same data as `depdog config`); <kbd>e</kbd> there opens
`depdog.yaml` in `$EDITOR`, and the editor exiting auto-re-runs the check so
the edited rules take effect on every screen. <kbd>m</kbd> opens the
**experimental visual rule editor** on top of it — an adjacency matrix where
rows import columns:

| Key | Action |
|-----|--------|
| <kbd>↑↓←→</kbd>   | move the edit cursor over the grid |
| <kbd>space</kbd>  | toggle the cursored edge: allow → deny → default |
| <kbd>b</kbd>      | boundaries overlay — <kbd>←/→</kbd> pick a member, <kbd>a</kbd> add, <kbd>d</kbd> remove, <kbd>s</kbd> toggle `sealed` |
| <kbd>a</kbd> / <kbd>p</kbd> / <kbd>R</kbd> | add a component · re-path it · rename it (refs follow automatically) |
| <kbd>w</kbd>      | save staged edits to `depdog.yaml` (edits stage in memory until then) |
| <kbd>esc</kbd>    | leave the editor — prompts save/discard when edits are staged |
