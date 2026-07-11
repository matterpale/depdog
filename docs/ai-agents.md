# For AI agents — driving depdog from tools

depdog is built to be driven by tools and agents, not just humans:

- **Machine-readable output.** `depdog check --format json` emits a stable
  schema (violations, components, boundaries, stats); the
  [exit codes](../README.md#commands) are a contract (`0` clean, `1`
  violations, `2` config/usage error). `depdog config` prints the compiled
  rule set so an agent can inspect what a config actually means before changing
  it.
- **Polyglot-aware.** Auto-detect or `--lang` means an agent doesn't need to
  know the language up front; a monorepo can be checked per subtree. (See
  [language selection](languages.md).)
- **A playbook for authoring `depdog.yaml`.**
  [`skills/depdog-config/SKILL.md`](../skills/depdog-config/SKILL.md) is a
  self-contained, tool-agnostic guide any coding agent can follow: it maps a
  codebase's layout to components and import rules, writes a `depdog.yaml`, and
  iterates with `depdog check`/`explain`/`config` until it matches the intended
  architecture (the full config-format reference is inline). Point your agent at
  that file directly, or drop the folder into wherever your agent discovers
  reusable skills or instructions — it's a standard skill directory (`SKILL.md`
  with front-matter), so it works as a skill for editor agents, as an
  `AGENTS.md` reference, or as plain context you paste in.
- **Editor schema.** [`schema/depdog.schema.json`](../schema/depdog.schema.json)
  gives autocomplete and validation in any JSON-schema-aware editor or agent.
