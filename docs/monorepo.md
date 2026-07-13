# Monorepos — one command across every language in the repo

The [README](../README.md#monorepos) has the quick tour; this page is the
complete guide to checking a monorepo. depdog governs a monorepo by fanning out
over its **units** — each checked independently against its own `depdog.yaml` —
and rolling the results into one report with a single exit code.

## What a unit is

A **unit** is a directory that holds a `depdog.yaml`. It is checked exactly as a
standalone project would be: depdog resolves the unit's language from its own
marker files (`go.mod`, `package.json`, `pyproject.toml`, …), loads that
subtree's import graph with the matching adapter, and evaluates the unit's rules.

Units are **self-contained**. A unit is never checked against another unit's
rules, and an import that leaves a unit — into a sibling unit or a third-party
package — classifies as `external`, the same as any dependency outside the unit.
Governing the edges *between* units is the separate, opt-in
[`depdog.work.yaml` layer](cross-language.md), which runs on top of the fan-out
described here.

## `depdog check --all` and `--unit`

From anywhere in the repo — typically the root — `depdog check --all` discovers
every unit under the current directory and checks them all:

```bash
depdog check --all                 # every unit under the cwd
depdog check --all --format github # aggregate, as GitHub PR annotations
depdog check --all --unit web --unit services/api   # only these units
```

- `--all` turns on discovery + fan-out. Without it, `depdog check` is the
  classic single-project check (it still auto-detects a `go.work` workspace —
  see [composition with go.work](#composition-and-non-goals)).
- `--unit <dir>` (repeatable) narrows an `--all` run to specific units, matched
  by their config directory (relative to the walk root, or absolute). `--unit`
  only applies with `--all`.

The run aggregates into one report — a per-unit section for each unit, a rolled-
up summary, and a single exit code that is the **max severity** across units
(`0` clean, `1` if any unit has a violation, `2` on any config or usage error).
Every `--format` (`text`, `json`, `github`, `sarif`) emits one combined
document keyed by unit path, so a monorepo is one CI step, not N.

The single-unit commands (`explain`, `graph`, `config`, `baseline`, `tui`) stay
per-unit: `cd` into the unit you want, or point `--config` at its `depdog.yaml`.
Run one of them at a multi-unit root and depdog tells you which units it found
and points you at `--all`.

## How discovery works

`--all` walks the directory tree **down** from the current directory. Every
directory that directly contains a `depdog.yaml` roots a unit; the results are
returned in a deterministic (lexicographic) order.

The walk **prunes** whole subtrees it should never descend into:

- Any directory whose name begins with `.` (`.git`, `.venv`, `.idea`, …).
- Dependency and build output directories: `node_modules`, `vendor`, `target`,
  `dist`, `build`, `out`, `__pycache__`.
- `testdata` — Go's convention for test fixtures. depdog dogfoods this: its own
  repo carries ~two dozen fixture configs under `testdata/`, and `depdog check
  --all` at the repo root finds exactly **one** unit (the repo itself), not the
  fixtures.

Pruning happens before depdog reads a subtree, so a `depdog.yaml` that a build
tool copied into `node_modules/` or `dist/` is invisible — as it should be.
**Nested units are allowed**: a `depdog.yaml` below another `depdog.yaml` roots
its own unit, checked independently. Symlinks are not followed.

## Advisory skips — the disjoint rule and its blind spot

Discovery also collects **ungoverned** subtrees: a directory that carries a
language marker (a `go.mod`, `package.json`, …) but is governed by no unit —
formally, no unit is that directory, an ancestor of it, or a descendant of it.
These are surfaced as **advisory skips** in the report: a nudge that a
buildable subtree has no `depdog.yaml`, never a failure (they never change the
exit code).

The rule has a deliberate **blind spot**: a marker directory that *contains* a
unit, or *sits under* a unit, is considered governed and is not flagged — even
if its own top level has no rules. Example: a repo-root `go.mod` alongside a
`web/depdog.yaml` unit is *not* advised, because the root contains a governed
unit. This keeps the advice quiet for the common "root is scaffolding, real
projects live in subdirectories" layout, at the cost of not nagging about a
partially-governed root. If you want the root governed, give it its own
`depdog.yaml`.

## The `lang:` config key

Each unit auto-detects its language from its marker files. When a unit's markers
are genuinely ambiguous — e.g. a JVM project rooted only on `build.gradle`,
which both the Java and Kotlin adapters claim — set the optional top-level
`lang:` key in that unit's `depdog.yaml` to pin the adapter:

```yaml
version: 2
lang: kt        # pin the Kotlin adapter for this unit
components:
  # …
```

`lang:` resolves the ambiguity per unit, without a global flag. It takes any
registered adapter id (`go`, `ts`, `py`, `rs`, `java`, `kt`, `rb`, `scala`,
`elm` — see [docs/languages.md](languages.md)). It is **not** combinable with a
global `--lang` under `--all`: a fan-out spans multiple languages, so pinning
one language for all units makes no sense — depdog rejects `--lang` with `--all`
and directs you to the per-unit `lang:` key instead.

## Composition and non-goals

- **`go.work` fan-out composes with `--all`.** Inside a Go workspace, a plain
  `depdog check` already fans out over the workspace's member modules (each
  member with a `depdog.yaml` is checked as a single module; `--module` narrows;
  `GOWORK=off` forces the classic single-module check). `--all` is the
  language-agnostic superset: it discovers units by `depdog.yaml`, not by
  `go.work` membership, so it spans TS, Python, Rust and the rest alongside Go.
  Use `check` (auto `go.work` fan-out) inside a pure-Go workspace; use `check
  --all` from the root of a mixed repo.
- **Cross-unit edge governance is a separate, opt-in layer.** `--all` by itself
  runs many *independent* units: it does not model or enforce edges *between*
  units — a TS app importing a Go service's generated client is `external` to
  the TS unit, full stop. To govern those edges, add a repo-root
  `depdog.work.yaml`: at that root, `depdog check` (and `--all`) runs this
  fan-out *plus* a cross-unit pass judging unit-to-unit edges against
  work-file rules, boundaries and surfaces. Full guide:
  [docs/cross-language.md](cross-language.md).
