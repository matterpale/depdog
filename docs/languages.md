# Language selection — auto-detection and the `--lang` override

depdog checks nine languages through a pluggable adapter registry; the
[README](../README.md#multi-language-support) lists them and the marker files
each is detected by. This page covers how depdog *chooses* an adapter for a run.

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
