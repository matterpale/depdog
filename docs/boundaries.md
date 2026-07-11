# Boundaries — mutual exclusion on an orthogonal axis

[Components](configuration.md) answer "who may this layer import?" along one
axis. **Boundaries** add a second, orthogonal axis: named sets of *members*
that may not import across each other. A package keeps its most-specific
component **and**, independently, belongs to every boundary whose region
contains it — so `cmd/service-a/services/x` can be the `service-a-services`
component (subject to layer rules) and a member of the `cmd-services` boundary
(subject to isolation) at once. That dissolves two kinds of boilerplate: peer
`deny` lists ("layers don't import each other") and cross-cutting isolation
("no service imports another"), which otherwise needs O(n²) deny lists.

```yaml
boundaries:
  # shorthand — a symmetric peer set; these three may not import each other
  service-a-layers: [ service-a-repositories, service-a-services, service-a-handlers ]

  # expanded form — members can be path globs, and sealed adds a one-way wall
  cmd-services:
    members: [ "cmd/service-a/**", "cmd/service-b/**" ]
    sealed: true
```

A **member** is a component name *or* a path glob (told apart by the same `/`-or-
metacharacter heuristic as allow/deny refs); the two may mix in one boundary.

| edge                                   | verdict                                                    |
|----------------------------------------|------------------------------------------------------------|
| member A → member B (A ≠ B)            | **denied** (a hard deny — wins over any component `allow`) |
| within one member (incl. same package) | allowed                                                    |
| member → ungrouped (e.g. a shared lib) | allowed                                                    |
| ungrouped → member                     | allowed — **denied** when the boundary is `sealed`         |

`sealed: true` adds one rule: nothing outside all members may import *into* a
member. The wall is one-way, so a service may still import a shared lib, but a
shared lib (or another service) must not reach in. Boundaries are **composable**
(each edge is checked against every boundary plus the component rules) and
**orthogonal to assignment** — membership never silences the `unassigned`
warning for a package no component claims. `explain` reports a crossing as
`denied by boundary "cmd-services"`, with `(sealed)` for the one-way rule.
