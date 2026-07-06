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
- **Per-component external allowlists (depguard-style).** In progress.
  - ✅ Shipped: an allow/deny entry that looks like an import path (contains `/`
    or `.`) matches a specific external module by prefix (core.RefExternalModule).
    `deny` wins; bare identifiers still must resolve to a component/group/special.
  - Follow-up slices: teach `explain <from> <to>` to resolve a bare external
    module target; wildcard module patterns (`golang.org/x/*`); surface module
    refs distinctly in `--format json` (currently rendered as plain strings).
- **Composable/base configs.** (M) `extends:` a shared base `depdog.yaml` (or a
  named built-in preset) so orgs can factor common rules out of each repo.
- ✅ **Shipped:** component `groups` — a named set of components (`groups: {
  inner: [domain, core] }`) usable in any allow/deny list, expanded at parse
  time. Group names can't be reserved or collide with components.

## Engine & correctness

- ✅ **Shipped:** component-level import cycles (a↔b at the architecture level,
  which need no package cycle) are detected via Tarjan SCC and reported in text
  and JSON — advisory, never fatal.
- **Build-tag / GOOS·GOARCH awareness.** (L) The loader does one metadata load;
  edges behind build tags for other platforms are invisible. Offer
  `--tags`/matrix loading so platform-specific imports are checked.
- **Nested modules & `go.work`.** (L) Both are declined today with a message.
  Support checking a workspace (each module in turn) and nested modules without
  crashing.
- **Loader cache.** (M) BenchmarkLoad now measures the loader (the bottleneck) —
  ~46ms for the small dirty fixture, dominated by `go/packages` startup. Still
  open: measure a large synthetic module, then decide whether a metadata cache
  is worth it.
- ✅ **Shipped:** a `replace` fixture whose dependency is a nested local module
  (replace => ./vendored) confirms the loader still classifies it as external.
  (A full vendor-tree fixture could still be added.)

## CLI & output

- **`init`: edit names/patterns, not just drop.** (M) `PLAN.md §4` wants the
  wizard to let you rename components and tweak patterns; today the interactive
  review can only include/exclude suggested components.
- **`init` merge mode.** (M) When a config exists, offer to show a diff / merge
  in newly-scanned packages instead of only refusing without `--force`.
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
- **`$EDITOR` + re-run.** (M) `e` opens the selected file at its line in
  `$EDITOR`; `r` re-runs the check in place (a step toward watch mode).
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

- **Turn on formatting/lint gates.** (S) Add a `.golangci.yml` enabling `gofmt`/
  `goimports` (and the import-order convention), and fix the pre-existing gofmt
  drift in `internal/lang/golang/loader.go` and `internal/core/match_test.go`
  (go1.26 struct-comment alignment). CI's default linters don't gate formatting
  today.
- **Cross-platform CI matrix.** (S) Add a Windows runner to exercise path
  handling (`filepath.ToSlash`, module-relative dirs).
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
