---
name: depdog
description: >-
  Work with depdog, the architecture/import linter. The main task is authoring
  and maintaining the depdog.yaml that declares which components may import whom;
  this skill also covers running and interpreting `depdog check`, wiring it into
  CI, baselining a legacy repo, and exploring a codebase with the TUI, `explain`
  and `graph`. Use when the user wants to adopt depdog, set up or tighten
  import/architecture rules, define components, aliases or boundaries, debug
  `depdog check` errors or violations, or integrate depdog into CI or an editor
  (LSP). Covers Go, TypeScript/JS, Python, Rust, Java, Kotlin, Ruby, Scala, and
  Elm.
---

# Working with depdog

[depdog](https://github.com/matterpale/depdog) checks a codebase's import edges
against architecture rules declared in a `depdog.yaml` at the repo root — one
neutral rule format and one engine across nine languages, behind a thin
per-language adapter. The **main task** this skill supports is authoring and
maintaining that `depdog.yaml`; the rest of the tool (check, CI, the TUI,
`explain`/`graph`, baseline, LSP) is covered below too. Produce a real, working
config — not a sketch.

## Run depdog

Prefer an installed binary; otherwise run it from source (no clone needed):

```bash
depdog --version 2>/dev/null || alias depdog='go run github.com/matterpale/depdog/cmd/depdog@latest'
```

depdog is a single Go binary; it needs **no** language toolchain for the target
project (it scans source statically). It auto-detects the language from marker
files, or `--lang <id>` forces one:

| `--lang` | Language | Marker files |
| --- | --- | --- |
| `go` | Go | `go.mod` |
| `ts` | TypeScript/JS (incl. TSX/JSX) | `tsconfig.json`, `package.json` |
| `py` | Python | `pyproject.toml`, `setup.py`, `setup.cfg` |
| `rs` | Rust | `Cargo.toml` |
| `java` | Java | `pom.xml`, `build.gradle`, `build.gradle.kts` |
| `kt` | Kotlin | `build.gradle.kts`, `settings.gradle.kts` |
| `rb` | Ruby | `Gemfile`, `.ruby-version`, `Rakefile` |
| `scala` | Scala | `build.sbt`, `build.sc` |
| `elm` | Elm | `elm.json` |

If a directory's markers are ambiguous (e.g. a JVM project rooted only on
`build.gradle`, claimed by both Java and Kotlin), pin the adapter per project
with an optional top-level `lang:` key in its `depdog.yaml` (`lang: kt`) instead
of passing `--lang` every time.

A bare `depdog` (no subcommand) runs `depdog check`. The subcommands are `init`,
`check`, `config`, `explain`, `graph`, `baseline`, `tui`, and `lsp`.

**Polyglot monorepo:** for a repo with several projects in one tree — a `web/`
TS app, a `services/api` Go module, an `ml/` Python package, each with its own
`depdog.yaml` — run `depdog check --all` from the repo root. It discovers every
`depdog.yaml` under the cwd, checks each unit against its own auto-detected
adapter, and aggregates into one report with a single exit code. Narrow it with
`--unit <dir>` (repeatable). Full guide: [docs/monorepo.md](../../docs/monorepo.md).

**Cross-unit rules:** to govern the edges *between* those units ("web may
depend only on shared", "no service imports another", "nothing reaches
`shared/internal/**`"), author a repo-root `depdog.work.yaml` — units as
members, the same allow/deny + boundaries vocabulary, plus per-unit
`surfaces`. At that root a plain `depdog check` then runs the fan-out *plus*
the cross-unit pass; `--format json` adds a `cross_unit` block with
machine-readable reasons and explanations. Schema:
[schema/depdog.work.schema.json](../../schema/depdog.work.schema.json); guide:
[docs/cross-language.md](../../docs/cross-language.md).

## The main task: author & maintain depdog.yaml

### 1. Understand the codebase before writing rules

Do the analysis first — a good config mirrors the *intended* layering, which you
infer from the tree and the code, not from guesses.

1. **Map the source layout.** List the top-level source directories and one level
   below. Identify the unit depdog uses as a node: a **package/module directory**
   (Go package dir, TS folder, Python package, Java/Kotlin `src/main/<lang>/…`
   package dir, Rust `src` module dir, Ruby dir, Scala package dir, Elm module).
2. **Name the components (architectural roles), not folders one-to-one.** Look
   for layers/roles: `domain`/`core`, `service`/`usecase`, `handler`/`api`/`http`,
   `repository`/`store`/`db`, `cmd`/`main`, shared `internal`/`pkg`/`util`. Read a
   few files to confirm what depends on what.
3. **State the rules in words first**, e.g. "domain imports only the stdlib",
   "handlers may call services but not repositories directly", "no `cmd/` service
   imports another". You'll encode exactly these.
4. **Seed a draft (optional).** `depdog init --yes` scans the project and writes a
   starter `depdog.yaml` (it matches your layout against an architecture preset —
   `ddd`, `hexagonal`, `layered`, or `flat`). Treat it as a starting point to
   refine, not the answer. Use `depdog graph --format mermaid` to see the real
   dependency structure while deciding. `depdog init --merge` extends an existing
   config in place, preserving comments and formatting.

### 2. Write the depdog.yaml

Root-level `depdog.yaml`. Format (`version: 2`):

```yaml
version: 2

# Each component: a path glob (or list) + who it may import, inline.
# Stance is inferred: an `allow` list is a whitelist (only these pass);
# a `deny`-only rule is a blacklist (everything except these); no rule falls
# back to the top-level `default`.
components:
  domain:     { path: "internal/domain/**",      allow: [std] }          # whitelist: stdlib only
  service:    { path: "internal/service/**",      allow: [domain, std] }
  handler:    { path: "internal/handler/**",      deny: [repository] }    # blacklist: anything but repo (go via service)
  repository: { path: "internal/repository/**",   allow: [domain, std, external] }
  main:       { path: "cmd/**" }                                          # no rule ⇒ uses `default`

default: allow        # fallback for rule-less components. Use `deny` to fail closed.

# Optional: named reusable entries, expanded in allow/deny. A member is a
# component OR an external-module prefix (a single member may be a bare scalar),
# so a long or repeated import blob is named once instead of pasted per rule.
# (The old key name `groups:` still works — components only; use `aliases:` for external prefixes.)
aliases:
  inner: [domain, service]
  pgx: github.com/jackc/pgx/v5

# Optional: boundaries — an orthogonal, symmetric axis. Members (component names
# OR path globs) may not import across each other: peer isolation without O(n²)
# deny lists. `sealed: true` also forbids anything outside the set from importing
# IN. Shorthand `name: [a, b]` is the list form; the expanded form adds `sealed`.
boundaries:
  cmd-services:
    members: ["cmd/service-a/**", "cmd/service-b/**"]   # neither service imports the other
    sealed: true                                        # + one-way wall: nothing outside imports in

options:
  test_files: hybrid                 # default; also same-rules | relaxed
  skip: ["internal/legacy/**"]       # dirs excluded from analysis
```

**What an `allow`/`deny` entry can be:**

| Entry | Matches |
| --- | --- |
| a component name (`domain`) | that component |
| `std` | the language's standard library / builtins |
| `external` | any third-party dependency (node_modules, gems, crates, Maven deps, …) |
| `unassigned` | in-project packages no component claims |
| `"*"` | everything |
| an alias name | its members — components and/or external prefixes (expanded at parse time) |
| an import path with `/` or `.` (`golang.org/x/sync`, `lodash`) | one external dependency, by prefix |

**Rules that matter:**
- **Most-specific-wins** when component globs overlap; equal-specificity overlap
  is a config *error* (depdog tells you which).
- **`deny` beats `allow`**; a boundary crossing is a hard deny.
- **Boundaries are the tool for peer isolation** ("no service imports another",
  "these plugins don't cross") — prefer one member set over hand-written O(n²)
  `deny` lists. Component rules stay for *directional* edges (who-imports-whom).
- **Path globs are language-neutral** but must match the language's on-disk
  layout. For Java/Kotlin that means package dirs under `src/main/<lang>/…` (e.g.
  `path: "src/main/java/**/domain/**"`); for Rust, `src` module dirs (e.g.
  `src/domain/**`); for Python/Ruby/TS/Go/Scala/Elm, the package/module dirs
  directly. Run `depdog config` to see the exact node paths depdog assigns.

### 3. Validate and iterate — this is the important part

```bash
depdog check                 # 0 clean · 1 violations · 2 config/usage error
depdog config                # the compiled rules (confirm components matched what you meant)
depdog explain <from> <to>   # why a specific edge is allowed/denied, which rule/boundary fired
```

- A **config/usage error (exit 2)** means the YAML is wrong (bad glob, unknown
  ref, overlapping components, empty component). Read the message — it names the
  fix — and correct it.
- **Violations (exit 1)** mean either the rules are right and the code is wrong,
  or the rules are too strict. Use `depdog explain` and the actual code to decide
  which. Tighten or loosen rules to match the *intended* architecture; don't just
  silence real problems.
- Iterate until `depdog check` reflects the architecture the user wants.

**Adopting on a codebase that doesn't pass yet:** don't relax rules to force a
pass. Baseline today's violations and fail only on new ones:

```bash
depdog baseline                 # writes depdog.baseline.yaml
depdog check --fail-on new      # exits 1 only on NEW violations (shrink the baseline over time)
```

## Explore & debug: TUI, explain, graph

- **`depdog tui`** opens the interactive UI: a Dashboard, browsable Violations, a
  per-package imports/importers view, and a Config tab showing the compiled rules.
  On the Config tab, <kbd>m</kbd> opens an **experimental visual rule editor** (the
  "matrix"): toggle cell verdicts and make structural edits — add/rename/re-path
  components, add/remove boundary members — staged in memory and written back to
  `depdog.yaml` on save. (A bare `depdog` runs the check; `depdog tui` opens the UI.)
- **`depdog explain <component-or-package> [import]`** — why an edge is
  allowed/denied, the rule or boundary that fired (with file:line), and boundary
  membership.
- **`depdog graph --format dot|mermaid`** (`--level component|package`,
  `--violations-only`, `--focus <component>`) — the real dependency structure.

## CI

`depdog check` is exit-code-driven; add it next to tests/lint. In CI, pass the
format explicitly — a bare `depdog` gives a plain check + exit code but no
annotations:

```yaml
- run: depdog check --format github     # inline PR annotations

# or, for the code-scanning tab:
- run: depdog check --format sarif > depdog.sarif
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: depdog.sarif }
```

Combine with `--fail-on new` (and a committed `depdog.baseline.yaml`) to gate only
new violations on a legacy repo. In a **polyglot monorepo**, `depdog check --all
--format github` governs every unit in one step (see the monorepo note above).

## Editors (LSP)

`depdog lsp` speaks the Language Server Protocol over stdio: violations become
inline diagnostics at their import lines, with `explain` verdicts on hover. Point
any LSP-capable editor at the command `depdog lsp` (project marker: `depdog.yaml`).
Per-editor setup is in [docs/editors.md](../../docs/editors.md).

## Agents (MCP)

If you are an MCP-capable agent, you can call depdog's read-only tools in the
loop instead of shelling out: `depdog mcp` exposes `check` (violations as JSON),
`explain(from, to)` (an edge's verdict + deciding rule/boundary with file:line),
and `can_import(from, to)` (a cheap rule-set-only pre-check — "may this package
import that?", no graph scan) as MCP tools, plus `depdog://config` and
`depdog://components` as resources. Use `can_import` before writing an import and
`check`/`explain` to interpret violations. Read-only — authoring `depdog.yaml`
stays in your editor (validated with `check`/`config`), never over MCP. Setup and
the full tool/resource reference: [docs/mcp.md](../../docs/mcp.md).

## Best practices

- **Start permissive, tighten incrementally.** Begin with `default: allow` and
  add `allow`/`deny` to the components you actually care about; flip to
  `default: deny` only when the map is complete.
- **Name components by role, keep the set small.** A handful of meaningful layers
  beats a component per folder.
- **Reach for `boundaries` for cross-cutting isolation** instead of pairwise deny
  lists; keep component `allow`/`deny` for directional layering.
- **Wire it into CI early** so a failing architecture is a failing build.

## Output

Leave the repo with a committed `depdog.yaml` that passes `depdog check` (or a
baseline), and give the user a short plain-English summary of the components,
boundaries and rules you encoded and any violations you baselined. Point them at
`depdog tui` to explore the result interactively.
