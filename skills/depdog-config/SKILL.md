---
name: depdog-config
description: >-
  Analyze a codebase and author (or repair) a depdog.yaml — the config for the
  depdog architecture linter. Use when the user wants to adopt depdog, set up or
  tighten import/architecture rules, define components and boundaries, or when
  `depdog check` reports config errors. Works for Go, TypeScript/JS, Python,
  Rust, Java, Ruby, and Kotlin projects.
---

# Authoring a depdog.yaml

[depdog](https://github.com/matterpale/depdog) checks a codebase's import edges
against architecture rules declared in a `depdog.yaml` at the repo root. Your job
with this skill: understand the project's structure, translate its intended
architecture into a `depdog.yaml`, and validate it until `depdog check` passes
(or a baseline is in place). Produce a real, working config — not a sketch.

## 0. Make sure depdog is runnable

Prefer an installed binary; otherwise run it from source (no clone needed):

```bash
depdog --version 2>/dev/null || alias depdog='go run github.com/matterpale/depdog/cmd/depdog@latest'
```

`depdog` is a single Go binary; it needs **no** language toolchain for the target
project (it scans source statically). It auto-detects the language from marker
files (`go.mod`→go, `tsconfig.json`/`package.json`→ts, `pyproject.toml`→py,
`Cargo.toml`→rs, `pom.xml`/`build.gradle`→java, `Gemfile`→rb,
`build.gradle.kts`→kt); pass `--lang <id>` to force one.

## 1. Understand the codebase before writing rules

Do the analysis first — a good config mirrors the *intended* layering, which you
infer from the tree and the code, not from guesses.

1. **Map the source layout.** List the top-level source directories and one level
   below. Identify the unit depdog uses as a node: a **package/module directory**
   (Go package dir, TS folder, Python package, Java/Kotlin `src/main/<lang>/…`
   package dir, Rust `src` module dir, Ruby dir).
2. **Name the components (architectural roles), not folders one-to-one.** Look
   for layers/roles: `domain`/`core`, `service`/`usecase`, `handler`/`api`/`http`,
   `repository`/`store`/`db`, `cmd`/`main`, shared `internal`/`pkg`/`util`. Read a
   few files to confirm what depends on what.
3. **State the rules in words first**, e.g. "domain imports only the stdlib",
   "handlers may call services but not repositories directly", "no `cmd/` service
   imports another". You'll encode exactly these.
4. **Seed a draft (optional).** `depdog init --yes` scans the project and writes a
   starter `depdog.yaml` (one component per discovered top-level dir). Treat it as
   a starting point to refine, not the answer. Use `depdog graph --format mermaid`
   to see the real dependency structure while deciding.

## 2. Write the depdog.yaml

Root-level `depdog.yaml`. Format (`version: 2`):

```yaml
version: 2

# Each component: a path glob (or list) + who it may import, inline.
# Stance is inferred: an `allow` list is a whitelist (only these pass);
# a `deny`-only rule is a blacklist (everything except these); no rule falls
# back to the top-level `default`.
components:
  domain:     { path: "internal/domain/**",     allow: [std] }          # whitelist: stdlib only
  service:    { path: "internal/service/**",     allow: [domain, std] }
  handler:    { path: "internal/handler/**",     deny: [repository] }    # blacklist: anything but repo
  repository: { path: "internal/repository/**",  allow: [domain, std, external] }
  main:       { path: "cmd/**" }                                         # no rule ⇒ uses `default`

default: allow        # fallback for rule-less components. Use `deny` to fail closed.

# Optional: named reusable component sets, expanded in allow/deny.
groups:
  inner: [domain, service]

# Optional: orthogonal mutual-exclusion. Members (component names OR path globs)
# may not import across each other — great for "services don't import each other"
# without O(n²) deny lists. `sealed: true` also forbids anything outside from
# importing IN.
boundaries:
  cmd-services:
    members: ["cmd/**"]      # or a list of component names
    sealed: true

options:
  test_files: hybrid                 # default; also same-rules | relaxed
  skip: ["internal/legacy/**"]       # dirs excluded from analysis
```

**What an `allow`/`deny` entry can be:**

| Entry | Matches |
| --- | --- |
| a component name (`domain`) | that component |
| `std` | the language's standard library / builtins |
| `external` | any third-party dependency (node_modules, gems, crates, Maven deps, …) |
| `unassigned` | in-project packages no component claims |
| `"*"` | everything |
| a group name | its members (expanded at parse time) |
| an import path with `/` or `.` (`golang.org/x/sync`, `lodash`) | one external dependency, by prefix |

**Rules that matter:**
- **Most-specific-wins** when component globs overlap; equal-specificity overlap
  is a config *error* (depdog tells you which).
- **`deny` beats `allow`**; a boundary crossing is a hard deny.
- **Path globs are language-neutral** but must match the language's on-disk
  layout. For Java/Kotlin that means package dirs under `src/main/<lang>/…` (e.g.
  `path: "src/main/java/**/domain/**"`); for Rust, `src` module dirs (e.g.
  `src/domain/**`); for Python/Ruby/TS/Go, the package/module dirs directly.
  Run `depdog config` to see the exact node paths depdog assigns.

## 3. Validate and iterate — this is the important part

```bash
depdog check            # 0 clean · 1 violations · 2 config/usage error
depdog config           # the compiled rules (confirm components matched what you meant)
depdog explain <from> <to>   # why a specific edge is allowed/denied, which rule fired
```

- A **config/usage error (exit 2)** means the YAML is wrong (bad glob, unknown
  ref, overlapping components, empty component). Read the message — it names the
  fix — and correct it.
- **Violations (exit 1)** mean either the rules are right and the code is wrong,
  or the rules are too strict. Use `depdog explain` and the actual code to decide
  which. Tighten or loosen rules to match the *intended* architecture; don't just
  silence real problems.
- Iterate until `depdog check` reflects the architecture the user wants.

**Adopting on a codebase that doesn't pass yet:** don't relax rules to force a
pass. Baseline today's violations and fail only on new ones:

```bash
depdog baseline                 # writes depdog.baseline.yaml
depdog check --fail-on new      # exits 1 only on NEW violations
```

## 4. Best practices

- **Start permissive, tighten incrementally.** Begin with `default: allow` and
  add `allow`/`deny` to the components you actually care about; flip to
  `default: deny` only when the map is complete.
- **Name components by role, keep the set small.** A handful of meaningful layers
  beats a component per folder.
- **Reach for `boundaries` for cross-cutting isolation** ("no service imports
  another", "layers within a service don't cross") instead of hand-written O(n²)
  `deny` lists.
- **Wire it into CI.** `depdog check` is exit-code-driven; add it next to
  tests/lint. `--format github` gives inline PR annotations, `--format sarif`
  feeds code scanning.

## Output

Leave the repo with a committed `depdog.yaml` that passes `depdog check` (or a
baseline), and give the user a short plain-English summary of the components and
rules you encoded and any violations you baselined. Point them at
`depdog tui` to explore the result interactively.
