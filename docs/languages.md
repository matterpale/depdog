# Language selection — auto-detection and the `--lang` override

depdog checks many languages through a pluggable adapter registry; the
[README](../README.md#multi-language-support) lists them and the marker files
each is detected by. Most are hand-written; some (e.g. C#) are **declarative**
adapters defined by a YAML spec, and you can add your own — see
[adapters.md](adapters.md). This page covers how depdog *chooses* an adapter for
a run, which is identical for hand-written and declarative adapters.

## Auto-detection

depdog picks the adapter from the project's marker files, walking up from the
working directory; the marker nearest the working directory wins in a nested
layout (e.g. a `web/` TS app inside a Go repo).

## Explicit override

The persistent `--lang` flag (available to every subcommand) bypasses detection:

```bash
depdog check --lang py        # force the Python adapter
depdog graph --lang rs        # force the Rust adapter
```

## Ambiguity

A directory that carries markers for **two** languages with no `--lang` is
genuinely ambiguous: depdog exits with a usage error naming `--lang` rather than
silently guessing.

## The `lang:` config key

For a project whose markers are ambiguous, the optional top-level `lang:` key in
its `depdog.yaml` pins the adapter without passing `--lang` on every invocation:

```yaml
version: 2
lang: kt        # a JVM project rooted only on build.gradle: pin Kotlin
components:
  # …
```

Resolution order is: an explicit `--lang` flag, else the config's `lang:` key,
else auto-detection. `lang:` takes any adapter id from the table above.

## Monorepos and `--all`

`--lang` and `lang:` select the adapter for a **single** project. A polyglot
monorepo has many projects in one tree, each its own language — so
`depdog check --all` discovers every `depdog.yaml` under the cwd and lets **each
unit** auto-detect its own adapter (or pin one with its `lang:` key). A global
`--lang` would force one language on every unit, which never makes sense under
`--all`, so depdog rejects the combination. See
[docs/monorepo.md](monorepo.md).
