# Configuration reference — the `depdog.yaml` vocabulary

The [README](../README.md#configuration) has the quick tour and an annotated
example; this page is the complete reference for every field. The orthogonal
[boundaries](boundaries.md) axis has its own page.

## Components and matching

A component is a named set of packages: each `path` glob is matched, recursive
doublestar style, against module-relative package directories. `path` takes a
single glob or a list (`path: ["internal/api/**", "internal/rpc/**"]`). When
patterns overlap, the most specific one wins; equal specificity is an ambiguity
error, not a silent pick.

## What goes in `allow` and `deny`

| Entry                  | Matches                                                             |
|------------------------|---------------------------------------------------------------------|
| `domain`, `handler`, … | another component, by name                                          |
| `std`                  | the language's standard library (each adapter fills the bucket)     |
| `external`             | any module that isn't yours                                         |
| `unassigned`           | in-module packages no component claims                              |
| `"*"`                  | everything                                                          |
| `golang.org/x/sync`    | one specific external module, by prefix — any entry with `/` or `.` |

**Aliases** name a reusable entry — or set of entries — for any allow/deny
list. An alias member is a component *or* an external-module prefix (or a list
mixing both), told apart the same way an inline entry is: a bare word is a
component, anything with a `/` or `.` is an external module. Declare them once
and reference the name wherever the expansion is needed:

```yaml
aliases:
  inner: [ domain, core ]                               # a set of components
  pgx:   github.com/jackc/pgx/v5                         # one prefix (bare scalar)
  aws:   [ github.com/aws/aws-sdk-go-v2, github.com/aws/smithy-go ]
components:
  api: { path: "internal/api/**", allow: [ inner, aws, std ] }
deny: [ pgx ]        # reuse the same alias by name, no duplicated blob
```

An alias with a single member may be written as a bare scalar (`pgx:
github.com/jackc/pgx/v5`); it expands to its members when the config loads. This
is the tool for a long external-module prefix — or a set of them — that you'd
otherwise paste into several rules.

> Renamed from `groups` (which only held components). `groups:` still works
> through the 1.x line, **frozen at its original behaviour — components only**; a
> config that uses it parses unchanged and prints a one-line deprecation notice.
> To alias an external-module prefix, use `aliases:` (that is the one thing
> `groups:` will not do — it errors with a pointer to `aliases:`). Setting both
> `groups:` and `aliases:` is an error. `groups` is removed in the next major.

A component's **stance** is read from which word its rule uses: one with an
`allow` list is a **whitelist** (only what's listed passes), one with only a
`deny` list a **blacklist** (`depdog config` prints the inferred stance per
component).

Two rules of precedence to remember: an explicit `deny` always beats an
`allow`, and a component with neither falls back to the top-level `default` —
under `default: allow` (the default) a rule-less component may import anything
(an explicit `allow: ["*"]` would be equivalent, just noisier); set
`default: deny` to make unruled components fail closed (`init` asks which
stance you want).

## Module-wide `deny` — a project-level ban

A **top-level** `deny` (a sibling of `components`, not nested inside one) bans a
dependency across the *entire* module, regardless of which component a package
belongs to:

```yaml
version: 2
deny: [ github.com/evil/pkg ]     # no package anywhere may import this
components:
  api: { path: "internal/api/**", allow: [ external, std ] }
  web: { path: "internal/web/**", allow: [ external, std ] }
```

It takes the same entries as a component list (an external-module prefix is the
common one; component and alias names work too) and is the right tool for a
security or license ban that must hold everywhere — `api` and `web` above may
import any *other* external module, but never `github.com/evil/pkg`.

Why not just a component `deny`, or a `path: "**"` catch-all component? Because
each package is owned by exactly **one** component (the most specific path
pattern wins), and a rule only governs its owner's packages. A `**` catch-all is
the *least* specific pattern, so it only owns the packages no real component
claims — it never sees the imports made from inside `api` or `web`. A component
`deny` works but must be repeated on every component that allows external and
re-added each time you add one. The top-level `deny` states the ban once and
applies it to all.

Precedence: a global `deny` **wins over any component `allow`**, applies even to
test-only imports (a banned dependency must not appear anywhere,
`options.test_files` notwithstanding), and covers packages no component claims.
An import *within* a package's own component is never treated as a dependency on
that component, so it is exempt. A global `deny` always fails the build (error
severity) and takes no `severity` key — unlike a component or boundary, there is
no `warn` global ban. When an edge both crosses a `boundary` and is globally
denied (only possible for a component ref, since boundaries are in-module), the
boundary is reported, matching what `explain` shows. `depdog check` reports a
global-deny violation under a `global deny [...]` heading, and
`depdog explain <pkg> <module>` names it as the deciding rule.

## Severity — warn vs error

A component or a boundary can carry a `severity`:

```yaml
components:
  legacy: { path: "internal/legacy/**", allow: [ std ], severity: warn }

boundaries:
  layers: { members: [ handler, service, repository ], severity: warn }
```

- `error` (the default when `severity` is omitted) — a violation fails the check
  (exit `1`).
- `warn` — the violation is still reported on every surface (text marks it
  `[warn]`, JSON carries `"severity": "warn"`, GitHub emits `::warning::`, SARIF
  uses `level: "warning"`, and the LSP shows it as a Warning), but it does **not**
  flip the exit code. A tree whose only violations are warnings exits `0`.

Severity is graded per **component** (it applies to every violation that
component emits — its `allow`/`deny` and its default-stance denials) and per
**boundary** (its crossings). It pairs with the baseline ratchet: mark a messy
component `warn` to surface its edges without blocking, while `check --fail-on
new` still fails on newly-introduced errors. Warnings are not written to
`depdog baseline` (they never fail, so there is nothing to ratchet).

## Signals that never fail the build

Three findings are always reported but never exit non-zero on their own —
visibility without blocking adoption:

- **Unmapped packages.** In-module packages no component claims are warnings;
  unmapped packages are how rule sets rot, so they stay visible.
- **Dead patterns.** A component whose patterns match no package is flagged —
  a likely typo.
- **Component cycles.** `a ↔ b` at the architecture level (which a
  package-level compile check can't even have) is detected and reported as an
  advisory.

## Test files

`test_files: hybrid` (the default) lets `_test.go` files import any external
module while still enforcing component-to-component rules; `same-rules` is
strict, `relaxed` exempts test files entirely.
