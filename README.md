<div align="center">

<img src="assets/logo.svg" alt="depdog" width="330">

**A Codebase Dependency Watchdog** — your architecture rules, enforced on every build.

[![Latest release](https://img.shields.io/github/v/release/matterpale/depdog?color=d68a1e)](https://github.com/matterpale/depdog/releases)
[![CI](https://github.com/matterpale/depdog/actions/workflows/ci.yml/badge.svg)](https://github.com/matterpale/depdog/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-d68a1e)](LICENSE)

[**Install**](#install)&nbsp;·&nbsp;[**Quick start**](#quick-start)&nbsp;·&nbsp;[**Configuration**](#configuration)
&nbsp;·&nbsp;[**Commands**](#commands)&nbsp;·&nbsp;[**CI**](#ci)

</div>

<p align="center">
  <img src="assets/demo.gif" alt="depdog demo: check, explain, and the TUI on a module with violations" width="820">
</p>

Architecture rules
usually live in someone's head or a wiki, and they rot.
**depdog** makes rules executable.

> _No more import spaghetti._

You declare who may import whom in a `depdog.yaml`, and
depdog checks it against every import edge in your codebase, exiting
non-zero for CI. One neutral rule format,
one engine, and a thin hot-swappable
[language adapter](#multi-language-support).

```
depdog check — github.com/matterpale/depdog

✗ core: allow [std]  (2 violations)
    github.com/matterpale/depdog/internal/core
      → github.com/matterpale/depdog/internal/report   internal/core/evaluate.go:9
      → github.com/charmbracelet/lipgloss              internal/core/core.go:12

2 violations · 10 packages · 107 edges checked in 112ms
```

<sub>*That's depdog checking its own repo: its architecture is declared in
[`depdog.yaml`](depdog.yaml) and enforced in CI — a failing architecture is a
failing build.*</sub>

## Use cases

- **CI — the main event.** `depdog check` exits non-zero on any violation and
  speaks `github` and `sarif`, so a tangled import fails the build like a broken
  test would. See [CI](#ci).
- **Coding agents.** A stable `--format json` schema, contract
  [exit codes](#commands), language auto-detect, and a drop-in authoring skill
  let an agent map a codebase and keep it honest. See [For AI agents](#for-ai-agents).
- **Local exploration.** The [TUI](#commands) (bare `depdog`) and `depdog explain`
  are for reading an existing graph and debugging a config by hand.
- **In your editor (LSP).** `depdog lsp` surfaces violations as inline
  diagnostics — nice if you live in your editor, though architecture drifts
  slowly enough that commit/CI time usually catches it just fine.

## Install

**Homebrew** (macOS):

```bash
brew install --cask matterpale/tap/depdog
```

**Go:**

```bash
go install github.com/matterpale/depdog/cmd/depdog@latest
```

Prebuilt binaries for Linux, macOS, and Windows are on the
[releases page](https://github.com/matterpale/depdog/releases); building from
source (`go build -o depdog ./cmd/depdog`) needs Go 1.26+.

## Quick start

```bash
depdog init      # scan the module and write a starter depdog.yaml
depdog check     # check against the rules; exit 1 on violations
```

`init` inspects your layout, matches it against an architecture preset, and
proposes a component mapping you refine interactively — drop, rename, or
re-pattern components — or accept as-is with `--yes`. It refuses to touch an
existing `depdog.yaml`; as the code grows, `depdog init --merge` rescans the
module and appends a component (and, under `default: deny`, a starter rule)
for every directory no existing pattern covers — editing the file in place
without disturbing your comments, ordering or formatting. When everything is
covered it changes nothing and says so.

## Configuration

`depdog.yaml` lives at the repo root, next to `go.mod`:

```yaml
version: 2

# Each component lists its path glob(s) and, inline, who it may import.
components:
  main: { path: "cmd/**" }                                # no rule → open (the default)
  domain: { path: "internal/domain/**", allow: [ std ] }      # whitelist: std only
  handler: { path: "internal/handler/**", deny: [ service, repository ] } # forbids its peers
  service: { path: "internal/service/**", deny: [ handler, repository ] }
  repository: { path: "internal/repository/**", deny: [ handler, service ] }

default: allow   # fallback for a rule-less component (like main); the default if omitted

options:
  test_files: hybrid              # default; also: same-rules, relaxed
  skip: [ "internal/legacy/**" ]    # package dirs excluded from analysis
```

Here `domain` is a **whitelist** (an `allow` list — only what's listed passes) and the
three peers are **blacklists** (a `deny` list — everything except what's listed); the
stance is read per component from which word you use. `main` has no rule at all, so it
falls back to the top-level `default` — which is `allow`, so it may import anything
(an explicit `allow: ["*"]` would be equivalent, just noisier). `path` takes a single
glob or a list (`path: ["internal/api/**", "internal/rpc/**"]`).

An editor JSON Schema ships at
[`schema/depdog.schema.json`](schema/depdog.schema.json) for autocomplete and
validation (a test keeps it in lockstep with the parser).

Two more knobs, both optional: **groups** name a reusable set of components you
can reference in any allow/deny list, and **boundaries** add an orthogonal
mutual-exclusion axis — named member sets that may not import across each other,
for isolating services in a monorepo without O(n²) deny lists.

**Full reference — [docs/configuration.md](docs/configuration.md):** component
matching and precedence, the complete `allow`/`deny` vocabulary, groups, the
non-blocking signals (unmapped packages, dead patterns, component cycles), and
test-file handling. Boundaries have their own page —
[docs/boundaries.md](docs/boundaries.md).

## Commands

| Command                                          | What it does                                                                                                                                                                                                 |
|--------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `depdog init`                                    | Scan the module and write a starter `depdog.yaml`; `--merge` extends an existing one in place                                                                                                                |
| `depdog check [packages]`                        | Evaluate every import edge against the rules                                                                                                                                                                 |
| `depdog baseline`                                | Record current violations to `depdog.baseline.yaml` for the [ratchet](#adopting-rules-on-a-codebase-that-doesnt-pass-yet)                                                                                    |
| `depdog graph`                                   | Emit the dependency graph as DOT or Mermaid                                                                                                                                                                  |
| `depdog explain <component-or-package> [import]` | Explain why something is red (the rule or boundary that fired, with file:line), how a component is constrained, its boundary membership, or whether *A* may import *B* and which rule or boundary decides it |
| `depdog config`                                  | Print the compiled rule set — components, patterns, inferred stances, boundaries, options — for debugging a config                                                                                           |
| `depdog lsp`                                     | LSP server over stdio: violations become inline editor diagnostics at their import lines ([editor setup](docs/editors.md) · [design](docs/lsp.md))                                                           |
| `depdog tui` (or bare `depdog`)                  | Interactive terminal UI: component dashboard, browsable violations, per-package imports and importers, and a Config tab showing the compiled rules                                                           |

<details>
<summary><b>All flags</b></summary>

| Command | Flags                                                                                                                                                                                            |
|---------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `init`  | `--preset ddd\|hexagonal\|layered\|flat` · `--default deny\|allow` · `--yes` (non-interactive) · `--force` (overwrite) · `--merge` (extend an existing file, preserving comments and formatting) |
| `check` | `--format text\|json\|github\|sarif` · `--fail-on any\|new` · `--color auto\|always\|never`                                                                                                      |
| `graph` | `--format dot\|mermaid` · `--level component\|package` · `--violations-only` · `--focus <component>`                                                                                             |

</details>

<details>
<summary><b>Editor / LSP setup</b></summary>

Wire `depdog lsp` into Neovim, Helix, VS Code (via the bundled
[`editors/vscode`](editors/vscode) extension scaffold), Zed, GoLand/JetBrains
(via the LSP4IJ plugin), or Emacs for inline architecture diagnostics —
per-editor snippets in [docs/editors.md](docs/editors.md).

</details>

<details>
<summary><b>TUI keys</b></summary>

In the TUI, <kbd>1</kbd>–<kbd>4</kbd> (or <kbd>tab</kbd>) switch between the
Dashboard, Violations, Packages and Config screens. The Violations and Packages
lists scroll and filter with <kbd>/</kbd>; <kbd>e</kbd> opens the selection in
`$EDITOR` at its file:line, <kbd>r</kbd> re-runs the check in place, and
<kbd>?</kbd> shows all keys. The Config tab (<kbd>4</kbd>) shows the active
config path and the compiled rule set (the same data as `depdog config`);
<kbd>e</kbd> there opens `depdog.yaml` in `$EDITOR`, and the editor exiting
auto-re-runs the check so the edited rules take effect on every screen.

</details>

Exit codes are a contract:

| Code | Meaning                      |
|:----:|------------------------------|
| `0`  | clean                        |
| `1`  | violations                   |
| `2`  | configuration or usage error |

## CI

`depdog check` is CI-ready as-is. For inline pull-request annotations use the
GitHub format; for GitHub code scanning, emit SARIF:

```yaml
- run: go run github.com/matterpale/depdog/cmd/depdog check --format github

# or, for the code-scanning tab:
- run: go run github.com/matterpale/depdog/cmd/depdog check --format sarif > depdog.sarif
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: depdog.sarif }
```

### Adopting rules on a codebase that doesn't pass yet

Record today's violations as a baseline, then fail only on new ones — and shrink
the baseline over time:

```bash
depdog baseline                 # writes depdog.baseline.yaml
depdog check --fail-on new      # exits 1 only on violations not in the baseline
```

## Multi-language support

depdog checks **nine** languages with the *same* `depdog.yaml`, the *same*
commands (`check`, `graph`, `explain`, `config`, TUI), and the *same* engine.
Only a thin language adapter differs; the rule format is neutral — component
`path` globs match project-relative directories, and `std` / `external` are
abstract buckets each adapter fills (Go stdlib vs Node builtins vs the Python
stdlib; a Go module vs an `node_modules` package vs a gem). Every adapter is a
pure-Go static import scanner — **no language toolchain is required** (no
Node/`tsc`, no `python`, no `cargo`), depdog stays a single binary.

|        | Language | Detected by                               | Scans                                               |
|--------|----------|-------------------------------------------|-----------------------------------------------------|
| `go`   | Go       | `go.mod`                                  | package imports                                     |
| `rs`   | Rust     | `Cargo.toml`                              | `use` / `mod` / `extern crate`                      |
| `py`   | Python   | `pyproject.toml`, `setup.py`, `setup.cfg` | `import` / `from … import` (incl. relative)         |
| `kt`   | Kotlin   | `build.gradle.kts`, `settings.gradle.kts` | `package` + `import`                                |
| `java` | Java     | `pom.xml`, `build.gradle`                 | `package` + `import`                                |
| `scala`| Scala    | `build.sbt`, `build.sc`                   | `package` + `import` (incl. `{A,B}`, `._`, `.*`, `given`) |
| `elm`  | Elm      | `elm.json`                                | `module` + `import` (module-name resolution)        |
| `rb`   | Ruby     | `Gemfile`, `.ruby-version`, `Rakefile`    | `require` / `require_relative` / `autoload`         |
| `ts`   | TS / JS  | `tsconfig.json`, `package.json`           | `import`/`export from`/`require`/dynamic `import()` |

`internal/core` (the engine) never changed as languages were added — the whole
point of the [adapter registry](internal/cli/languages.go) is that a new
language is one `internal/lang/<x>` package plus one registry entry.

depdog picks the adapter from the project's marker files automatically, and the
persistent `--lang` flag forces a specific one — details, including nested
layouts and two-language ambiguity, in [docs/languages.md](docs/languages.md).

## For AI agents

depdog is built to be driven by tools and agents, not just humans:
`check --format json` emits a stable schema and the [exit codes](#commands) are
a contract; auto-detect (or `--lang`) means an agent needn't know the language
up front; and [`skills/depdog-config/SKILL.md`](skills/depdog-config/SKILL.md)
is a self-contained, tool-agnostic playbook any coding agent can follow to map a
codebase to components and author a `depdog.yaml` — drop the folder wherever your
agent discovers skills, or point it at the file directly. The editor
[JSON Schema](schema/depdog.schema.json) hands the same autocomplete and
validation to any schema-aware agent.

## Limitations

- **Static analysis.** Every adapter scans source for import statements; it does
  not run or type-check your code. Fully dynamic imports (a computed
  `require(someVar)`, a reflective Java classload) are invisible by design —
  architecture rules are about the imports you *write*.
- **Go adapter — one build configuration.** The Go adapter loads packages for
  the host's `GOOS`/`GOARCH` and default build tags; imports guarded by other
  build constraints (e.g. `//go:build windows` on a non-Windows machine) aren't
  seen.
- **Go workspaces — per-module checks.** In a Go workspace (`go.work`), `depdog
  check` fans out over every member module that has its own `depdog.yaml`,
  reporting them together (members without one are advisory-skipped); narrow the
  run with `--module <path-or-dir>` (repeatable). Each member is still checked as
  a single module, so an import between two workspace members classifies as
  `external` — depdog does not yet govern edges *between* members. `GOWORK=off`
  forces the classic single-module check.

## Status

v0.6.0 — the current release. depdog now checks **nine** languages (Go,
TypeScript/JS, Python, Rust, Java, Ruby, Kotlin, Scala, Elm) through a pluggable adapter
registry, on top of the config v2 format (per-component `allow`/`deny`,
`default` stance) and `boundaries` (orthogonal mutual-exclusion groups). In a Go
workspace, `depdog check` now fans out over the member modules, each checked
against its own `depdog.yaml` (see [Limitations](#limitations)). The M0–M5
roadmap is complete, and editor/LSP integration has landed: `depdog lsp` plus a
per-editor [setup guide](docs/editors.md).

## License

[MIT](LICENSE)

---

<p align="center"><sub>🐕 <em>woof.</em></sub></p>
