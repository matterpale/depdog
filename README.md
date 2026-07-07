# depdog

Keep a Go codebase's internal dependencies pointing in the right direction.

Architecture rules — "the domain imports nothing but the standard library",
"handlers never import repositories" — usually live in people's heads or a wiki,
and they rot. depdog makes them executable: you declare which *components* exist
and who may import whom in a small `depdog.yaml`, and `depdog check` enforces it
against every import edge in the module, with a non-zero exit code for CI.

```
depdog check — github.com/acme/shop

✗ domain: allow [std]  (2 violations)
    github.com/acme/shop/internal/domain/order
      → github.com/acme/shop/internal/repository   internal/domain/order/order.go:7
      → github.com/google/uuid                     internal/domain/order/order.go:9

1 violation · 1 warning · 12 packages · 39 edges checked in 40ms
```

![depdog demo: check, explain, and the TUI on a module with violations](docs/demo.gif)

## Install

```bash
# From source (Go 1.26+):
go install github.com/matterpale/depdog/cmd/depdog@latest

# Or build the repo directly:
go build -o depdog ./cmd/depdog
```

Prebuilt binaries for Linux, macOS, and Windows are on the
[releases page](https://github.com/matterpale/depdog/releases).

## Quick start

```bash
depdog init      # scan the module and write a starter depdog.yaml
depdog check     # enforce the rules; exit 1 on violations
```

`init` inspects your layout, matches it against an architecture preset, and
proposes a component mapping you refine interactively — drop, rename, or
re-pattern components — or accept as-is with `--yes`.

When a `depdog.yaml` already exists, `init` refuses to touch it. As the code
grows, `depdog init --merge` rescans the module and appends a component (and,
under `policy: deny`, a starter rule) for every directory no existing pattern
covers — editing the file in place without disturbing your comments, ordering
or formatting. When everything is covered it changes nothing and says so.

## Configuration

`depdog.yaml` lives at the repo root, next to `go.mod`:

```yaml
version: 1

components:
  main:       ["cmd/**"]
  domain:     ["internal/domain/**"]
  handler:    ["internal/handler/**"]
  service:    ["internal/service/**"]
  repository: ["internal/repository/**"]

policy: deny          # whitelist stance — only what a rule allows may be imported

rules:
  main:       { allow: ["*"] }                 # the entrypoint wires everything
  domain:     { allow: [std] }                 # the pure core: std-lib only
  handler:    { allow: [domain, std, external] }
  service:    { allow: [domain, std, external] }
  repository: { allow: [domain, std, external] }

options:
  test_files: hybrid              # default; also: same-rules, relaxed
  skip: ["internal/legacy/**"]    # package dirs excluded from analysis
```

An editor JSON Schema ships at
[`schema/depdog.schema.json`](schema/depdog.schema.json) for autocomplete and
validation (a test keeps it in lockstep with the parser).

Key ideas:

- **Components** are named sets of packages, matched by recursive doublestar
  globs against module-relative package dirs. When patterns overlap, the most
  specific wins; equal specificity is an ambiguity error.
- **Stance is inferred per rule from word choice.** A rule with an `allow` list
  is a *whitelist* (only the listed imports pass); a rule with only a `deny` list
  is a *blacklist* (everything passes except what's listed). An explicit `deny`
  always beats an `allow`. This lets stances mix per component — `handler:
  { deny: [repository] }` means "anything but repository" even when other
  components are strict whitelists.
- **policy** is the fallback for components with no `allow`/`deny` rule: `deny`
  (whitelist) or `allow` (blacklist). It is optional — omit it for the strict
  `deny` default. `init` asks which you want.
- Allow/deny entries are component names or the specials `std`, `external`,
  `unassigned` and `"*"`. An entry that looks like an import path (contains `/`
  or `.`) restricts a **specific external module** by prefix, e.g. `allow: [std,
  "golang.org/x/sync"]` permits std and that one dependency; list it under `deny`
  to forbid just that module.
- **Groups** name a reusable set of components. Declare `groups: { inner:
  [domain, core] }`, then reference `inner` in any allow/deny list; it expands
  to its members when the config loads.
- In-module packages no component claims are always reported as **warnings**,
  but never fail the build on their own — unmapped packages are how rule sets
  rot, so they stay visible without blocking adoption. A component whose
  patterns match no package is likewise flagged (a likely typo or dead pattern).
- **Component import cycles** (`a ↔ b` at the architecture level — which a
  package-level compile check can't even have) are detected and reported as an
  advisory, never failing the build on their own.
- **test_files: hybrid** (the default) lets `_test.go` files import any external
  module while still enforcing component-to-component rules; `same-rules` is
  strict, `relaxed` exempts test files entirely.

## Commands

| Command | What it does |
|---|---|
| `depdog init` | Scan the module and write a starter `depdog.yaml`. `--preset ddd\|hexagonal\|layered\|flat`, `--policy deny\|allow`, `--yes` (non-interactive), `--force` (overwrite), `--merge` (add components for uncovered directories to an existing file, preserving its comments and formatting). |
| `depdog check [packages]` | Evaluate imports against the rules. `--format text\|json\|github\|sarif`, `--fail-on any\|new`, `--color auto\|always\|never`. |
| `depdog baseline` | Record current violations to `depdog.baseline.yaml` for the ratchet below. |
| `depdog graph` | Emit the dependency graph. `--format dot\|mermaid`, `--level component\|package`, `--violations-only`, `--focus <component>`. |
| `depdog config` | Print the compiled rule set — components, patterns, each component's inferred stance and rule, the policy and options — for debugging a config. |
| `depdog explain <component-or-package> [import]` | Explain why something is red (the rule that fired, with file:line), how a component is constrained, or — with a second argument — whether one package may import another (a package, component, or external module) and which rule decides it. |
| `depdog tui` / bare `depdog` | Interactive terminal UI: a component dashboard, a browsable violations list, and per-package imports/importers. The Violations and Packages lists scroll and filter with `/`; `e` opens the selection in `$EDITOR` at its file:line; `r` re-runs the check in place; `?` shows all keys. |

Exit codes are a contract: **0** clean, **1** violations, **2** configuration or
usage error.

## CI

`depdog check` is CI-ready as-is. For inline pull-request annotations use the
GitHub format; for GitHub code scanning, emit SARIF:

```yaml
- run: go run github.com/matterpale/depdog/cmd/depdog check --format github

# or, for the code-scanning tab:
- run: go run github.com/matterpale/depdog/cmd/depdog check --format sarif > depdog.sarif
- uses: github/codeql-action/upload-sarif@v3
  with: { sarif_file: depdog.sarif }
```

### Adopting rules on a codebase that doesn't pass yet

Record today's violations as a baseline, then fail only on new ones — and shrink
the baseline over time:

```bash
depdog baseline                 # writes depdog.baseline.yaml
depdog check --fail-on new      # exits 1 only on violations not in the baseline
```

## depdog checks itself

depdog's own architecture is declared in its `depdog.yaml` and enforced in CI:
the language-agnostic engine (`internal/core`) depends on the standard library
only, language knowledge lives behind an adapter interface, and the layers above
may only import inward. A failing architecture is a failing build.

## Limitations

- **One build configuration.** depdog loads packages for the host's
  `GOOS`/`GOARCH` and default build tags. Imports guarded by other build
  constraints (e.g. `//go:build windows` on a non-Windows machine) aren't seen.
- **Single module.** Go workspaces (`go.work`) aren't supported: depdog checks
  one module and declines to run inside a workspace with a clear message (set
  `GOWORK=off` to bypass a workspace and check the module directly).

## Status

v0.1.0 — the first tagged release. The M0–M5 roadmap in `PLAN.md` is complete;
`BACKLOG.md` tracks what's next.

## License

[MIT](LICENSE)
