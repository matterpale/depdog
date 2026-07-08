# Boundaries — orthogonal mutual-exclusion groups

Status: **shipped**. Concept and the five design decisions below were settled
with the owner on 2026-07-08 and implemented the same day: the `boundaries` key,
the core membership index and evaluate gate, the JSON schema, and the text /
`explain` / `config` / `graph` surfaces all ship together. This document is the
settled design; the engine now enforces it.

## Problem

depdog assigns each package to exactly one component (most-specific-wins), so a
component's rules can only express relationships along that single axis. Two real
needs don't fit that shape:

1. **Cross-cutting isolation.** "No `cmd/` service imports another" spans every
   service's whole subtree. Encoding it today means each service `deny`s every
   other — O(n²) entries — and it fights the per-layer components: a package
   under `cmd/query-ce/domain` is the component `query-ce-domain`, *not*
   `query-ce`, so it can't also carry a service-level rule.
2. **Peer mutual-exclusion boilerplate.** "repositories, services, handlers don't
   import each other" is three symmetric `deny` lists that must be kept in sync
   by hand, and repeated per service.

Both are the same shape: **a set of regions that may not import across each
other.** Today you can only approximate it with hand-written `deny` lists, and
only along the component axis.

## Model: a second, orthogonal axis

Add **boundaries**: named sets of *members* that may not import one another. A
boundary is orthogonal to component assignment — a package keeps its
most-specific component *and*, independently, belongs to every boundary member
whose region contains it. Component rules and boundary exclusions both apply to
each edge, and membership is many-valued (a package can sit in several boundaries
at once).

This is the key that dissolves the tension: `cmd/query-ce/services/x` is the
component `query-ce-services` (subject to layer rules) **and** a member of the
`cmd-services` boundary (subject to isolation), at the same time. No repartition,
no catch-all.

## Config surface

Shorthand (a list of members) is symmetric. The expanded form takes options:

```yaml
boundaries:
  # shorthand — peers within a service, replaces three symmetric deny lists
  query-ce-layers: [query-ce-repositories, query-ce-services, query-ce-handlers]

  # members can be path globs, spanning many components — cross-service isolation
  cmd-services:
    - "cmd/query-ce/**"
    - "cmd/router/**"
    - "cmd/engine-monitor/**"

  # expanded form — a sealed boundary: nothing OUTSIDE may import in
  services:
    sealed: true
    members: ["cmd/**/services/**", "cmd/**/repositories/**"]
```

A **member** is either a **component name** or a **path glob**. They are told
apart by the same heuristic as allow/deny refs: a member containing `/` (or glob
metacharacters) is a path glob; a bare identifier is a component name. Component
and glob members may mix in one boundary.

## Semantics

Every boundary enforces **symmetric exclusion between distinct members**:

| edge | verdict |
|---|---|
| member A → member B (A ≠ B) | **denied** |
| within one member (incl. same package) | allowed |
| member → ungrouped (e.g. a shared lib) | allowed |
| ungrouped → member | allowed (see `sealed`) |

- **Ungrouped is neutral.** A package in no member of a boundary is invisible to
  it, so shared libraries (`internal/*`, `pkg/*`) stay importable by every
  service.
- **Composable.** Boundaries are independent. Each edge is checked against every
  boundary it is subject to, plus the component rules; all must pass.
- **A boundary crossing is a hard deny** — it behaves like an implicit
  cross-member `deny` and wins over any component `allow`, consistent with
  today's deny-wins rule. `explain` reports `denied by boundary "cmd-services"`.
- **Members disjoint within a boundary.** A package matching two members of the
  *same* boundary is ambiguous → config error. Glob members resolve
  most-specific-wins; equal-specificity overlap is an error. (Overlap *across*
  different boundaries is expected — that's the composability.)

### Sealed boundaries (settled: include a one-way flag)

`sealed: true` **adds** one rule to the symmetric base: **no package outside all
members may import into any member.** The wall is one-way — a member may still
import outward (a service importing a shared lib is fine), but nothing outside may
reach in.

| edge | symmetric | `sealed: true` adds |
|---|---|---|
| ungrouped → member | allowed | **denied** |
| member → ungrouped | allowed | allowed (unchanged) |

- On a **single-member** boundary (no symmetric rule to apply) `sealed` means
  simply "nothing outside this region may import into it."
- "Outside" = not under any member of *this* boundary. So with a sealed
  `cmd-services`, `internal/foo → cmd/query-ce/...` is denied (a shared lib must
  not depend on a service), and `cmd/comparator → cmd/query-ce/...` is denied (a
  non-member importing in). `explain` reports `denied by boundary "cmd-services"
  (sealed)`.
- Caveat to document: a cross-service *composition root* that legitimately wires
  services together would sit outside and be blocked; in a repo where each
  service has its own `cmd/<svc>/main`, that root is a member and intra-service
  wiring is fine.

### Test files (settled: respect `test_files`)

Boundary crossings honour the top-level `test_files` mode exactly as component
rules do: under `hybrid`/`relaxed` a crossing that appears only in `_test.go` is
relaxed accordingly; `same-rules` enforces it. One consistent rule across the
whole config.

### Membership vs assignment (settled: orthogonal)

Boundary membership never assigns a package to a component. A package matched only
by a boundary glob (and no component) is **still** reported as "not covered by any
component", and the boundary's rules still apply to it. The two axes stay cleanly
separate.

## Distinct from `groups`

Today's `groups` are a reusable *set expanded inside `allow`/`deny` lists* — a
naming shorthand, not a rule. Boundaries are a *rule* (mutual exclusion) plus a
*membership axis*. They are complementary; `groups` stays exactly as-is. Do **not**
redefine `groups` to be mutually exclusive — that would break the documented
`allow: [inner]` expansion.

## Engine changes (this is not config sugar)

As shipped:

- **`internal/core`:** a `Boundary` type (name, members, `sealed`) and a
  per-package boundary-membership index built alongside component assignment.
  `Evaluate` runs, per edge, for each boundary the edge touches: cross-member →
  violation; and if the boundary is `sealed`, ungrouped-source → in-member-target
  → violation. A violation reason kind (`boundary`, distinguishing sealed) rides
  each verdict, and `DecideBoundary` is the single decision path shared by
  `Evaluate` and `explain`. Boundaries stay off the component-cycle SCC —
  they're a separate axis.
- **`internal/config`:** the top-level `boundaries` key; shorthand list and
  expanded `{ members, sealed }` forms; member = component-name | glob (same
  `/`-heuristic as refs); validation with actionable errors (unknown component
  member, overlapping members within a boundary, empty boundary, glob matching no
  package).
- **`internal/report`:** `explain`, text, JSON, `config`, and `graph` show
  boundary membership and boundary-sourced verdicts (including `(sealed)`). The
  JSON schema carries a stable `boundaries` array and a `boundary` field on
  boundary violations.
- **`schema/depdog.schema.json`:** `boundaries` is declared (kept in lockstep by
  the reflection test).
- **`internal/wizard`:** still out of scope (boundaries are hand-authored for
  now); a future cut could propose a `cmd-services`-style boundary from the
  directory scan.

No changes to the loader or the `internal/lang` adapter seam.

## Backward compatibility

Purely additive. Absent `boundaries`, behaviour is unchanged. `groups`,
components, `default`, options — all untouched. No config-version bump: it is a
new optional key under `version: 2`.

## What it buys a real config (calc-engine)

- Replace every peer `deny` list (query-ce / engine-monitor / metrics-collector /
  router) with one `boundaries` entry per service's layers.
- Add a single `cmd-services` boundary — one glob member per service — for
  cross-service isolation, the thing that is currently impossible without O(n²)
  deny lists. Make it `sealed` to also forbid shared libs and other services from
  importing in.
- Drop the `rest: "**"` catch-all that existed only to hang isolation on, which
  also removes the advisory component cycle it introduced.

## Decisions (settled 2026-07-08)

1. **Key/concept name:** `boundaries`.
2. **Member type:** component name **or** path glob.
3. **Direction:** symmetric by default, with an opt-in `sealed: true` one-way flag
   (nothing outside may import in).
4. **Test files:** boundary crossings respect the `test_files` mode.
5. **Membership vs assignment:** orthogonal — membership does not silence
   `unassigned` warnings.

Remaining, implementation-level (not blocking the design): exact
`explain`/JSON wording for sealed verdicts; whether `config` prints a per-package
boundary column or a per-boundary member list.

## Testing

- Unit: membership index (glob + component members; disjointness errors);
  `Evaluate` matrix — in-member allowed, cross-member denied, member→ungrouped
  allowed, ungrouped→member (sealed) denied, multi-boundary composition,
  test-file relaxation under each `test_files` mode.
- Fixture module: a two-service tree over shared libs; assert the cross-service
  edge is a violation, the shared-lib→service edge is a violation under `sealed`,
  and service→shared-lib passes.
- Golden e2e: `check`, `explain` (boundary + sealed verdict), `config`
  (membership), and the JSON schema.
- Dogfood: consider a boundary in depdog's own config once a second language
  adapter exists (`internal/lang/golang` isolated from a future adapter).

## Effort

M–L. Contained but real: core + evaluate, five report formats, config parser,
schema, docs. The `sealed` flag adds one edge-direction check and a reason
variant, not a new subsystem. No loader or adapter-seam changes.
