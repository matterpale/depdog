# TUI Config tab — view the compiled rules, edit via `$EDITOR`, auto re-check

Status: **planned**. Evaluated with the owner on 2026-07-08: the original proposal
was editing the YAML *inside* the TUI; that was rejected (reasons below) in favor
of a read-only panel with an `$EDITOR` round-trip and automatic re-check.

## Problem

The loop the TUI should serve — *tweak a rule, see what turns red* — has no
first-class path today. Editing `depdog.yaml` means a second terminal (or `:e` in
a suspended editor), then `r` back in the TUI. And the TUI shows the *results* of
the config but never the config itself, even though the inferred per-component
stances (config v2's subtle part) are exactly what you want in view while
tweaking rules.

## Decision: no embedded editor

Editing the YAML in a TUI widget (`bubbles/textarea` or similar) was rejected:

1. **It breaks the TUI's design constraint.** PLAN §6 and the `internal/tui`
   package comment: the TUI is a pure consumer of engine types — "it adds
   navigation, not data". An embedded editor makes it a second *write path* to
   `depdog.yaml` (after `init` / `init --merge`), and this project treats user
   YAML bytes as sacred (`init --merge` splices bytes to keep comments and
   formatting verbatim). A textarea holding the whole file inherits none of that.
2. **It reimplements a strictly worse editor** — no syntax highlighting, shallow
   undo, none of the user's keybindings — for an audience that has a configured
   `$EDITOR`, which the TUI already integrates (`e`, `tea.ExecProcess`,
   per-editor line syntax).
3. **The hard part already exists.** The refresh hook wired in
   `internal/cli/tui.go` re-runs the full pipeline *including re-reading the
   config from disk*, and the failed-re-run path ("re-run failed: … — fix it and
   press r again", old data kept) is battle-tested.
4. **Large failure surface for the value:** unsaved-changes state, crash safety
   mid-edit, invalid-YAML-on-save semantics, file-changed-on-disk conflicts,
   plus heavy golden-frame testing for an editing widget.

## Behavior

A fourth tab, **Config** (key `4`), showing:

- the active config path (module-relative), and
- the **compiled rule set** — the same content as `depdog config`: `default`,
  `test_files`, `skip`, then each component's patterns, inferred stance, and
  allow/deny refs.

Keys on that tab:

- `up/down` scroll the document (this tab is a document, not a list — a scroll
  offset, not a selection), with the existing `▲/▼ N more` window markers.
- `e` opens **`depdog.yaml` itself** in `$EDITOR` (line 1). When the editor
  exits, the existing refresh pipeline fires automatically — status
  "config edited — re-running…" — so the edited rules take effect on every
  screen, including this one. An invalid config flows through the existing
  `refreshMsg.err` path: the parser's actionable message in the footer, old data
  kept.

### Why the compiled rule set, not raw YAML

`depdog config` is the CLI command that makes this view legal under the
"everything the TUI displays is obtainable via the CLI" note; the compiled view
shows the *inferred stances*, which raw YAML doesn't; and raw YAML is exactly
what `$EDITOR` is for. One source of truth: render via `report.RuleSet`
(`tui → report` is an allowed edge in depdog's own config).

## Implementation steps

1. **Plumbing** (`internal/cli/tui.go`, `internal/tui/model.go`): a
   `tui.WithConfig(path string, rs *core.RuleSet)` option — `launch()` already
   holds `ev.ConfigPath` and `ev.Rules`. Widen the refresh hook to
   `func() (*core.Result, []core.PackageView, *core.RuleSet, error)` and add the
   rule set to `refreshMsg`, so the panel refreshes after edits (`core.Result`
   does not carry the rule set; the JSON renderer takes it separately, so the
   TUI must too). The config path is stable across refreshes and stays static in
   the model.
2. **The tab** (`model.go`, `screens.go`): `tabConfig` before `numTabs`, title
   "Config", `4` shortcut, `configView()` rendering the path plus
   `report.RuleSet` output into the styled frame; a new scroll-offset field with
   height-aware windowing. Help overlay, footer hints updated.
3. **Edit round-trip** (`edit.go`): a `tabConfig` branch in the `e` handling
   builds the editor argv for the config file at line 1; a flag records that the
   editor was launched from this tab, and `editorFinishedMsg` with that flag set
   fires `startRefresh()`. No new error handling. Watch one detail: multi-line
   config errors vs the one-line footer — truncate if needed.
4. **Tests, same change** (three tiers per convention): model tests — tab
   cycling with the new `numTabs` (existing cycling tests need updating), `4`
   selection, scroll clamping, config-tab `e` argv, auto-refresh on editor exit,
   re-render on a delivered rule set; golden frames for the new screen.
5. **Docs:** README TUI section, BACKLOG entry, and the PLAN §6 design note
   survives intact — the panel displays `depdog config`'s data; editing stays in
   `$EDITOR`.

## Out of scope (separate, later items)

- **Structured rule editing** (toggle an allow entry with a keystroke, huh
  forms) — the true "rule editing in the TUI" backlog item. It must build on the
  byte-splicing machinery from `init --merge` to preserve user formatting, and
  deserves its own design doc if wanted.
- **Stretch (cheap, later):** jump `e` to the *selected component's* line via
  `yaml.Node` positions instead of line 1.

## Effort

S–M. No engine, config-schema, or report-format changes; one new screen, one
widened hook signature, and a two-line editor-return hook, plus the test and
docs sweep.
