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
- **Per-component external allowlists (depguard-style).** (L) Let `external`
  carry sub-rules so a component can allow only specific third-party modules
  (e.g. `external: { allow: ["github.com/google/uuid"] }`). Currently third
  parties are one opaque bucket.
- **Composable/base configs.** (M) `extends:` a shared base `depdog.yaml` (or a
  named built-in preset) so orgs can factor common rules out of each repo.
- **Component aliases / groups.** (S) Let a rule reference a set of components by
  a group name (e.g. `inner: [domain, core]`) to cut repetition.

## Engine & correctness

- **Component-level import-cycle detection.** (M) `go vet` catches package
  cycles; cycles *between components* (a→b→a at the architecture level) are the
  interesting ones and nothing surfaces them today.
- **Build-tag / GOOS·GOARCH awareness.** (L) The loader does one metadata load;
  edges behind build tags for other platforms are invisible. Offer
  `--tags`/matrix loading so platform-specific imports are checked.
- **Nested modules & `go.work`.** (L) Both are declined today with a message.
  Support checking a workspace (each module in turn) and nested modules without
  crashing.
- **Loader benchmark + optional cache.** (M) The loader is the bottleneck;
  `PLAN.md` says "measure first". Add a benchmark over a large synthetic module,
  then decide whether a metadata cache is worth it.
- **`replace`/vendored edge cases.** (S) Add fixtures for `replace` to a nested
  path and a vendored tree; confirm classification stays correct.

## CLI & output

- **`init`: edit names/patterns, not just drop.** (M) `PLAN.md §4` wants the
  wizard to let you rename components and tweak patterns; today the interactive
  review can only include/exclude suggested components.
- **`init` merge mode.** (M) When a config exists, offer to show a diff / merge
  in newly-scanned packages instead of only refusing without `--force`.
- ✅ **Shipped:** `graph` now has module-relative package labels, DOT clustering
  by component, `--violations-only`, and `--focus <component>` (a component and
  its direct neighbours).
- **`explain` an edge.** (S) `explain <from> <to>` to answer "why is this
  specific import allowed/denied", showing the rule and stance that decided it.
- ✅ **Shipped:** `check --color=auto|always|never` — auto keeps the per-writer
  detection (honoring NO_COLOR); always/never force the profile.
- **Richer JSON.** (S) Include the resolved policy and each component's rule text
  in `--format json` so consumers have full context (keep additive/stable).
- ✅ **Shipped:** `depdog config` dumps the compiled rule set — components,
  patterns, each component's inferred stance and rule, policy and options.

## TUI

- ✅ **Shipped:** the Violations and Packages lists now scroll — a height-aware
  window follows the selection with `▲/▼ N more` markers. (Custom windowing over
  the manual render, not `bubbles/viewport`.)
- **Search Packages.** (S) The Violations screen now filters with `/` (substring
  over component/import/rule). Still open: fuzzy-search on the Packages screen.
- **`$EDITOR` + re-run.** (M) `e` opens the selected file at its line in
  `$EDITOR`; `r` re-runs the check in place (a step toward watch mode).
- **Help overlay.** (S) `?` toggles a key legend via `bubbles/help`.
- **Import-class legend & consistent color-coding** on the Packages screen. (S)

## Adoption & baseline

- **`baseline --prune`.** (S) Drop baseline entries that are no longer
  violations, so the ratchet file doesn't accumulate stale grandfathering.
- ✅ **Shipped:** `check --fail-on new` now reports how many baselined entries
  are resolved and nudges the user to rerun `depdog baseline` to shrink the file.

## Config validation & DX

- ✅ **Shipped:** components whose patterns match no package are now flagged as
  `empty-component` warnings (never fatal), in both text and JSON output.
- **JSON Schema for `depdog.yaml`.** (S) Ship a schema for editor autocomplete
  and validation; link it from the docs.

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
