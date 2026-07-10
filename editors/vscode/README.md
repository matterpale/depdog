# depdog for VS Code (unpublished scaffold)

A thin VS Code extension that runs [`depdog lsp`](../../docs/lsp.md) — depdog's
LSP server over stdio — so architecture rule violations show up as inline
diagnostics on the exact import lines, with `depdog explain` verdicts on
hover. It activates for the seven adapter languages (Go, TypeScript/JS incl.
TSX/JSX, Python, Rust, Java, Ruby, Kotlin) or when the workspace contains a
`depdog.yaml`.

**Not on the Marketplace.** Publishing needs the owner's publisher account
(the `publisher` field is a placeholder), so this scaffold is built and
installed locally:

## Build and install

Prerequisites: `node`/`npm`, and the `depdog` binary on your `PATH`
(`brew install --cask matterpale/tap/depdog` or
`go install github.com/matterpale/depdog/cmd/depdog@latest`).

```bash
cd editors/vscode
npm install
npx @vscode/vsce package          # emits depdog-<version>.vsix
code --install-extension depdog-*.vsix
```

To remove it: `code --uninstall-extension depdog-placeholder.depdog`.

## How it works

`extension.js` (~40 lines) starts a `vscode-languageclient` `LanguageClient`
with `{command: "depdog", args: ["lsp"]}` and a document selector for the
seven languages. The server re-checks on every save; external `depdog.yaml`
edits are picked up both by the server's own dynamic
`workspace/didChangeWatchedFiles` registration and by this extension's
`synchronize.fileEvents` watcher (belt and braces). All diagnostics carry
source `depdog`.

There is no configuration surface yet; to pass flags (e.g.
`--config path/to/depdog.yaml`), edit `args` in `extension.js` and rebuild.
