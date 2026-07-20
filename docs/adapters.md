# Declarative language adapters — add a language with a spec

Most of depdog's languages are **hand-written** Go adapters (one package under
`internal/lang/<name>`). But a language whose import syntax and module resolution
are simple enough can be added **declaratively** — by writing a YAML *spec*
instead of Go. The spec drives a generic, comment/string-aware engine
(`internal/lang/spec`) that produces the exact same language-neutral graph a
hand-written adapter does, so `depdog check`, the reporters, and every config
feature work unchanged.

This is **additive**. The hand-written adapters are untouched, and a language
whose scanning or resolution can't be expressed faithfully in a spec simply stays
hand-written — the declarative path never lowers the correctness bar.

- **Built-in** declarative adapters ship embedded in depdog (today: **C#**, `cs`).
- **User** adapters live in your repo at `.depdog/adapters/<name>.yaml` — add a
  language to a repo without waiting for a depdog release.

Both register through the same path as hand-written adapters: their **markers**
drive auto-detection, and their **name** is the `--lang` value. Editor validation
comes from [`schema/adapter.schema.json`](../schema/adapter.schema.json).

## Is my language a good fit?

The engine covers the **common case**, not every language. A spec fits when:

- **Imports are a keyword plus a string or a dotted/`::`-separated name** —
  `require "x"`, `using A.B.C;`, `import Foo.Bar`. Brace-grouped forms
  (Rust `use a::{b, c}`) and `from X import Y` (Python/TS) are *not* in the
  vocabulary; those languages stay hand-written.
- **Resolution is one of two families:**
  - **path** — the specifier is a file path resolved against roots, extensions,
    and index files (Ruby, Lua). 
  - **name-index** — the specifier is a name matched against the set of names the
    project's own files *declare* (C# `namespace`, Elm modules).

If your language needs something outside this, write a hand-written adapter — it
is the same `lang.Adapter` contract and coexists with declarative ones.

## A worked example: Lua

Lua's `require "a.b"` maps a dotted module to a file (`a/b.lua` or `a/b/init.lua`).
Drop this at `.depdog/adapters/lua.yaml`:

```yaml
name: lua
markers: [".luarc.json", "*.rockspec"]   # auto-detect; '*' is a glob
extensions: [".lua"]
skipDirs: [".luarocks", "node_modules"]

comments:
  line: ["--"]
  block:
    - { open: "--[[", close: "]]" }       # long-bracket block comment

strings:
  - { open: "\"", escape: "\\" }
  - { open: "'",  escape: "\\" }

imports:
  - { keyword: require, capture: string, kind: plain }   # require "a.b" / require('a.b')

resolve:
  mode: path
  separator: "."          # "a.b" -> a/b on disk
  roots: ["."]
  extensions: [".lua"]
  indexFiles: ["init.lua"] # require "a" -> a/init.lua

stdlib:
  match: full
  modules: [string, table, math, io, os, coroutine, package, debug, utf8]
```

Now `depdog check` in that repo auto-detects Lua from its markers (or `--lang lua`
forces it). A `require` that resolves to a project file is **in-module**; one that
matches the `stdlib` table is **std**; anything else is **external**.

## The other resolution family: name-index (C#)

C# `using System.Text;` imports a *namespace*, not a file. An import is in-module
when some project file **declares** that namespace. That needs a `provides`
surface and `resolve.mode: name-index`; see the shipped
[`internal/lang/spec/builtin/csharp.yaml`](../internal/lang/spec/builtin/csharp.yaml),
which also shows the `using` modifiers (`global`, `static`, aliases) and the
verbatim/interpolated/raw string forms. In-module C# edges point at the
directory that declares the namespace (the first, sorted, when a namespace spans
several).

## Spec reference (summary)

The full contract — with every field documented — is the JSON schema at
[`schema/adapter.schema.json`](../schema/adapter.schema.json). The top-level keys:

| Key | Purpose |
|-----|---------|
| `name` | The `--lang` value / adapter label. |
| `markers` | Project-root marker files, priority order; a `*` entry is a glob (`*.csproj`). |
| `extensions` | Source extensions to scan (`[".lua"]`). |
| `skipDirs` | Directories pruned from the walk (dotdirs are always pruned). |
| `comments` | `line` prefixes and `block` forms (`nesting`, `lineAnchored`). |
| `strings` | Literal forms: `quoted` (with `escape`, `multiline`, `quoteDoubling`), `char`, `raw-hash` (Rust `r#"…"#`), `raw-run` (C# `"""`). |
| `imports` | Surfaces: `capture: string` / `skip-to-string` / `path-token`, plus modifiers (`prefixKeywords`, `skipKeywords`, `alias`, `strictTerminator`). |
| `provides` | A declaration surface feeding name-index resolution (`namespace Foo.Bar`). |
| `resolve` | `mode: path`\|`name-index`, `separator`, `roots`, `rootsIfExist`, `extensions`, `indexFiles`, `relativeKinds`, `dropSelfEdges`. |
| `stdlib` | `match: full`\|`head`, `modules`, and namespace `prefixes` (C# `System`). |
| `tests` | `stemSuffixes` (`_test`) and `dirs` (`spec`) that mark test-only source. |
| `module` | How `ModulePath` is derived: `dir-basename`, or `fromFile` (a manifest `name =` assignment). |

## Precedence and safety

- A user spec **overrides a built-in** declarative adapter of the same name (patch
  a shipped adapter in one repo), but **never a hand-written** adapter — a
  `.depdog/adapters/go.yaml` is rejected with an error.
- A malformed spec is a hard, human-actionable error naming the file and field —
  never silently ignored.

## Contributing a built-in

To promote a spec to a shipped built-in, add `<name>.yaml` under
`internal/lang/spec/builtin/` (it is `go:embed`-ed automatically) with tests, and
— for a language whose resolution can be reproduced against a hand-written
adapter — a parity test like `internal/lang/spec/parity`.
