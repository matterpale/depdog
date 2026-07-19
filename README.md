<div align="center">

<img src="assets/logo.svg" alt="depdog" width="330">

**A Codebase Dependency Watchdog** — your architecture rules, enforced on every build.

[![Latest release](https://img.shields.io/github/v/release/matterpale/depdog?color=d68a1e)](https://github.com/matterpale/depdog/releases)
[![CI](https://github.com/matterpale/depdog/actions/workflows/ci.yml/badge.svg)](https://github.com/matterpale/depdog/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-d68a1e)](LICENSE)

[**Install**](#install)&nbsp;·&nbsp;[**Quick start**](#quick-start)&nbsp;·&nbsp;[**Configuration**](#configuration)
&nbsp;·&nbsp;[**CI**](#ci)&nbsp;·&nbsp;[**Commands**](#commands)&nbsp;·&nbsp;[**Docs**](docs/README.md)

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
non-zero for CI. One tool, one engine, **polyglot** across
[languages](#multi-language-support) through thin, hot-swappable adapters. Mixed monorepos
supported.

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

[**CI**](#ci)

`depdog check` exits non-zero on any violation and
  speaks `github` and `sarif`.

[**Coding agents**](#for-ai-agents)

A stable JSON schema, contract
  [exit codes](#commands), and a skill
  help your agent get you started.

[**Local exploration**](#commands)

The TUI and `depdog explain`
  help with reading an existing graph and debugging by hand.

[**LSP for your IDE**](#lsp-setup)

`depdog lsp` surfaces violations as inline
  diagnostics in the editor of your choice.

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
depdog init      # interactively kick off a starter depdog.yaml
depdog check     # check against the rules; exit 1 on violations
```

`init` inspects your layout, matches it against an architecture preset, and
proposes a component mapping you refine interactively — drop, rename, or
re-path components. Or accept all as-is with `--yes`.

> [!TIP]
Alternatively, ask a coding agent to get you started with the dedicated [skill](skills/depdog/SKILL.md).

## Configuration

`depdog.yaml` lives at the repo root, next to `go.mod`:

```yaml
version: 2

# Each component lists its path glob(s) and, inline, who it may import.
components:
  main: { path: "cmd/**" }                                # no rule → open (the default)
  domain: { path: "internal/domain/**", allow: [ std ] }  # whitelist: std only
  handler: { path: "internal/handler/**" }
  service: { path: "internal/service/**" }
  repository: { path: "internal/repository/**" }

# A boundary isolates its members from each other: one line, no O(n²) deny lists.
boundaries:
  layers: [ handler, service, repository ]

default: allow   # fallback for a rule-less component (like main); the default if omitted

options:
  test_files: hybrid                # default; also: same-rules, relaxed
  skip: [ "internal/legacy/**" ]    # package dirs excluded from analysis
```

Here `domain` is an `allow` list — only what's listed passes. The `layers`
**boundary** keeps the three peers out of each other — the same effect
as giving each a `deny` list naming its two siblings, in one line.
`deny` lists still exist for one-off exclusions.

An editor JSON Schema ships at
[`schema/depdog.schema.json`](schema/depdog.schema.json) for autocomplete and
validation (a test keeps it in lockstep with the parser).

**Full reference — [docs/configuration.md](docs/configuration.md):** component
matching and precedence, the complete `allow`/`deny` vocabulary, groups, the
non-blocking signals (unmapped packages, dead patterns, component cycles), and
test-file handling. Boundaries have their own page —
[docs/boundaries.md](docs/boundaries.md).

## CI

`depdog check` is CI-ready as-is. For inline pull-request annotations use the
`github` format; for code scanners, use `sarif`:

```yaml
- run: go run github.com/matterpale/depdog/cmd/depdog check --format github

# or, for the code-scanning tab:
- run: go run github.com/matterpale/depdog/cmd/depdog check --format sarif > depdog.sarif
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: depdog.sarif }
```

Or use the **composite action** — it downloads the released binary (no Go
toolchain needed on the runner, so it works for any-language repos) and runs
depdog:

```yaml
- uses: matterpale/depdog@v0.6.0
  with:
    version: latest                    # a tag like v0.6.0, or "latest"
    args: check --all --format github
```

For a **polyglot monorepo**, one step governs every language at once — from the
repo root, `--all` discovers every `depdog.yaml` under the tree, checks each
unit against its own auto-detected adapter, and aggregates into one report with
a single exit code:

```yaml
- run: go run github.com/matterpale/depdog/cmd/depdog check --all --format github
```

See [monorepos](#monorepos) for what a unit is and how discovery works.

### Ratchet-friendly

For a codebase that doesn't pass yet: record today's violations as a baseline,
then fail only on new ones — and shrink the baseline over time:

```bash
depdog baseline                 # writes depdog.baseline.yaml
depdog check --fail-on new      # exits 1 only on violations not in the baseline
```

### Pre-commit hook

Catch a broken architecture before it reaches CI. Install a git hook directly:

```bash
depdog install-hook   # writes .git/hooks/pre-commit → runs `depdog check`
```

It is idempotent and won't overwrite a pre-commit hook it didn't write (pass
`--force` to replace one). Or, with the [pre-commit framework](https://pre-commit.com),
add to `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/matterpale/depdog
    rev: v0.6.0
    hooks:
      - id: depdog
```

### Diff your architecture per PR

`depdog diff --since <ref>` reports how a change *moved* the architecture: the
cross-component import edges a branch **adds** and **removes** relative to a git
ref, each flagged when it crosses a boundary — e.g. "3 new cross-component edges
(1 crosses the `adapters` boundary), 1 removed". It is **informational** (always
exits `0`, unlike `check`), so it never blocks a merge — it surfaces new
structure a reviewer should notice.

Post it as a PR comment with the GitHub-flavoured markdown format:

```yaml
- name: Architecture diff
  run: |
    go run github.com/matterpale/depdog/cmd/depdog diff \
      --since origin/${{ github.base_ref }} --format github > diff.md
    gh pr comment ${{ github.event.number }} --body-file diff.md
  env: { GH_TOKEN: ${{ github.token }} }
```

Use `--format json` for tooling — a stable, sorted `{ since, added[], removed[],
stats }` delta (snake_case; empty collections are `[]`, never `null`):

```bash
depdog diff --since origin/main --format json | jq '.stats'
```

The "before" graph is the ref materialized in a throwaway git worktree; both
graphs are mapped to components under the *current* `depdog.yaml`, so the diff
reflects structural movement, not a config change.

## Commands

| Command                                          | What it does                                                                                                                                       |
|--------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| `depdog init`                                    | Scan the module and write a starter `depdog.yaml`; `--merge` extends an existing one in place                                                      |
| `depdog check [packages]`                        | Evaluate every import edge against the rules                                                                                                       |
| `depdog graph`                                   | Emit the dependency graph as DOT or Mermaid                                                                                                        |
| `depdog diff --since <ref>`                      | Show how a change moved the architecture vs a git ref: cross-component edges added/removed, boundary crossings flagged (informational, exits `0`)   |
| `depdog explain <component-or-package> [import]` | Explain why something is red (rule/boundary that fired, with file:line), constraints, boundary membership etc.                                     |
| `depdog config`                                  | Print the compiled rule set — components, patterns, inferred stances, boundaries, options — for debugging a config                                 |
| `depdog lsp`                                     | LSP server over stdio: violations become inline editor diagnostics at their import lines ([editor setup](docs/editors.md) · [design](docs/lsp.md)) |
| `depdog mcp`                                     | Read-only MCP server over stdio: `check`, `explain` and `can_import` tools plus config resources, for agents ([docs/mcp.md](docs/mcp.md))          |
| `depdog tui`                                     | Interactive terminal UI: component dashboard, browsable violations, per-package imports and importers, a Config tab showing the compiled rules — and an experimental visual rule editor ([keys](docs/README.md#tui-keys)) |
| `depdog baseline`                                | Record current violations to `depdog.baseline.yaml` for the [ratchet](#ratchet-friendly)                          |
| `depdog install-hook`                            | Install a git `pre-commit` hook that runs `depdog check` (idempotent; `--force` to replace a foreign hook)         |

Run bare, `depdog` evaluates the check — the same as `depdog check`, taking the
same flags — so a plain `depdog` yields the real 0/1/2 exit code in a pipe or on
a terminal. The interactive view is `depdog tui`.

Exit codes are a [contract](docs/compatibility.md):

| Code | Meaning    |
|:----:|------------|
| `0`  | clean      |
| `1`  | violations |
| `2`  | error      |

## Multi-language support

depdog checks **nine** languages with the *same* `depdog.yaml`, the *same*
commands, and the *same* engine.

Only a thin language adapter differs; the rule format is neutral — component
`path` globs match project-relative directories, and `std` / `external` are
abstract buckets each adapter fills. Every adapter is a
pure-Go static import scanner — **no language toolchain is required**, depdog
stays a single binary. (The one exception: the Go adapter resolves its package
graph through `go list` metadata — see [limitations](#limitations).)

|         | Language | Detected by                               | Scans                                                     |
|---------|----------|-------------------------------------------|-----------------------------------------------------------|
| `go`    | Go       | `go.mod`                                  | package imports                                           |
| `rs`    | Rust     | `Cargo.toml`                              | `use` / `mod` / `extern crate`                            |
| `py`    | Python   | `pyproject.toml`, `setup.py`, `setup.cfg` | `import` / `from … import` (incl. relative)               |
| `kt`    | Kotlin   | `build.gradle.kts`, `settings.gradle.kts` | `package` + `import`                                      |
| `java`  | Java     | `pom.xml`, `build.gradle`                 | `package` + `import`                                      |
| `scala` | Scala    | `build.sbt`, `build.sc`                   | `package` + `import` (incl. `{A,B}`, `._`, `.*`, `given`) |
| `elm`   | Elm      | `elm.json`                                | `module` + `import` (module-name resolution)              |
| `rb`    | Ruby     | `Gemfile`, `.ruby-version`, `Rakefile`    | `require` / `require_relative` / `autoload`               |
| `ts`    | TS / JS  | `tsconfig.json`, `package.json`           | `import`/`export from`/`require`/dynamic `import()`       |

`internal/core` (the engine) never changed as languages were added — the whole
point of the [adapter registry](internal/cli/languages.go) is that a new
language is one `internal/lang/<x>` package plus one registry entry.

depdog picks the adapter from the project's marker files automatically, and the
persistent `--lang` flag forces a specific one — details, including nested
layouts and two-language ambiguity, in [docs/languages.md](docs/languages.md).

## For AI agents

depdog is built to be driven by tools and agents, not just humans:
`check --format json` emits a [stable schema](docs/compatibility.md) and the
[exit codes](#commands) are a contract.

[`skills/depdog/SKILL.md`](skills/depdog/SKILL.md)
is a self-contained, tool-agnostic playbook any coding agent can follow to work
with depdog end to end — mapping a codebase to components, authoring the
`depdog.yaml`, and validating with `check`. The editor
[JSON Schema](schema/depdog.schema.json) hands the same autocomplete and
validation to any schema-aware agent.

depdog also speaks **MCP** — `depdog mcp` exposes read-only
`check`/`explain`/`can_import` tools (plus `config`/`components` resources) over
stdio so an MCP-capable agent can consult the architecture in the loop. See
[docs/mcp.md](docs/mcp.md).

## LSP Setup

Wire `depdog lsp` into Neovim, Helix, VS Code (via the bundled
[`editors/vscode`](editors/vscode) extension scaffold), Zed, GoLand/JetBrains
(via the LSP4IJ plugin), or Emacs for inline architecture diagnostics —
per-editor snippets in [docs/editors.md](docs/editors.md).


## How depdog compares

Every major ecosystem has grown its own architecture linter —
[go-arch-lint](https://github.com/fe3dback/go-arch-lint) for Go,
[dependency-cruiser](https://github.com/sverweij/dependency-cruiser) for JS/TS,
[deptrac](https://github.com/deptrac/deptrac) for PHP (Python has
import-linter and Tach, Java has ArchUnit). Each is excellent inside its
ecosystem, and each is welded to it — its language, and usually its toolchain.
depdog's bet is different: one rule model, one engine, thin adapters.

|                                            | depdog                     | go-arch-lint     | dependency-cruiser                 | deptrac                                  |
|--------------------------------------------|----------------------------|------------------|------------------------------------|------------------------------------------|
| Languages                                  | nine, one rule format      | Go               | JS/TS (+ Vue, Svelte)              | PHP                                      |
| Needs                                      | one static binary*         | the Go toolchain | Node + the project's own compilers | PHP ≥ 8.2                                |
| Baseline ratchet                           | ✓                          | —                | ✓                                  | ✓                                        |
| CI formats                                 | GitHub annotations · SARIF | JSON             | Markdown · TeamCity · Azure DevOps | GitHub annotations · JUnit · CodeClimate |
| Architecture diff per PR (edges ±)         | ✓ `diff --since`           | —                | —                                  | —                                        |
| Inline editor diagnostics (LSP)            | ✓                          | —                | —                                  | —                                        |
| Agent interface                            | [MCP](docs/mcp.md) · [skill](skills/depdog/SKILL.md) · [semver-stable JSON](docs/compatibility.md) | JSON | JSON · JS API | JSON |
| Rules *across* languages in one monorepo   | ✓ `depdog.work.yaml`       | —                | —                                  | —                                        |

<sub>*The Go adapter is the one exception: it resolves its package graph
through `go list` metadata — see [limitations](#limitations).</sub>

**vs go-arch-lint**, the nearest neighbour on a Go codebase: go-arch-lint
loads your packages through the Go toolchain — even its basic check shells out
to `go list`, and its default-on *deepscan* fully type-checks the module, so
your dependencies must resolve. The payoff is real — deepscan traces
dependency injection through interfaces, catching inversions no import scan
can see. depdog deliberately stays at the import layer: scan source text,
never build — so it runs mid-refactor, on code that doesn't compile yet, and
the identical engine covers eight more languages. On top of that layer depdog
adds what go-arch-lint doesn't have: the baseline ratchet, SARIF, an LSP, an
MCP server, and per-PR architecture diffs.

Inside their home ecosystems, dependency-cruiser (regex rules, a dozen-plus
output formats) and deptrac (semantic layer collectors: by attribute,
interface, composer package) go deeper than depdog does. depdog's case is the
column no per-language tool can fill: the same `depdog.yaml` in every corner
of the repo, the agent surface, and
[cross-language rules](docs/cross-language.md) between the units of a mixed
monorepo.

## Limitations

- **Static analysis.** Every adapter scans source for import statements; it does
  not run or type-check your code. Fully dynamic imports (a computed
  `require(someVar)`, a reflective Java classload) are invisible by design —
  architecture rules are about the imports you *write*.
- **Go adapter — one build configuration.** The Go adapter loads packages for
  the host's `GOOS`/`GOARCH` and default build tags; imports guarded by other
  build constraints (e.g. `//go:build windows` on a non-Windows machine) aren't
  seen.
- <a id="monorepos"></a>**Monorepos — per-unit fan-out; cross-unit rules are
  opt-in.** depdog checks a monorepo by fanning out over its **units**, each
  checked independently against its own `depdog.yaml`, via two discovery kinds:
  **`go.work` fan-out** (automatic — inside a Go workspace `depdog check` fans
  out over every member module with a `depdog.yaml`; members without one are
  advisory-skipped, `--module <path-or-dir>` narrows, `GOWORK=off` opts out),
  and **`--all` polyglot discovery** (any language — from the repo root `depdog
  check --all` walks down, discovers *every* `depdog.yaml`, and checks each unit
  against its own auto-detected adapter; `--unit <dir>` narrows). Both aggregate
  into one report and a single exit code (max severity across units) in every
  `--format`. Within the fan-out each unit is checked in isolation, so an import
  from one unit into another classifies as `external`; governing the edges
  *between* units is the opt-in
  [`depdog.work.yaml` layer](docs/cross-language.md) on top. Note the
  cross-unit pass detects source-level references (resolved paths and import
  identities) — a plain HTTP call between services leaves no import to detect.
  Full guides: [docs/monorepo.md](docs/monorepo.md),
  [docs/cross-language.md](docs/cross-language.md).

## License

[MIT](LICENSE)

---

<p align="center"><sub>🐕 <em>woof.</em></sub></p>
