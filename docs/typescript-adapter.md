# TypeScript / JavaScript adapter — pure-Go import scanner

Status: **proposed**. The owner has chosen a **pure-Go static import scanner**
(no Node.js / `tsc` runtime dependency — depdog stays a single Go binary).
Design settled with the owner on **2026-07-08**. This document is the design of
record; nothing below is implemented yet.

## Goal

Add a second language adapter — TypeScript/JavaScript — behind the existing
`internal/lang.Loader` seam, so `depdog check`, `graph`, `explain`, `config` and
the TUI work on a TS/JS project with the **same** `depdog.yaml` format and the
**same** engine (`internal/core`) that today drives the Go adapter. The adapter
lives entirely in a new `internal/lang/typescript` package. **`internal/core`
does not change** — proving that the core is genuinely language-agnostic is the
whole point of the exercise.

## Where this fits in the existing seam

The contract every adapter satisfies (`internal/lang/lang.go`):

```go
type Loader interface {
    Load(ctx context.Context, patterns ...string) (*core.Graph, error)
}
```

The Go adapter (`internal/lang/golang/loader.go`) hands `core.Evaluate` a
`*core.Graph` of:

- `core.Package{ImportPath, RelDir, Imports}` — one node per package, where
  `RelDir` is the **module-relative directory** (`.` for the module root).
- `core.Import{Path, Class, RelDir, TestOnly, Positions}` — one edge per
  imported target. `Class` is `ClassStd | ClassExternal | ClassInModule`;
  `RelDir` is the **module-relative directory of the target** and is only
  meaningful for `ClassInModule`. `Position.File` is module-relative.

Everything the engine reasons about — component `path` globs, `groups`,
`boundaries`, `options.skip` — matches against `RelDir` (see
`internal/core/match.go`, `RuleSet.AssignComponent`, `RuleSet.Skipped`,
`RuleSet.BoundaryMembership`). The engine never sees a Go import path as
anything but an opaque display string; **it decides on directories**. The TS
adapter's job is therefore to produce the same directory-keyed graph shape from
TS/JS sources.

---

## 1. Node unit — directory-based (recommended)

**Decision: nodes are module-relative directories**, exactly like the Go
adapter. One node = one source directory under the project root that contains at
least one scanned `.ts/.tsx/.js/.jsx/.mjs/.cts/.mts` file. All the files in a
directory are aggregated into a single node, and edges are attributed to that
node.

Why directory-based and not file-based:

1. **Identical config semantics across languages.** `depdog.yaml` component
   `path` globs, `groups`, `boundaries` and `skip` all match module-relative
   *directories* today. If TS nodes were files, `internal/core` would need a
   second matching mode (file globs, e.g. distinguishing `internal/x/a.ts` from
   `internal/x/b.ts`), which means editing `core` — forbidden by the exercise's
   premise and a real regression in the "core is language-agnostic" property.
   Directory nodes keep `MatchPattern` and every rule path untouched.
2. **The Go analogue already exists and is understood.** A Go package *is* a
   directory. Treating a TS folder as a "package" gives users one mental model
   regardless of language and lets the same fixture-shaped golden tests apply.
3. **Architecture rules are about layers, not files.** "The handler layer may
   not import the repository layer" is a statement about directories/modules.
   Per-file granularity buys nothing at the architecture-rule altitude and
   multiplies node count (and golden-output noise).

Consequence / accepted trade-off: a directory that mixes concerns (two logical
components in one folder) collapses into one node, exactly as a Go package
would. That is a project-layout smell, not an adapter limitation, and it is
already how the Go path behaves.

### `ImportPath` for a TS node

`core.Package.ImportPath` is a display string (shown in `check`/`explain`/`graph`
output; the engine keys everything else off `RelDir`). For TS we set it to a
stable, readable label derived from the project root:

- Root: the package name from the nearest `package.json` (its `name` field), or
  the directory basename if unnamed — this is the TS analogue of the Go module
  path and is stored on `core.Graph.ModulePath`.
- A node: `<ModulePath>/<RelDir>` with `RelDir` slash-joined, matching how Go
  renders `example.test/clean/internal/util`. The root node uses `ModulePath`
  alone. This keeps text/JSON/graph output shaped like the Go golden files.

---

## 2. Import discovery — static scan (no type-checking, no Node)

A pure-Go scanner reads each source file and extracts module specifiers from the
four import surfaces TS/JS uses. It does **not** run `tsc`, does **not** shell
out to Node, and does **not** build a type-checker — it is the moral equivalent
of the Go adapter's `parser.ParseFile(..., parser.ImportsOnly)`: read enough to
find the edges, nothing more.

### What is scanned

For each file with extension `.ts .tsx .js .jsx .mjs .cjs .cts .mts`:

- **Static import**: `import X from '…'`, `import { a, b } from "…"`,
  `import * as ns from '…'`, `import '…'` (side-effect only),
  `import type { T } from '…'`.
- **Re-export**: `export { a } from '…'`, `export * from '…'`,
  `export * as ns from '…'`, `export type { T } from '…'`.
- **Dynamic import**: `import('…')` and `import(` followed by a **string
  literal** first argument.
- **CommonJS require**: `require('…')` where the first argument is a **string
  literal**.

The specifier captured is always the **string-literal argument**. The scanner
records a `core.Position{File, Line}` (module-relative file, 1-based line) for
each occurrence, mirroring the Go adapter.

### Scanning technique (robust, but not a full parser)

A hand-written lexer-lite pass over the byte stream that is *aware of the
constructs that would otherwise cause false positives*, but does **not** build an
AST:

- Track and skip **line comments** (`// …`), **block comments** (`/* … */`),
  **string literals** (`'…'`, `"…"`), and **template literals** (`` `…` ``,
  including `${…}` interpolation nesting) so a specifier-looking substring inside
  a comment or an unrelated string is never mistaken for an import. This is the
  single most important correctness rule — see the pitfall note below.
- Only *outside* those regions do we match the import/export/require/`import(`
  forms and then read the following string-literal specifier.
- Regex is used for the coarse "does this line region start an import form"
  detection, but the specifier extraction is done by scanning to the balanced
  quote so we correctly handle escapes and mixed quote styles.

This is deliberately simpler than a real TS parser and cannot be defeated by the
comment/string pitfall (below), which a naive line-regex approach would trip on.
It is also fully deterministic and dependency-free (Go stdlib + our own code
only), which keeps the `internal/lang/typescript` package's allowed imports
narrow (`core`, `lang`, `std`).

### What static scanning cannot resolve (and why that is acceptable)

Static scanning intentionally does **not** resolve:

- **Computed / dynamic specifiers**: `import(someVariable)`,
  `require(`${base}/mod`)``, `require(getName())`. The specifier is not a
  literal, so there is no edge to attribute at analysis time.
- **Fully generated module names** or specifiers assembled at runtime.
- **Type-only relationships that never appear as an `import`/`export from`**
  (e.g. types referenced through a triple-slash `/// <reference>` — out of scope
  for v1; see Non-goals).
- **Ambient module declarations** and `declare module '…'` remaps.

Why this is fine for *architecture-rule enforcement*:

1. Architecture rules govern the **stable, declared** dependency structure of a
   codebase. Dynamic/computed imports are rare, are themselves a design smell in
   a layered codebase, and by construction cannot be statically attributed to a
   directory — no tool without a running program can. depdog's Go adapter has
   the exact same blind spot for `plugin`-style dynamic loading; the property we
   promise ("the *declared* import graph obeys the rules") is unchanged.
2. A **missed** edge can only ever *under-report* (it never invents a violation
   that isn't in the source). The failure mode is conservative: we do not fail a
   build on an edge we cannot see. This matches depdog's "human-actionable, no
   false alarms" ethos.
3. The scanner **can** and does emit a low-noise diagnostic count of
   unresolvable dynamic specifiers (so a user is never silently misled), but
   these do not become graph edges.

---

## 3. Resolution & classification

Given a raw specifier found in file `F` (in directory `D`), classify it into one
of `core`'s three buckets and, for in-project targets, derive the target's
module-relative directory.

### Module root & module-relative dir

- **Project root** = the directory of the nearest `tsconfig.json`, else the
  nearest `package.json`, walking up from the loader's `Dir` (the TS analogue of
  `config.ModuleRoot` walking up to `go.mod`). This root is where `depdog.yaml`
  lives, and it is the anchor for *all* `RelDir` values — identical to how the Go
  adapter makes everything relative to the module root.
- **`RelDir`** of a node/target = `filepath.Rel(root, dir)` with
  `filepath.ToSlash`, `.` for the root itself — byte-for-byte the same
  convention as `golang.relDir`.

### Resolution rules

1. **Relative specifiers** (`./…`, `../…`): resolve against `D` on disk, using
   TS/Node resolution order:
   - exact file, then try appending each candidate extension
     (`.ts .tsx .js .jsx .mjs .cjs .cts .mts .d.ts`),
   - if the resolved path is a **directory**, resolve its `index.*` (same
     extension list).
   The resolved file's **containing directory** becomes the target; its `RelDir`
   is derived as above. `Class = ClassInModule`.
2. **`tsconfig` path aliases** (`compilerOptions.baseUrl` + `paths`): a
   bare-looking specifier that matches a `paths` pattern (e.g. `@app/*` →
   `src/*`) is rewritten to a `baseUrl`-relative on-disk path and then resolved
   exactly like a relative specifier. If it resolves inside the project root →
   `ClassInModule`; if the alias points outside the root, treat as external.
   `extends` in `tsconfig.json` is followed one level so a shared base config's
   `paths` are honored.
3. **Bare specifiers** (`react`, `lodash`, `@scope/pkg`, `@scope/pkg/sub`): a
   dependency from `node_modules`. `Class = ClassExternal`, `RelDir = ""`. We do
   **not** need `node_modules` to be present on disk — a specifier that is
   neither relative nor a matched alias nor a known builtin *is* external by
   definition. (This mirrors the Go adapter's `classifyFallback`: an import path
   whose first segment "looks external" is external without a second load.)
4. **`node:`-prefixed builtins** (`node:fs`, `node:path`, `node:crypto`) and the
   **bare builtin names** (`fs`, `path`, `crypto`, `http`, `os`, `util`,
   `stream`, `events`, …): `Class = ClassStd`, `RelDir = ""`. This is the
   TS/Node analogue of Go's standard library. The builtin list is a small,
   maintained constant set in the adapter; anything on it (with or without the
   `node:` prefix) is std, everything else bare is external.

### Edge aggregation & determinism

Within a node, multiple files importing the same target collapse to a single
`core.Import` (as the Go adapter merges duplicate import paths), with
`Positions` accumulated and sorted by `(File, Line)`. `TestOnly` is set when the
edge appears **only** in test files (see below). `Graph.Packages` are sorted by
`ImportPath` and each `Package.Imports` by `Path`, satisfying `core`'s
determinism contract (`golden` tests depend on it).

### Test-file classification (`options.test_files`)

To honor `depdog.yaml`'s existing `options.test_files` mode, the adapter marks a
file as a test if it matches the conventional patterns `*.test.*`, `*.spec.*`,
or lives under a `__tests__/` directory. An edge seen only in such files gets
`TestOnly = true`, and `core` applies the same `test_files` policy it already
applies to Go `_test.go` edges. No engine change needed.

---

## 4. Language selection

**Decision: auto-detect by marker file, with an explicit `--lang` override.**

### Auto-detect

At the point where the CLI resolves the project (today
`internal/cli/eval.go:evaluateModule` hardcodes `&golang.Loader{Dir: root}`):

- `go.mod` present at/above cwd ⇒ **Go**.
- `tsconfig.json` or `package.json` present at/above cwd ⇒ **TypeScript/JS**.
- Both present (a polyglot repo) ⇒ prefer the marker **nearest** the config /
  cwd; if still ambiguous, error with an actionable message telling the user to
  pass `--lang`.
- Neither ⇒ the existing "no go.mod found" error, extended to mention TS markers.

Detection is a new helper alongside `config.ModuleRoot` — e.g.
`config.DetectLanguage(startDir) (lang string, root string, err error)` — so
`config.Find` / `evaluateModule` can locate the config next to whichever marker
was found rather than only next to `go.mod`. `config.Find` today assumes
`go.mod`; it gains a language-neutral "root marker" notion (go.mod **or**
tsconfig/package.json) but its behavior for Go projects is unchanged.

### Explicit override

A persistent `--lang go|ts` flag on the root command (and thus available to
`check`, `graph`, `explain`, `config`, `tui`). When set, it bypasses
auto-detect and selects the adapter directly, and it changes which marker file
`Find` looks for the config beside. Invalid values are a usage error (exit 2),
consistent with the `--format`/`--color` validation style already in `check.go`.

### Why `depdog.yaml` stays language-neutral

The config format needs **no** language-specific keys:

- Component `path` globs already match module-relative **directories**; a TS
  layout (`src/domain/**`, `src/api/**`) is expressed identically to a Go layout
  (`internal/domain/**`).
- `allow`/`deny` refs are component names, other globs, or the reserved
  `std` / `external` / `unassigned` / `*` tokens — all language-neutral. "std"
  means "the platform standard library" (Go stdlib **or** Node builtins);
  "external" means "third-party dependency" (a Go module **or** an
  `node_modules` package). The adapter is what maps a concrete import onto those
  buckets, so the config author never writes anything Go- or Node-specific.
- `groups`, `boundaries`, `options.skip`, `options.test_files` are all
  directory- and class-based, hence already neutral.

The only optional, additive config surface worth considering later (not v1) is a
top-level `language: ts` hint so a checked-in config self-documents which
adapter it targets, removing reliance on marker-file detection in CI. It would
be validated in `internal/config` and read by the CLI; it does **not** touch
`internal/core`. Left out of the initial slices to keep the config schema frozen
for v1.

---

## File-level change map

Everything is additive and stays **out of `internal/core`**.

New package `internal/lang/typescript`:

- `internal/lang/typescript/loader.go` — `Loader{Dir string}` implementing
  `lang.Loader`; orchestrates root detection, file discovery (walk, honoring
  `patterns` and skipping `node_modules`/dotdirs), scan, resolve, classify,
  aggregate, sort → `*core.Graph`. `var _ lang.Loader = (*Loader)(nil)`.
- `internal/lang/typescript/scan.go` — the comment/string-aware static scanner:
  bytes → `[]specifier{raw, line}` per file.
- `internal/lang/typescript/resolve.go` — relative/alias/bare/builtin resolution
  and `Class` + target-`RelDir` derivation; tsconfig `baseUrl`/`paths`/`extends`
  loading; the Node-builtin set.
- `internal/lang/typescript/tsconfig.go` — minimal `tsconfig.json` reader (JSONC
  tolerant: strips `//` and `/* */`, trailing commas) for `baseUrl`, `paths`,
  `extends`; `package.json` `name` reader for `ModulePath`.

CLI wiring (`internal/cli`):

- `internal/cli/eval.go` — replace the hardcoded `&golang.Loader{Dir: root}`
  with adapter selection driven by detected/overridden language.
- `internal/cli/root.go` — register the persistent `--lang` flag.

Config (`internal/config`):

- `internal/config/discover.go` — add `DetectLanguage` and a language-neutral
  root-marker notion; keep Go behavior identical.

Fixtures & golden e2e:

- `testdata/fixtures/ts-clean/` — a small TS project (tsconfig + package.json +
  `depdog.yaml` + a `src/` tree with a clean layering) that exercises relative
  imports, an alias, a bare external, and a `node:` builtin.
- `testdata/fixtures/ts-dirty/` — same shape but with a layering violation, so
  `check` exits 1.
- `internal/e2e/e2e_test.go` — new `TestCheckTS*` cases mirroring the Go ones.
- `internal/e2e/testdata/ts_clean_text.golden`, `ts_dirty_text.golden`,
  `ts_dirty_json.golden` — golden output (regenerated with `-update`).

Self-check: `depdog.yaml` gains a `typescript` component
(`internal/lang/typescript/**`, `allow: [core, lang, std]` — pure scanner, no
external deps) so the dogfood self-check keeps the new package inside the
adapter seam.

Docs: this file; a short pointer added to `README`/`PLAN.md` backlog when the
work lands.

---

## Two commit slices

**Slice 1 — the adapter, pure and CLI-free.** Everything under
`internal/lang/typescript` (`loader.go`, `scan.go`, `resolve.go`,
`tsconfig.go`) plus unit tests (`scan_test.go`, `resolve_test.go`,
`loader_test.go`) that build a `*core.Graph` from in-memory / `t.TempDir`
sources and assert nodes, edges, classes and `RelDir`s directly. No CLI, no
golden e2e, no config changes. The `depdog.yaml` self-check `typescript`
component is added here so the new package is governed from the first commit.
This slice proves the adapter in isolation against `core` types.

**Slice 2 — selection, fixtures, e2e, docs.** The CLI `--lang` flag and
`eval.go` selection, `config.DetectLanguage`, the two `testdata/fixtures/ts-*`
projects, the new `TestCheckTS*` golden e2e cases and their golden files, and
this doc's status flip to "shipped". This slice proves the adapter end-to-end
through the real binary and the same `depdog.yaml` a Go project uses.

---

## Non-goals (v1)

- Running `tsc`, a type-checker, or any Node process.
- Resolving computed/dynamic specifiers, `declare module` remaps, or
  triple-slash references.
- Monorepo / workspaces (multiple `package.json` roots) — single-root only, in
  step with the Go adapter's single-module rule.
- A `language:` config key (deferred; keep the schema frozen for v1).
- JS/TS-specific config sugar — the format stays language-neutral by design.

## Risks & mitigations

- **Scanner false positives/negatives from comments & template literals.**
  Mitigated by the comment/string-aware pass (not naive line regex) and targeted
  unit tests for each pitfall.
- **tsconfig `paths` corner cases** (multiple targets, `extends` chains,
  wildcard positions). Mitigated by scoping v1 to `baseUrl` + single-level
  `paths`/`extends` and documenting the limit; unresolved aliases fall back to
  external rather than crashing.
- **`ImportPath` display drift from Go golden expectations.** Mitigated by
  deriving `ImportPath` as `<ModulePath>/<RelDir>` so text/JSON/graph output is
  shaped like the existing Go golden files.
- **Determinism** (map iteration, filesystem walk order). Mitigated by the same
  explicit sorts the Go adapter uses; golden e2e is the backstop.
