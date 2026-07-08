# depdog — Backlog

Improvements, refinements, and polish beyond the M0–M5 work already shipped.
`PLAN.md` remains the design source of truth; this file is the running list of
"what next". Rough priority within each section; effort is S/M/L.

---

## Rules & policy

- ✅ **Shipped: boundaries — orthogonal mutual-exclusion groups.** A second
  classification axis: named sets of members (components or path globs) that may
  not import across each other, *orthogonal* to most-specific-wins component
  assignment — a package sits in its layer component **and** every boundary
  whose region contains it. Collapses peer `deny`-list boilerplate into one
  declaration and makes cross-cutting isolation ("no `cmd/` service imports
  another") expressible without O(n²) deny lists or a `**` catch-all. Symmetric
  by default, with an opt-in `sealed: true` one-way flag (nothing outside may
  import in). Distinct from today's `groups` (kept as-is). Landed as a top-level
  `boundaries` key (shorthand list or `{ members, sealed }`) — purely additive,
  no config-version bump: a core `Boundary` type + per-package membership index
  (most-specific-wins, equal-specificity overlap is a config error), an
  `Evaluate` gate (cross-member deny, sealed ungrouped→member deny, deny-wins,
  `test_files`-aware, off the cycle SCC, one violation per edge) sharing a
  `DecideBoundary` path with `explain`, a machine-readable `ReasonKind`, JSON
  `boundaries` array + `boundary` field, schema, and boundary rendering in text
  / `explain` / `config` / `graph` — proven by a two-service fixture and golden
  e2e. Design and full write-up in [docs/boundaries.md](docs/boundaries.md).

- ✅ **Shipped (config v2, breaking):** merged the separate `components:` and
  `rules:` blocks into one. Each component is now `name: { path: <glob|list>,
  allow: [...], deny: [...] }` — rules are per-component, so the old two-block
  layout duplicated every name. `path` takes a scalar or a list; an entry with
  no allow/deny falls back to `default`. `version` is now `2`; a v1 config gets a
  migration error built from the user's own first component. No dual-format
  support (configs stay standalone, matching the `extends:` decision).
- ✅ **Shipped (config v2, breaking):** the top-level fallback field was renamed
  `policy` → `default` and **its default flipped**: a component with no
  allow/deny rule now imports anything (was: nothing). "No rule = no restriction"
  matches the non-blocking adoption ethos, and it fixes the old deny-default that
  made rule-less components violate on every import (including std). Set
  `default: deny` for the strict fail-closed stance. A lingering `policy:` key
  gets an actionable rename error; the `--policy` init flag became `--default`.
- ✅ **Shipped:** whitelist/blacklist stance is now inferred per rule from
  `allow` vs `deny` (an `allow` list ⇒ whitelist, a `deny`-only rule ⇒
  blacklist, otherwise the global `policy`), fixing the deny-only-under-policy-
  deny footgun. Top-level `policy` is now optional (defaults to `deny`).
  Follow-up: teach the wizard to generate mixed-stance configs.
- ✅ **Per-component external allowlists (depguard-style).** Done.
  - An allow/deny entry that looks like an import path (contains `/` or `.`)
    matches a specific external module by prefix (core.RefExternalModule).
    `deny` wins; bare identifiers still must resolve to a component/group/special.
  - `explain <from> <module>` resolves a bare external-module target and reports
    the verdict (core.RuleSet.DecideModule).
  - Wildcards (`golang.org/x/*`) are unnecessary — a bare prefix like
    `golang.org/x` already matches that module and its sub-paths (but not
    `golang.org/xyz`). JSON already renders module refs as their path (the `/`
    distinguishes them).
- ❌ **Composable/base configs (`extends:`).** Decided (owner): not building it —
  configs stay standalone.
- ✅ **Shipped:** component `groups` — a named set of components (`groups: {
  inner: [domain, core] }`) usable in any allow/deny list, expanded at parse
  time. Group names can't be reserved or collide with components.

## Engine & correctness

- ✅ **Shipped:** component-level import cycles (a↔b at the architecture level,
  which need no package cycle) are detected via Tarjan SCC and reported in text
  and JSON — advisory, never fatal.
- ✅/❌ **Build-tag / GOOS·GOARCH awareness.** Decided (owner): keep the single
  host-config load and document the limitation (now in the README's
  Limitations); no `--tags`/matrix loading for v1.
- ❌ **Nested modules & `go.work`.** Decided (owner): keep declining workspaces
  for v1 (single module only; documented in the README's Limitations).
- ✅ **Loader benchmarks + cache decision.** BenchmarkLoad (~46ms, dirty
  fixture) and BenchmarkLoadLarge (~165ms for a synthetic 300-package module)
  show loads stay well under the PLAN's 1s target, so no metadata cache is
  warranted for v1.
- ✅ **Shipped:** a `replace` fixture whose dependency is a nested local module
  (replace => ./vendored) confirms the loader still classifies it as external.
  (A full vendor-tree fixture could still be added.)

## CLI & output

- ✅ **Shipped:** `init`'s interactive review now edits, not just drops: an
  optional per-component pass renames components (rule refs follow) and rewrites
  their patterns, validated inline (collisions, reserved names, glob syntax) so
  the generated file still round-trips the check validator.
- ✅ **Shipped:** `init --merge` adds only newly-scanned components (dirs no
  existing pattern covers, matched by the checker's own pattern engine) — plus
  a starter rule under `policy: deny` — to an existing depdog.yaml. yaml.Node
  locates the end of the `components:`/`rules:` blocks and new lines are
  spliced into the original bytes, so comments, ordering and alignment survive
  verbatim; anchors/aliases and flow-style mappings are refused with the fix
  named. Name collisions rename deterministically; a no-op merge changes
  nothing; the merged file must pass config.Parse before it is written.
- ✅ **Shipped:** `graph` now has module-relative package labels, DOT clustering
  by component, `--violations-only`, and `--focus <component>` (a component and
  its direct neighbours).
- ✅ **Shipped:** `explain <from> <to>` answers whether one package may import
  another (or a component / std / external) and which rule or policy decides it
  (core.RuleSet.Decide).
- ✅ **Shipped:** `check --color=auto|always|never` — auto keeps the per-writer
  detection (honoring NO_COLOR); always/never force the profile.
- ✅ **Shipped:** `--format json` now includes the resolved top-level `policy`
  and, per component, its inferred `stance` and `allow`/`deny` refs (omitempty),
  so consumers get the full config context alongside the results.
- ✅ **Shipped:** `depdog config` dumps the compiled rule set — components,
  patterns, each component's inferred stance and rule, policy and options.

## TUI

- ✅ **Shipped: Config tab — view the compiled rules, edit via `$EDITOR`, auto
  re-check.** A fourth screen (key `4`) showing the active config path
  (module-relative) and the compiled rule set (`depdog config`'s data, rendered
  via `report.RuleSet` — `tui → report` added as an allowed edge in depdog's own
  config). A document view: `↑/↓` scroll a height-aware window with the existing
  `▲/▼ N more` markers, not a list selection. `e` there opens `depdog.yaml`
  itself in `$EDITOR` at line 1, and the editor exiting auto-fires the existing
  `r` refresh pipeline (status "config edited — re-running…") so the edited rules
  take effect on every screen — an invalid config surfaces through the existing
  re-run error path (truncated to the one-line footer), old data kept. The
  refresh hook was widened to hand back the recompiled rule set alongside the
  result (`core.Result` does not carry it). Deliberately **not** an embedded YAML
  editor — that would break the TUI's "adds navigation, not data" design note and
  reimplement a worse editor; structured rule editing stays a separate, later
  item. Proven by model tests (tab cycling, `4` selection, scroll clamping,
  config-tab `e` argv, auto-refresh on editor exit, re-render on a delivered rule
  set) and two golden frames. Evaluated with the owner 2026-07-08; design in
  [docs/tui-config-tab.md](docs/tui-config-tab.md).

- ✅ **Shipped:** the Violations and Packages lists now scroll — a height-aware
  window follows the selection with `▲/▼ N more` markers. (Custom windowing over
  the manual render, not `bubbles/viewport`.)
- ✅ **Shipped:** both the Violations and Packages screens filter with `/`
  (substring; the shared filter state applies to whichever list is active).
- ✅ **Shipped:** `e` opens the selection at its file:line in `$EDITOR`
  (tea.ExecProcess; per-editor line syntax for vim/nano/code/subl, actionable
  message when `$EDITOR` is unset); `r` re-runs the load+check pipeline
  asynchronously and refreshes every screen in place (errors keep the old data).
- ✅ **Shipped:** `?` toggles a full-screen key legend (custom overlay; swallows
  navigation until closed with `?`/esc).
- ✅ **Shipped:** the Packages screen shows a legend explaining the
  `[std]`/`[external]`/`[name]`/`✗` import tags.

## Adoption & baseline

- ~~`baseline --prune`~~ — obviated: `depdog baseline` already overwrites with
  the current violations, so it never accumulates stale entries.
- ✅ **Shipped:** `check --fail-on new` now reports how many baselined entries
  are resolved and nudges the user to rerun `depdog baseline` to shrink the file.

## Config validation & DX

- ✅ **Shipped:** components whose patterns match no package are now flagged as
  `empty-component` warnings (never fatal), in both text and JSON output.
- ✅ **Shipped:** schema/depdog.schema.json (draft-07) for editor autocomplete
  and validation, linked from the README. A test reflects over the parser's
  file struct so the schema's top-level properties can't drift, and checks
  every fixture config conforms.

## Testing & CI

- ✅ **Shipped:** formatting gates via `.golangci.yml` (v2 schema, matching CI's
  golangci-lint-action@v8) — `gofmt` + `goimports` formatters with
  `local-prefixes: github.com/matterpale/depdog` enforcing the std → external →
  internal import order; fixed the go1.26 struct-comment gofmt drift in
  `internal/lang/golang/loader.go` and `internal/core/match_test.go`. Also
  turned the (previously red) CI lint job green: the `std-error-handling`
  exclusion preset restores the classic errcheck defaults, and the one
  staticcheck QF1012 in `internal/tui/screens.go` was fixed in code.
- ✅ **Shipped:** cross-platform CI matrix — the test job now runs build/vet/test
  on `ubuntu-latest` and `windows-latest` (`fail-fast: false`); lint and the
  depdog self-check stay Linux-only. Windows enablers: `.gitattributes` forces
  LF checkouts (Windows runners default to `core.autocrlf=true`, which would
  break byte-exact golden comparisons) and the e2e harness builds the binary
  with an `.exe` suffix on Windows. Audit found no other hazards — path
  handling already goes through `filepath.Join`/`FromSlash`/`ToSlash`.
- ✅ **Shipped:** fuzz tests for `config.Parse` (FuzzParse) and
  `core.MatchPattern` (FuzzMatchPattern), with seed corpora that run under
  normal `go test`. Ran clean at ~200k+ execs each; assert real invariants
  (accepted configs have components; validated patterns match without error).

## Release & ecosystem (owner-gated)

- ✅ **Shipped:** MIT license (owner decision, 2026-07-08) — LICENSE at the
  root, README License section, PLAN decision table updated.
- ✅ **Shipped:** goreleaser release pipeline — `.goreleaser.yaml` (archives for
  linux/darwin/windows × amd64/arm64, version stamped into `cli.Version`) driven
  by `.github/workflows/release.yml` on `v*` tags. Homebrew tap config is
  included but deliberately disabled (needs a `homebrew-tap` repo + PAT; steps
  in the comment). README documents `go install` and the releases page.
- ✅ **Shipped:** `vhs` demo — `docs/demo.tape` records check → explain → TUI on
  the dirty fixture into `docs/demo.gif`, embedded at the top of the README.
- ✅ **Shipped: second language adapter — TypeScript/JavaScript.** A pure-Go
  static import scanner (`internal/lang/typescript`, no Node/`tsc`) behind the
  existing `lang.Loader` seam, selected by auto-detect (go.mod ⇒ go,
  tsconfig.json/package.json ⇒ ts) or an explicit persistent `--lang go|ts`;
  ambiguity is an actionable exit-2 error, never a silent guess. `internal/core`
  did not change and `depdog.yaml` stays language-neutral — the strongest
  validation that `core` is truly language-agnostic. See
  [`docs/typescript-adapter.md`](docs/typescript-adapter.md).
- **Editor/LSP integration** for inline architecture diagnostics.
