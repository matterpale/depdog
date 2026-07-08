# depdog — Project Plan

> A CLI + TUI tool that keeps a codebase's internal dependencies pointing in the right direction.
> Go-only at first; architected so other languages can be added later.

---

## 1. Vision

Architecture rules ("domain imports nothing but std-lib", "handlers never import repositories")
usually live in people's heads or in a wiki, and they rot. depdog makes them executable:

1. **Initialize** a repo (`depdog init`) — a short wizard inspects the module layout and helps
   you map packages to named *layers*, optionally starting from a preset (DDD, hexagonal, …).
2. **Declare rules** in a small, readable config file (`depdog.yaml`) — who may import whom.
3. **Check** (`depdog check`) — every import edge in the module is evaluated against the rules;
   violations are reported with file:line, and a non-zero exit code makes it CI-ready.
4. **Explore** (`depdog` / `depdog tui`) — a Bubble Tea TUI to browse layers, violations, and the
   dependency graph interactively.

Prior art (deptrac for PHP, ArchUnit for Java, go-arch-lint / arch-go / depguard for Go) validates
the problem. depdog's differentiators: the guided init experience, a first-class TUI, and a
baseline/ratchet workflow for adopting rules in brownfield codebases.

---

## 2. Core domain model

These concepts are language-agnostic and form the heart of the tool:

| Concept | Meaning |
|---|---|
| **Component** (layer) | A named set of packages, defined by path patterns relative to the module root (e.g. `domain = internal/domain/**`). |
| **Dependency class** | What an import resolves to: a component, `std` (standard library), `external` (third-party module), or `unassigned` (in-module package matched by no component). |
| **Rule** | For a source component: which dependency classes/components it may or may not import. |
| **Policy** | What happens when no rule matches: `deny` (whitelist stance) or `allow` (blacklist stance). Both are first-class; each project picks its own. |
| **Violation** | A concrete import edge that breaks a rule, carrying: source package, imported path, rule violated, and every file:line where the import appears. |
| **Baseline** | A recorded snapshot of known violations that are tolerated (grandfathered); only *new* violations fail the check. |

The rule engine operates on an abstract graph of `(package, imports, positions)` — nothing Go-specific.
Go knowledge lives entirely in a *language adapter* that produces that graph. That's the seam for
future languages.

---

## 3. Configuration format

`depdog.yaml` at the repo root, next to `go.mod` (`--config` overrides; no upward search
beyond the module root). Design goals: readable in a code review, diff-friendly, no regex
unless asked for.

Whitelist and blacklist styles are **equally first-class** — the top-level `default` decides the
fallback for a component with no rule (optional, and itself defaulting to `allow`), and each
project chooses its stance (the wizard asks):

- **Whitelist style:** `default: deny` + `allow` lists — anything not explicitly allowed is a
  violation.
- **Blacklist style:** `default: allow` (the default, so it can be omitted) + `deny` lists —
  everything passes except what is explicitly forbidden.

They can be mixed; an explicit `deny` always beats an `allow`. Example encoding the DDD style
from the brief (whitelist flavor):

```yaml
version: 2

# Module path is auto-detected from go.mod; can be overridden.
# module: github.com/acme/shop

# Each component lists its path glob(s) and, inline, who it may import.
# Anything else inside the module that no component claims is "unassigned".
components:
  main:       { path: "cmd/**",                 allow: ["*"] }                   # wires everything together
  domain:     { path: "internal/domain/**",     allow: [std] }                   # the DDD heart: std-lib only
  handler:    { path: "internal/handler/**",    allow: [domain, std, external] }
  service:    { path: "internal/service/**",    allow: [domain, std, external] }
  repository: { path: "internal/repository/**", allow: [domain, std, external] }
  # handler/service/repository cannot import each other simply because
  # they are not in each other's allow lists (default: deny).

default: deny          # a component with no rule may import nothing (strict mode)

options:
  test_files: hybrid          # default — see semantics below; also: same-rules, relaxed
  skip: ["internal/legacy/**"]  # package dirs excluded from analysis
```

The same cross-import ban in blacklist flavor (`default: allow` is the default, shown here for
clarity):

```yaml
default: allow
components:
  handler:    { path: "internal/handler/**",    deny: [service, repository] }
  service:    { path: "internal/service/**",    deny: [handler, repository] }
  repository: { path: "internal/repository/**", deny: [handler, service] }
  domain:     { path: "internal/domain/**",     deny: [handler, service, repository, external, unassigned] }
```

Semantics:

- Each component's value is a mapping: `path` (a glob or a list of globs) plus an optional
  `allow`/`deny` list. `allow`/`deny` entries are component names or the specials `std`,
  `external`, `unassigned`, `"*"`. `deny` always wins over `allow`.
- **Per-component stance is inferred from word choice.** For an edge a component's lists don't
  mention,
  the fallback is read off the rule itself: an `allow` list makes the component a *whitelist*
  (only listed imports pass); a `deny`-only rule makes it a *blacklist* (everything passes
  except what's listed). A component with no rule — or a rule with neither list — falls back to
  the top-level `default`, which is itself `allow` (open) unless set to `deny`. So an unruled
  component imports anything by default; `default: deny` flips that to fail-closed. This lets
  stances mix per component and avoids the trap where a `deny`-only rule silently forbade
  everything. `default` is the fallback for rule-less components, not a global override.
- Pattern matching: recursive doublestar globs against module-relative package dirs, and
  **most specific wins** when patterns overlap. Elaborate or deep trees are covered with a
  catch-all plus carve-outs instead of exhaustive per-directory patterns:

  ```yaml
  components:
    app:    { path: "internal/**" }          # catch-all for everything under internal/
    domain: { path: "internal/domain/**" }   # deeper pattern wins for the domain subtree
  ```

  Equally specific overlapping patterns are an ambiguity error for the affected package.
- In-module packages claimed by no component are **always listed as warnings** in `check`
  output (any stance, any format) but never fail the build by themselves — unmapped packages
  are how rule sets rot, so they stay visible without blocking adoption.
- `test_files: hybrid` (the default): `_test.go` files may import any **external** module
  (testify, gomock, …) but component-to-component rules still apply — test-only coupling
  between layers is still flagged. Alternatives: `same-rules` (strict), `relaxed` (exempt).
- `external` can later grow sub-rules (e.g. per-module allowlists), but v1 treats third-party
  as one bucket.

Presets shipped with `init`: `ddd` (the layout above), `hexagonal` (core / ports / adapters),
`layered` (ui → app → domain → infra), `flat` (empty scaffold with comments).

---

## 4. CLI surface

Cobra for command structure, wrapped with charmbracelet **fang** for polished help/errors/manpages.

| Command | Behavior |
|---|---|
| `depdog init [--preset ddd] [--yes]` | Wizard (charmbracelet **huh** forms): detects `go.mod`, scans top-level package dirs, proposes a component mapping (preset patterns matched against reality), lets the user adjust names/patterns, writes `depdog.yaml`. `--yes` accepts suggestions non-interactively. Refuses to overwrite an existing config without `--force`. |
| `depdog check [./...]` | Loads config, builds the import graph, evaluates rules. Human output by default; `--format json\|sarif\|github` for machines/CI. Exit codes: `0` clean, `1` violations, `2` config/usage error. `--fail-on new` honors the baseline. |
| `depdog baseline` | Writes current violations to `depdog.baseline.yaml` so `check --fail-on new` only flags regressions. The ratchet: shrink it over time. |
| `depdog graph [--format dot\|mermaid] [--level component\|package]` | Emits the dependency graph, violations highlighted, for READMEs and docs. |
| `depdog explain <pkg-or-component>` | Answers "why is this red?": shows which rule fired and the exact import chain(s), or for a clean package, which rules constrain it. |
| `depdog` (bare) / `depdog tui` | Launches the TUI. Bare invocation without a config points at `init`. |

Human `check` output sketch (lipgloss-styled, grouped by rule):

```
depdog check — github.com/acme/shop

✗ domain may only import std (2 violations)
  internal/domain/order
    → github.com/google/uuid          order.go:7
  internal/domain/pricing
    → github.com/acme/shop/internal/repository
                                       pricing.go:12   (also: discount.go:5)

✗ handler may not import repository (1 violation)
  internal/handler/checkout
    → github.com/acme/shop/internal/repository/orders
                                       checkout.go:15

3 violations · 2 rules broken · 41 packages checked in 0.4s
```

---

## 5. Analysis engine (Go adapter)

- **Loading:** `golang.org/x/tools/go/packages` with a minimal mode
  (`NeedName | NeedFiles | NeedImports | NeedModule`) — no type-checking, so it stays fast even on
  large repos, while still getting accurate module/std/external classification and build-tag
  handling for free. Positions of import declarations come from a lightweight `go/parser`
  pass (`ImportsOnly`) over the files of packages that have violations — pay the parsing cost
  only where it's needed for reporting.
- **Classification:** std-lib via `packages` metadata; in-module vs external via `go.mod` module
  path prefix (with awareness of `replace` directives and nested modules — nested modules are
  out of scope for the check but must not crash it).
- **Test files:** `_test.go` files (including external `_test` packages) tracked separately so
  the `test_files` modes apply: `hybrid` (default) auto-allows `external` for test-only imports
  while still enforcing component rules; `same-rules` and `relaxed` do what they say.
- **Evaluation:** pure function `Evaluate(graph, config) → []Violation`. Deterministic ordering
  (by rule, then package, then import) so output and golden tests are stable.
- **Performance target:** a 500-package module checks in well under a second after the initial
  load; the loader is the bottleneck and `packages.Load` in metadata mode is roughly `go list`
  speed. No caching layer in v1 — measure first.

---

## 6. TUI (charmbracelet stack)

Stack: **bubbletea** (runtime) + **bubbles** (list, table, viewport, help) + **lipgloss** (style)
+ **huh** (init wizard, shared with the CLI).

Three screens for v1, tab-switchable:

1. **Dashboard** — component summary table: packages per component, outgoing edges, violation
   count; a compact component-level matrix (who imports whom, ✗ on illegal edges); overall status.
2. **Violations** — filterable list (by component, by rule) with a detail pane: the offending
   import, each file:line occurrence, and the rule text. `e` opens the file at the line in
   `$EDITOR`; `r` re-runs the check.
3. **Packages** — browse packages grouped by component; select one to see its imports (classified
   and color-coded) and its importers. This is `explain`, interactive.

Later (not v1): rule editing from the TUI, watch mode (re-check on file save), graph rendering.

Design note: the TUI is a pure consumer of the engine's result types (`Result`, `Violation`,
`Graph`). Everything it displays must also be obtainable via the CLI in `--format json` — the
TUI adds navigation, not data.

---

## 7. depdog's own architecture (dogfooding)

depdog must pass `depdog check` on itself from the first milestone. Module path:
`github.com/matterpale/depdog`. Layout:

```
cmd/depdog/            main — wires everything, may import all
internal/core/         domain model: Graph, Component, Rule, Violation, Evaluate()
                       → imports std-lib only (our own "domain" layer)
internal/config/       depdog.yaml + baseline load/validate  → core, std, yaml lib
internal/lang/         Language adapter interface (defined against core types)
internal/lang/golang/  Go loader (x/tools/go/packages)       → core, lang, std, external
internal/report/       text/json/sarif/github formatters     → core, std, lipgloss
internal/cli/          cobra commands                        → everything except tui internals
internal/tui/          bubbletea app                         → core, report, std, external
```

Key inversions: `core` knows nothing about Go-the-analyzed-language, YAML, or terminals.
`lang` defines the adapter interface; `golang` implements it. Adding TypeScript later means one
new package under `lang/` and zero changes to `core`.

The repo's own `depdog.yaml` encodes exactly this and runs in CI next to tests and lint.

---

## 8. Milestones

**M0 — Skeleton (small)**
`go.mod`, directory layout above, cobra+fang root command, CI (build, test, `golangci-lint`),
the repo's own `depdog.yaml` written by hand (checked by depdog itself from M1 on).

**M1 — Engine + `check` (the core; largest milestone)**
Config parsing & validation with good error messages (unknown component in a rule, overlapping
patterns, bad glob). Go loader. Rule evaluation. Plain-text output, exit codes, `--format json`.
Golden tests against fixture modules in `testdata/` (a clean DDD module, a violating one, one
with build tags, one with external test packages). **After M1 the tool is already useful in CI.**

**M2 — `init` wizard**
Preset library; repo scan + suggestion heuristics (match preset patterns against existing dirs,
propose components for unmatched top-level packages); huh-based interactive flow; `--yes` path;
generated config round-trips through the M1 validator.

**M3 — Adoption & CI polish**
`baseline` + `--fail-on new`; lipgloss-styled human output (the sketch in §4); `--format sarif`
and `github` (inline PR annotations); `options.test_files` and `options.skip`.

**M4 — TUI**
Dashboard, Violations, Packages screens; `$EDITOR` integration; `teatest`-based snapshot tests.

**M5 — Graph & explain, v0.1 release**
`depdog graph` (dot + mermaid, component and package level), `depdog explain`; README with
animated demo (vhs); goreleaser (binaries + homebrew tap); tag v0.1.0. The project is
currently unlicensed (private) — a license must be chosen before this milestone publishes
anything.

**Post-v0.1 backlog (explicitly not now)**
Watch mode; rule editing in the TUI; external-dependency allowlists per component
(depguard-style); second language adapter; editor/LSP integration; import-cycle detection
(go vet catches package cycles, but component-level cycles are interesting).

---

## 9. Testing strategy

- **Engine:** table-driven unit tests for `Evaluate` (rules × dependency classes matrix);
  fixture Go modules under `testdata/` loaded through the real adapter; golden files for every
  output format. Determinism is a hard requirement — sort everything.
- **Config:** a corpus of invalid configs, each asserting a specific, human-readable error.
- **CLI:** integration tests executing the built binary against fixtures (exit codes, stdout).
- **TUI:** `charmbracelet/x/exp/teatest` for model-update logic and golden frames; keep
  view functions thin so most logic is testable without a terminal.
- **Dogfood:** `depdog check` on depdog itself in CI — a failing architecture is a failing build.

---

## 10. Decisions made (and why)

| Decision | Choice | Rationale |
|---|---|---|
| Config file | `depdog.yaml`, YAML | Reviewable, commentable; the wizard writes it so hand-authoring is optional. |
| Rule styles | Whitelist & blacklist both first-class | `default` + per-component `allow`/`deny` lists; the wizard asks which stance a project takes. |
| Config shape | Single merged `components` block (v2) | Rules are per-component, so a separate `rules` block duplicated every name; each entry now carries `path` + inline `allow`/`deny`. Merged 2026-07-08 with the owner. Version bumped 1→2 with a migration error; no dual-format support. |
| Fallback field | `default: allow\|deny`, defaulting to `allow` (v2) | Renamed from `policy` and flipped: a component with no rule now imports anything ("no rule = no restriction"), matching the tool's non-blocking adoption ethos and avoiding the old deny-default that made rule-less components violate on everything. `default: deny` opts into fail-closed. Owner decision 2026-07-08; also renamed the `--policy` init flag to `--default`. |
| Bare `depdog` | Opens TUI | The TUI is the product's face; CI always says `check` explicitly anyway. |
| Package loading | `x/tools/go/packages`, metadata mode | Correctness (build tags, module resolution) without type-checking cost. |
| Component matching | Recursive globs, most specific wins | Catch-all + carve-out covers elaborate trees; equal specificity is an error. |
| Unassigned packages | Always warned, never fatal on their own | Visible rot-prevention without blocking adoption. |
| Test files | `hybrid` default | External test libs allowed; cross-component test coupling still flagged. |
| Config discovery | Repo root only, next to `go.mod` | One canonical location; `--config` flag for special cases. |
| Workspaces (`go.work`) | Detect & decline in v1 | Explicit message instead of misbehavior; support is post-v0.1. |
| CLI framework | cobra + fang | Standard ecosystem + charmbracelet polish, consistent with the TUI stack. |
| Module path | `github.com/matterpale/depdog` | Confirmed with the owner. |
| License | MIT | Chosen by the owner on 2026-07-08, ahead of the v0.1.0 release. |

All previously open questions were resolved with the project owner on 2026-07-06 and folded
into the decisions table above.
