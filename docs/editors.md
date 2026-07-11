# Editor setup — wiring `depdog lsp` into your editor

`depdog lsp` speaks the Language Server Protocol over stdio (design of record:
[docs/lsp.md](lsp.md)). Any LSP-capable editor can point a generic client at
the command `depdog lsp` and get depdog's rule violations as inline
diagnostics on the exact import lines, plus `depdog explain` verdicts on
hover. This guide gives concrete snippets for Neovim, Helix, VS Code, Zed,
GoLand/JetBrains and Emacs, covering all seven adapter languages: **Go,
TypeScript/JavaScript (incl. TSX/JSX), Python, Rust, Java, Ruby, Kotlin**.

Things that hold for every editor:

- **Command** — `depdog lsp`. The binary must be on the editor's `PATH`
  (`brew install --cask matterpale/tap/depdog` or
  `go install github.com/matterpale/depdog/cmd/depdog@latest`).
- **Flags** — `depdog lsp --config path/to/depdog.yaml` pins the config
  explicitly (default: discovered next to the project marker); the global
  `--lang go|ts|py|rs|java|rb|kt` flag forces an adapter when auto-detection
  would be ambiguous. Append these to the args list in any snippet below.
- **Root** — the natural project root marker is `depdog.yaml`.
- **Re-check triggers** — the server re-checks on every save. Clients that
  support dynamic registration of `workspace/didChangeWatchedFiles` (VS Code,
  Neovim ≥ 0.10, Helix ≥ 23.10) also re-check when `depdog.yaml` changes
  *outside* the editor (git checkout, terminal edits, `depdog baseline`);
  clients without it simply re-check on the next save.
- **Diagnostics** — all carry source `depdog`; severity is always Error
  (matching `depdog check`'s exit code so the editor never disagrees with CI).

### How these snippets were validated

This machine has no `nvim`, `hx`, `zed` or `emacs`, so **editor-level behavior
is untested** unless a section says otherwise. What *was* verified
mechanically (throwaway script, 2026-07-10): the Helix snippet parses as TOML,
the Zed and VS Code snippets parse as JSON, and the Neovim snippets compile as
Lua (`luajit -bl`). The VS Code extension scaffold additionally **builds
end-to-end** on this machine (`npm install`, `node --check`,
`npx @vscode/vsce package` → `.vsix`). Each section carries its own marker.
If you run one of these editors and a snippet needs a fix, a PR correcting it
(and this marker) is very welcome.

---

## Neovim (≥ 0.10)

*Status: syntax validated (compiles as Lua via `luajit -bl`); editor behavior
untested — no Neovim on the authoring machine.*

Neovim ≥ 0.10 ships a built-in LSP client with dynamic file-watching
registration, so external `depdog.yaml` edits re-check automatically.

**Neovim 0.11+** — the declarative `vim.lsp.config()` / `vim.lsp.enable()`
API (put this in `init.lua`, or the table part in
`~/.config/nvim/lsp/depdog.lua`):

```lua
vim.lsp.config('depdog', {
  cmd = { 'depdog', 'lsp' },
  -- To pin a config file: cmd = { 'depdog', 'lsp', '--config', 'depdog.yaml' },
  filetypes = {
    'go',
    'typescript', 'javascript', 'typescriptreact', 'javascriptreact',
    'python',
    'rust',
    'java',
    'ruby',
    'kotlin',
  },
  root_markers = { 'depdog.yaml' },
})
vim.lsp.enable('depdog')
```

**Neovim 0.10** — `vim.lsp.config` does not exist yet; start the client from a
`FileType` autocmd instead (`vim.lsp.start` reuses one client per root, so
multiple buffers share the session):

```lua
vim.api.nvim_create_autocmd('FileType', {
  pattern = {
    'go',
    'typescript', 'javascript', 'typescriptreact', 'javascriptreact',
    'python',
    'rust',
    'java',
    'ruby',
    'kotlin',
  },
  callback = function(args)
    local root = vim.fs.root(args.buf, { 'depdog.yaml' })
    if root then
      vim.lsp.start({
        name = 'depdog',
        cmd = { 'depdog', 'lsp' },
        root_dir = root,
      })
    end
  end,
})
```

depdog runs *alongside* your language's main server (gopls,
typescript-language-server, …) — Neovim happily attaches several clients to
one buffer, and depdog's diagnostics are namespaced under source `depdog`.

---

## Helix (≥ 23.10)

*Status: syntax validated (parses as TOML via a scratchpad script); editor
behavior untested — no Helix on the authoring machine.*

Helix ≥ 23.10 supports multiple language servers per language **and** the
dynamic `didChangeWatchedFiles` registration depdog uses for config watching.
Add to `~/.config/helix/languages.toml` (or the per-project
`.helix/languages.toml`):

```toml
[language-server.depdog]
command = "depdog"
args = ["lsp"]
# To pin a config file: args = ["lsp", "--config", "depdog.yaml"]

# NOTE: a `language-servers` key REPLACES the default list for that language,
# so keep your usual server first. The primaries below are Helix's usual
# defaults — adjust to whatever `hx --health <lang>` says you use.

[[language]]
name = "go"
language-servers = ["gopls", "depdog"]

[[language]]
name = "typescript"
language-servers = ["typescript-language-server", "depdog"]

[[language]]
name = "tsx"
language-servers = ["typescript-language-server", "depdog"]

[[language]]
name = "javascript"
language-servers = ["typescript-language-server", "depdog"]

[[language]]
name = "jsx"
language-servers = ["typescript-language-server", "depdog"]

[[language]]
name = "python"
language-servers = ["pylsp", "depdog"]

[[language]]
name = "rust"
language-servers = ["rust-analyzer", "depdog"]

[[language]]
name = "java"
language-servers = ["jdtls", "depdog"]

[[language]]
name = "ruby"
language-servers = ["solargraph", "depdog"]

[[language]]
name = "kotlin"
language-servers = ["kotlin-language-server", "depdog"]
```

---

## VS Code

*Status: **validated** — the scaffold builds on the authoring machine
(`npm install` clean, `node --check extension.js` passes,
`npx @vscode/vsce package` emits a `.vsix`). Not published to the
Marketplace (publishing needs owner credentials) and installing the `.vsix`
is a manual, local step.*

VS Code has no generic "run this LSP command" setting, so depdog ships a thin
extension scaffold at [`editors/vscode/`](../editors/vscode/) — ~40 lines of
glue that starts `depdog lsp` for the seven adapter languages and watches
`depdog.yaml`. Build and install it locally:

```bash
cd editors/vscode
npm install
npx @vscode/vsce package        # emits depdog-<version>.vsix
code --install-extension depdog-*.vsix
```

See [`editors/vscode/README.md`](../editors/vscode/README.md) for details.
The extension activates on any of the seven languages or when the workspace
contains a `depdog.yaml`, and expects `depdog` on `PATH`.

---

## Zed

*Status: syntax validated (parses as JSON via a scratchpad script); editor
behavior untested — no Zed on the authoring machine. Caveat: Zed normally
sources language servers from extensions; if your Zed version does not accept
a settings-only server the way shown here, a dedicated Zed extension is
needed (owner-gated, like Marketplace publishing).*

In `~/.config/zed/settings.json` (or the per-project `.zed/settings.json`):
declare the server binary under `lsp` and enable it per language — the
`"..."` entry keeps each language's default servers running alongside depdog.

```json
{
  "lsp": {
    "depdog": {
      "binary": {
        "path": "depdog",
        "arguments": ["lsp"]
      }
    }
  },
  "languages": {
    "Go": { "language_servers": ["...", "depdog"] },
    "TypeScript": { "language_servers": ["...", "depdog"] },
    "TSX": { "language_servers": ["...", "depdog"] },
    "JavaScript": { "language_servers": ["...", "depdog"] },
    "Python": { "language_servers": ["...", "depdog"] },
    "Rust": { "language_servers": ["...", "depdog"] },
    "Java": { "language_servers": ["...", "depdog"] },
    "Ruby": { "language_servers": ["...", "depdog"] },
    "Kotlin": { "language_servers": ["...", "depdog"] }
  }
}
```

(Java, Ruby and Kotlin support in Zed come from Zed extensions; install those
first so the language entries exist.)

---

## GoLand / JetBrains IDEs (via LSP4IJ)

*Status: untested — no GoLand or LSP4IJ on the authoring machine. Unlike VS
Code, JetBrains IDEs have no built-in client for arbitrary user-defined LSP
servers, so this relies on the third-party **[LSP4IJ](https://plugins.jetbrains.com/plugin/23257-lsp4ij)**
plugin (Red Hat, free, JetBrains Marketplace). Configuration is done through
the Settings UI rather than a text file, so there's no snippet here to
syntax-validate.*

1. Install **LSP4IJ**: `Settings → Plugins → Marketplace`, search "LSP4IJ".
2. `Settings → Languages & Frameworks → Language Servers → +` → "New
   Language Server".
3. Configure the new entry:
   - **Command**: `depdog lsp` (append `--config path/to/depdog.yaml` to pin
     a config explicitly, same flag as every other editor in this guide).
   - **Mappings**: add a file type mapping for each adapter language you use
     — Go, TypeScript, JavaScript (incl. TSX/JSX), Python, Rust, Java, Ruby,
     Kotlin.
4. Apply. LSP4IJ starts `depdog lsp` alongside your existing language server
   (gopls, etc.) — same "runs alongside, namespaced under source `depdog`"
   behavior as Neovim, Helix and Zed above.

LSP4IJ's UI has changed across versions; if these steps don't match what you
see, check LSP4IJ's own docs. As with the other untested sections, a PR
correcting this after a real GoLand run is welcome.

---

## Emacs (eglot)

*Status: untested — no Emacs on the authoring machine, and elisp was not
machine-validated (no byte-compiler available).*

Eglot is built into Emacs 29+. **Caveat:** eglot manages exactly *one* server
per buffer, so the snippet below makes depdog *the* server for these modes,
shadowing gopls/pyright/rust-analyzer — you get architecture diagnostics but
lose completion and the rest. Use it selectively (e.g. only in projects where
you review architecture), or use `lsp-mode`, which supports add-on servers
(`lsp-register-client` with `:add-on? t`) running next to the primary one.

```elisp
(with-eval-after-load 'eglot
  (dolist (mode '(go-mode go-ts-mode
                  typescript-mode typescript-ts-mode tsx-ts-mode
                  js-mode js-ts-mode
                  python-mode python-ts-mode
                  rust-mode rust-ts-mode
                  java-mode java-ts-mode
                  ruby-mode ruby-ts-mode
                  kotlin-mode kotlin-ts-mode))
    (add-to-list 'eglot-server-programs
                 `(,mode . ("depdog" "lsp")))))
;; To pin a config file: `(,mode . ("depdog" "lsp" "--config" "depdog.yaml"))
```

Then `M-x eglot` in a buffer of one of those modes.

---

## Troubleshooting

- **No diagnostics?** Run `depdog check` in the same directory first — the
  LSP server publishes exactly what `check` prints. If `check` errors (no
  config, ambiguous language markers), the server logs the same error to
  stderr and publishes nothing; check your editor's LSP log.
- **`depdog.yaml` edits not picked up?** Saves inside the editor always
  trigger a re-check. External edits need client-side file watching
  (`workspace/didChangeWatchedFiles` dynamic registration) — supported by
  VS Code, Neovim ≥ 0.10 and Helix ≥ 23.10; on other clients, save any
  source file to force a re-check.
- **Logs** — the server writes to stderr only; stdout carries protocol
  frames. Most editors expose the stderr stream in their LSP log view
  (`:LspLog` / `hx --verbose` / VS Code's Output panel → depdog).
