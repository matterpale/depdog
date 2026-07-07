# depdog — Backlog

Improvements, refinements, and polish beyond the M0–M5 work already shipped.
`PLAN.md` remains the design source of truth; this file is the running list of
"what next". Rough priority within each section; effort is S/M/L.

---

## Rules & policy

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

- **`init`: edit names/patterns, not just drop.** (M) `PLAN.md §4` wants the
  wizard to let you rename components and tweak patterns; today the interactive
  review can only include/exclude suggested components.
- **`init --merge`.** (M) When a config exists, add only newly-scanned
  components (dirs no existing pattern covers) instead of refusing. Needs
  yaml.Node editing to preserve the user's formatting/comments — a text append
  can't add a second `components:` block. Deferred as a careful standalone
  effort rather than rushed into a fragile shape.
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

- **Choose a license** before any public release (deliberately deferred).
- **goreleaser + Homebrew tap**; document `go install …/cmd/depdog@latest`.
- **`vhs` animated demo** in the README.
- **Second language adapter (e.g. TypeScript)** to prove the `lang` seam — the
  strongest validation that `core` is truly language-agnostic.
- **Editor/LSP integration** for inline architecture diagnostics.
