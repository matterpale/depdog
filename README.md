<div align="center">

<img src="assets/logo.svg" alt="depdog" width="330">

**A Codebase Dependency Watchdog** — your architecture rules, enforced on every build.

[![Latest release](https://img.shields.io/github/v/release/matterpale/depdog?color=d68a1e)](https://github.com/matterpale/depdog/releases)
[![CI](https://github.com/matterpale/depdog/actions/workflows/ci.yml/badge.svg)](https://github.com/matterpale/depdog/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-d68a1e)](LICENSE)

[**Install**](#install)&nbsp;·&nbsp;[**Quick start**](#quick-start)&nbsp;·&nbsp;[**Configuration**](#configuration)
&nbsp;·&nbsp;[**CI**](#ci)&nbsp;·&nbsp;[**Commands**](#commands)

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
non-zero for CI. **Nine languages, one tool, one command — even in one repo:**
one neutral rule format and one engine, **polyglot** across
[nine languages](#multi-language-support) through thin, hot-swappable adapters,
and `depdog check --all` governs a mixed monorepo — a `web/` TS app, a
`services/api` Go module, an `ml/` Python package — in a single pass with one
exit code ([monorepos](#monorepos)). Every adapter is a pure static import
scanner — **no build, no toolchain** — so depdog is fast and runs even on code
that doesn't compile yet.

<sub>*Unlike Go-package-loader–based linters such as
[go-arch-lint](https://github.com/fe3dback/go-arch-lint) (which load your
packages through the Go toolchain), depdog scans source directly — so it works
mid-refactor, on a broken build, and across languages with no Go toolchain at
all.*</sub>

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

## Quickstart

```bash
depdog init      # interactively kick off a starter depdog.yaml
depdog check     # check against the rules; exit 1 on violations
```

`init` inspects your layout, matches it against an architecture preset, and
proposes a component mapping you refine interactively — drop, rename, or
re-path components. Or accept all as-is with `--yes`.

Alternatively, ask a coding agent to get you started with the dedicated [skill](skills/depdog/SKILL.md).

## Configuration

`depdog.yaml` lives at the repo root, next to `go.mod`:

```yaml
version: 2

# Each component lists its path glob(s) and, inline, who it may import.
components:
  main: { path: "cmd/**" }                                    # no rule → open (the default)
  domain: { path: "internal/domain/**", allow: [ std ] }      # whitelist: std only
  handler: { path: "internal/handler/**", deny: [ service, repository ] } # forbids its peers
  service: { path: "internal/service/**", deny: [ handler, repository ] }
  repository: { path: "internal/repository/**", deny: [ handler, service ] }

default: allow   # fallback for a rule-less component (like main); the default if omitted

options:
  test_files: hybrid                # default; also: same-rules, relaxed
  skip: [ "internal/legacy/**" ]    # package dirs excluded from analysis
```

Here `domain` is a **whitelist** (an `allow` list — only what's listed passes); the
three peers instead use `deny` lists to forbid their siblings, and the stance is read
per component from which word you use. `main` has no rule at all, so it
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

## Commands

| Command                                          | What it does                                                                                                                                       |
|--------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| `depdog init`                                    | Scan the module and write a starter `depdog.yaml`; `--merge` extends an existing one in place                                                      |
| `depdog check [packages]`                        | Evaluate every import edge against the rules                                                                                                       |
| `depdog graph`                                   | Emit the dependency graph as DOT or Mermaid                                                                                                        |
| `depdog explain <component-or-package> [import]` | Explain why something is red (rule/boundary that fired, with file:line), constraints, boundary membership etc.                                     |
| `depdog config`                                  | Print the compiled rule set — components, patterns, inferred stances, boundaries, options — for debugging a config                                 |
| `depdog lsp`                                     | LSP server over stdio: violations become inline editor diagnostics at their import lines ([editor setup](docs/editors.md) · [design](docs/lsp.md)) |
| `depdog tui` (or bare `depdog`)                  | Interactive terminal UI: component dashboard, browsable violations, per-package imports and importers, and a Config tab showing the compiled rules |
| `depdog baseline`                                | Record current violations to `depdog.baseline.yaml` for the [ratchet](#adopting-rules-on-a-codebase-that-doesnt-pass-yet)                          |

<details>
<summary><b>All flags</b></summary>

| Command | Flags                                                                                                                                                                                            |
|---------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `init`  | `--preset ddd\|hexagonal\|layered\|flat` · `--default deny\|allow` · `--yes` (non-interactive) · `--force` (overwrite) · `--merge` (extend an existing file, preserving comments and formatting) |
| `check` | `--format text\|json\|github\|sarif` · `--fail-on any\|new` · `--color auto\|always\|never`                                                                                                      |
| `graph` | `--format dot\|mermaid` · `--level component\|package` · `--violations-only` · `--focus <component>`                                                                                             |

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
pure-Go static import scanner — **no language toolchain is required**, depdog stays a single binary.

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
`check --format json` emits a stable schema and the [exit codes](#commands) are
a contract.

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


## Limitations

- **Static analysis.** Every adapter scans source for import statements; it does
  not run or type-check your code. Fully dynamic imports (a computed
  `require(someVar)`, a reflective Java classload) are invisible by design —
  architecture rules are about the imports you *write*.
- **Go adapter — one build configuration.** The Go adapter loads packages for
  the host's `GOOS`/`GOARCH` and default build tags; imports guarded by other
  build constraints (e.g. `//go:build windows` on a non-Windows machine) aren't
  seen.
- <a id="monorepos"></a>**Monorepos — per-unit, no cross-unit governance.**
  depdog checks a monorepo by fanning out over its **units**, each checked
  independently against its own `depdog.yaml`, via two discovery kinds:
  **`go.work` fan-out** (automatic — inside a Go workspace `depdog check` fans
  out over every member module with a `depdog.yaml`; members without one are
  advisory-skipped, `--module <path-or-dir>` narrows, `GOWORK=off` opts out),
  and **`--all` polyglot discovery** (any language — from the repo root `depdog
  check --all` walks down, discovers *every* `depdog.yaml`, and checks each unit
  against its own auto-detected adapter; `--unit <dir>` narrows). Both aggregate
  into one report and a single exit code (max severity across units) in every
  `--format`. Each unit is still checked in isolation, so an import from one
  unit into another classifies as `external`: depdog does **not** yet govern
  edges *between* units — cross-unit governance is designed future work, not
  part of this release. Full guide: [docs/monorepo.md](docs/monorepo.md).

## Compatibility

depdog 1.0 follows semver. The **machine-readable contract is stable** — broken
only by a major bump, additive within a major line: the config v2 `depdog.yaml`
schema, both `--format json` shapes (the single-unit report **and** the
`--all`/`go.work` aggregate envelope, `root`/`units[]`/`skipped[]`/`stats`), the
exit codes (`0` clean · `1` violations · `2` config/usage error), and documented
CLI flag semantics. The **human-readable presentation is not** — text and TUI
rendering, log/stderr wording, Go internals (all `internal/`, no exported API),
and SARIF/GitHub detail beyond path + rule identity may change any release. The
JSON goldens, the schema-reflection test, and the single-unit byte-identity
guard hold this line in CI. Full policy: [docs/compatibility.md](docs/compatibility.md).

## License

[MIT](LICENSE)

---

<p align="center"><sub>🐕 <em>woof.</em></sub></p>
