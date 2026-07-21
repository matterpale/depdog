# Compatibility — what 1.0 promises

From 1.0 on, depdog follows [semantic versioning](https://semver.org/): the
surfaces below are **stable**, and anything that breaks them ships only in a
major bump. This page draws the line precisely so you can build on depdog —
scripts, CI gates, agents, dashboards — without a minor release moving the
ground under you.

The rule of thumb: **the machine-readable contract is stable; the
human-readable presentation is not.** If a tool consumes it, we hold it steady.
If a person reads it, we reserve the right to make it nicer.

## Stable under semver

Breaking changes to any of these wait for a major version. Within a major line,
changes are **additive only** — new keys and new fields may appear in a minor
release, but existing ones never change name or meaning.

- **The config v2 YAML schema** — [`schema/depdog.schema.json`](../schema/depdog.schema.json)
  and the `depdog.yaml` vocabulary it describes (`version`, `components`,
  `allow`/`deny`, `aliases` — with `groups` retained as a deprecated synonym —
  `boundaries`, `default`, `options`, `lang`). New keys may be added in a minor;
  the meaning of an existing key never changes under a minor. A key may be
  *soft-deprecated* in a minor — it keeps working, with a one-line notice — and
  only *removed* in a major, the way `groups` became `aliases`. A config that
  validates today keeps validating and keeps meaning the same thing.
- **`--format json` — the single-unit report.** Top-level `module`, `default`,
  `violations`, `warnings`, `components`, `boundaries`, `cycles`, `stats`, with
  their `snake_case` field names (`from_package`, `test_only`, `duration_ms`, …).
  Absent collections encode as `[]`, never `null`. Fields are added, never
  renamed or removed. Each violation also carries an `explanation` — a
  plain-English WHY-plus-fix for the denied edge, added additively; the
  machine-readable `reason`/`kind` classification alongside it (empty for an
  ordinary rule violation, `boundary`/`boundary-sealed` for a boundary crossing)
  is unchanged and remains the field to branch on. The wording of `explanation`
  itself is human-facing prose and may be refined within a major.
- **`--format json` — the aggregate (multi-unit) envelope.** A `go.work`
  fan-out or a `depdog check --all` run emits the envelope:
  `root` (the walk-root's basename), `units[]` (each carries `dir` + `lang`
  plus the same per-unit report fields as the single-unit report), `skipped[]`
  (each `{dir, reason}` — a marker directory with no `depdog.yaml`), and rolled-
  up `stats`. The presence of the `units` array is the discriminator between the
  two shapes: a run that analyzes exactly one unit with nothing skipped emits
  the single-unit report at the top level (no envelope), so a single-project
  consumer is never surprised by an envelope. Envelope fields are additive —
  including the `cross_unit` block a
  [`depdog.work.yaml` run](cross-language.md) adds, which follows the same
  rules once present: fields are added, never renamed or removed.
- **Exit codes.** `0` clean, `1` violations found, `2` config or usage error.
  These are the CI/agent contract and do not move.
- **Documented CLI flag semantics.** The behavior of documented flags —
  `check`'s `--format`, `--fail-on`, `--all`, `--unit`, `--module`, `--config`,
  `--color`; `init`'s `--preset`, `--default`, `--merge`, and friends — is held
  stable. New flags may be added; the documented meaning of an existing flag
  does not change under a minor.

## Explicitly unstable

These may change in **any** release, including a patch. Do not parse or assert
against them:

- **Text and TUI rendering.** The human-facing `--format text` layout, wording,
  spacing, color, and the entire interactive TUI are presentation and evolve
  freely. Machines should consume `--format json`.
- **Log and stderr wording.** Progress lines, hints, and error message text are
  not a contract. Branch on **exit codes**, not on the words on stderr.
- **Go internals.** depdog ships as a CLI, not a library — the entire codebase
  lives under `internal/`, so there is **no exported Go API** to depend on and
  no Go-source compatibility promise. Import paths, package layout, and function
  signatures can change at will.
- **SARIF and GitHub cosmetic detail.** The stable part of `--format sarif` and
  `--format github` is the identity of each finding — the reported **path** and
  the **rule identity**. Everything else (message phrasing, help URIs, rule
  metadata, ordering nuances) is cosmetic and may change.

## How the contract is enforced

The stable surface is not a promise on paper — it is a regression wall of tests
that already exist. Breaking any stable shape turns one of these red:

- **The byte-level JSON golden e2e corpus** (`internal/e2e/testdata/*.golden`,
  driven from `internal/e2e/e2e_test.go`). Every `--format json` shape is pinned
  to a committed golden and compared byte-for-byte: the aggregate envelope in
  `monorepo_json.golden` and `ws_json.golden`, the single-unit report in
  `dirty_json.golden`, `boundaries_json.golden`, and the per-language
  `*_dirty_json.golden` set. Rename or restructure a field and the diff fails
  the build.
- **The schema-reflection test** `TestSchemaMatchesFileStruct`
  (`internal/config/schema_test.go`). It reflects the config parser's struct
  against `schema/depdog.schema.json` and fails if either side grows or drops a
  key the other doesn't have — the schema physically cannot drift from what the
  parser accepts.
- **The single-unit byte-identity guard** `TestCheckMonorepoUnitNarrowsToSingle`
  (`internal/e2e/e2e_test.go`). It asserts that narrowing a fan-out to one unit
  (`--all --unit <one>`) produces output **byte-identical** to running `check`
  directly inside that unit — so the single-project output can never silently
  drift into the envelope shape.

Together these are the contract's regression surface: the compatibility promise
holds exactly as far as these tests are green.
