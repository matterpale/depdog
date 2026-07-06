# depdog

A Go CLI + TUI tool that keeps a codebase's internal dependencies pointing in the right
direction: architecture/import rules are declared in `depdog.yaml` and enforced by
`depdog check`. `PLAN.md` is the single source of truth for design decisions and the
M0–M5 roadmap (M0+M1 done; next: M2 init wizard, M3 baseline/SARIF, M4 charmbracelet
TUI, M5 graph/explain + v0.1.0 release).

## Commands

```bash
go build ./...                  # build everything
go test ./...                   # unit + fixture + golden e2e tests
go test ./internal/e2e -update  # regenerate golden files after intended output changes
go run ./cmd/depdog check       # dogfood: depdog checks its own architecture
```

CI (`.github/workflows/ci.yml`) runs build, vet, test, golangci-lint, and the self-check.
All five must stay green; a depdog self-check violation is a build failure by design.

## Architecture (enforced by depdog.yaml)

- `internal/core` — language-agnostic engine (graph, rules, evaluate). **Std-lib only.**
- `internal/lang` — language-adapter interface; `internal/lang/golang` — the Go adapter.
- `internal/config` — depdog.yaml parsing/validation/discovery.
- `internal/report` — text + JSON renderers; JSON field names are a public, stable schema.
- `internal/cli` — cobra commands; `cmd/depdog` — fang-wrapped main.

Import direction is checked, not aspirational: run the self-check before committing.

## Conventions

- Trunk-based, solo: commit directly to `main`; milestone-gated manual releases.
- Determinism is a hard requirement — sort all engine output; golden tests depend on it.
- Errors are human-actionable and include the fix (see `internal/config` messages).
- Tests ship in the same change as the code (three tiers: unit, fixture-module, golden e2e).
- Exit codes: 0 clean, 1 violations, 2 config/usage error.
- Import order: std → external → `github.com/matterpale/depdog/internal/...`.

## Babysitter

This project is onboarded to [babysitter](https://github.com/a5c-ai/babysitter) for
orchestrated multi-step runs. Profile: `.a5c/project-profile.json` (owner preferences:
**autonomous** mode — only stop for destructive/irreversible operations; no CI/CD
integration — CI stays plain by owner choice).

### Commands

- `/babysitter:call <goal>` — orchestrate a complex workflow (e.g. a full milestone).
- `/babysitter:plan <goal>` — plan a run without executing it.
- `/babysitter:resume` — resume an interrupted run in `.a5c/runs/`.
- `babysitter process-library:active --json` — resolve the active process library.

### Recommended methodology

ATDD/TDD (`methodologies/atdd-tdd` in the process library), with domain-driven-design
as secondary — matches the repo's test-first, golden-gated, layered style.

### Recommended library processes (by milestone)

| Milestone | Process (under `specializations/cli-mcp-development/`) |
|---|---|
| M2 init wizard | `interactive-form-implementation.js`, `interactive-prompt-system.js` |
| M3 output/baseline | `cli-output-formatting.js`, `configuration-management-system.js` |
| M4 TUI | `tui-application-framework.js`, `dashboard-monitoring-tui.js` |
| M5 release | `cli-binary-distribution.js`, `cli-documentation-generation.js` |

Useful library skills: `bubble-tea-scaffolder`, `tui-test-renderer` (M4),
`goreleaser-setup`, `homebrew-formula-generator` (M5). Locally installed skills that fit
this repo's workflow: `tdd`, `code-review`, `verify`, `run`.

Full recommendations: `.a5c/runs/<latest project-install run>/artifacts/tool-recommendations.json`.
