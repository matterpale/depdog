# Cross-language edge governance (`depdog.work.yaml`)

The [polyglot fan-out](monorepo.md) checks every unit of a monorepo *in
isolation*. This guide covers the layer above it: a single **`depdog.work.yaml`**
at the repo root that governs the dependency edges **between** units —

> "the `web/` TS app may depend on the shared TS lib but not reach its
> `internal/**` paths; no Go service may depend on another; the shared lib
> depends on nobody."

No other architecture linter — go-arch-lint, deptrac, dependency-cruiser,
import-linter, ArchUnit — governs edges *across* languages. depdog can because
its core is language-neutral: a **unit** is just a coarse component in a
super-graph whose nodes are units, judged by the same allow/deny + boundary
engine that judges packages inside one unit.

## The model: units, not symbols

Within one language an edge is import-path → import-path. Across languages
there is no shared symbol space — a TS `import` string never names a Go
package. So cross-language rules are written between **unit identities** ("may
`web` depend on `api`?"), and edges are **detected structurally** from each
unit's own scanned import graph:

- **Path channel** — an import that *resolves on disk* into another unit's
  tree: a TS/JS relative or `tsconfig`-alias import crossing subtrees, a Python
  relative import, and so on. Ownership is by unit `path` prefix,
  most-specific-wins for nested units.
- **Identity channel** — an `external`-classified import whose path matches
  another unit's **import identity** by segment prefix. Identities are read
  from each unit's marker files automatically: the `go.mod` module path, the
  `package.json` name, the `Cargo.toml` package name, the `pyproject.toml`
  project name. This is what detects `api` (Go) importing
  `example.com/billing/…` when `billing`'s `go.mod` says
  `module example.com/billing`, or `web` importing `@acme/shared` in a
  JS workspace.

A call that leaves no import — a plain HTTP request — is invisible to static
analysis; declare the intended topology in `rules:` anyway and the file is
still the reviewed, versioned statement of your architecture (violations
appear the moment someone adds a generated client or a direct import).

## The file

`depdog.work.yaml` sits at the repo root and is picked up automatically: at
that root, plain `depdog check` (or `--all`, same result) runs the fan-out
*plus* the cross pass. It is versioned independently of `depdog.yaml`
(`version: 1`), with a [JSON Schema](../schema/depdog.work.schema.json) for
editor autocomplete.

```yaml
version: 1

units:                          # name → root-relative directory (not a glob)
  web:     { path: web, lang: ts }   # lang optional — auto-detected from markers
  shared:  shared                    # shorthand: bare dir
  api:     { path: services/api }
  billing: { path: services/billing }

default: allow                  # unit with no rule → may depend on anything

rules:                          # the same allow/deny vocabulary, units as members
  web:    { allow: [shared] }   # whitelist: web may depend only on shared
  shared: { deny: ["*"] }       # a library depends on nobody

boundaries:                     # mutual exclusion, units as members
  services: [api, billing]      # no service may depend on another
  # sealed variant: services: { members: [api, billing], sealed: true }

surfaces:                       # which sub-paths of a unit others may reach
  shared:
    exports: ["src/**"]         # whitelist of reachable sub-paths …
    internal: ["internal/**"]   # … and paths denied even on an allowed edge
```

Semantics, all inherited from the component engine:

- **deny wins** over allow; a unit with an `allow` list is a whitelist, with a
  `deny`-only rule a blacklist, otherwise `default` applies (`allow` unless you
  set `deny`).
- **Boundaries** are symmetric mutual exclusion; `sealed: true` adds the
  one-way wall (no outside unit may depend inward).
- **Surfaces** run only on edges the unit rules allow (one violation per
  edge): a target sub-path matching `internal` is denied; with a non-empty
  `exports` list, any other non-empty sub-path is denied too. The unit's
  public root — a bare `@acme/shared` — always passes. Globs are
  unit-relative.
- **Unit cycles** are reported as an advisory (never fatal), like component
  cycles.

## Units and their own configs

The work file and per-unit `depdog.yaml`s **compose**: every discovered
`depdog.yaml` is still checked with its own rules and adapter, and the cross
pass runs on top. A declared unit **without** its own `depdog.yaml` is still
scanned (adapter auto-detected from its markers, or pinned with `lang:`) so
its outgoing edges are governed — it is listed with the skipped units as
"scanned for cross-unit governance only". That makes the work file a
low-friction first step: govern the topology today, add per-unit rules
per team later.

Nested units resolve most-specific-wins: a package under `services/api` counts
as `api` even when an outer unit's dir contains it, and its edges are judged
once, by the innermost unit's scan.

## Report

One report, one exit code (max severity across intra-unit and cross-unit
violations), in every `--format`:

```
▸ cross-unit (depdog.work.yaml, 4 units governed)

✗ denied by boundary "services"  (1 violation)
    api
      → billing  services/api/main.go:8

✗ shared: internal [internal/**]  (1 violation)
    web
      → shared/internal  web/src/app.ts:4
```

Positions are repo-root-relative on every surface, so GitHub annotations and
SARIF results land on the right file. `--format json` adds an additive
`cross_unit` block to the aggregate envelope — declared units (with their
detected identities), bespoke violations with machine-readable `reason` kinds
(`cross-unit`, `cross-unit-boundary`, `cross-unit-boundary-sealed`,
`cross-unit-surface`) and plain-English explanations, boundaries, unit cycles
and stats. The MCP `check` tool (`{all: true}`) returns the same payload.

`--unit <dir>` narrows the run to intra-unit checking and **skips the cross
pass** — cross-unit edges only mean something over the complete unit set.
An explicit `--config` also bypasses work mode entirely.

## Limitations (v1)

- Only units declared in `units:` participate in cross-unit governance;
  discovered-but-undeclared units are intra-checked as usual.
- The baseline/ratchet (`--fail-on new`) does not yet cover cross-unit
  violations — the work file is opt-in, so start it clean.
- `explain`, the LSP and the TUI stay per-unit surfaces for now.
- Generated-artifact provenance (a codegen client dir counting as an edge to
  its source-of-truth unit) is future work.
