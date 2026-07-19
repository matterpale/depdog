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

**Groups** name a reusable set of components: declare `groups: { inner:
[domain, core] }`, then reference `inner` in any allow/deny list; it expands
to its members when the config loads.

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
