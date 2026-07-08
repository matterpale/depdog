package typescript

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// sourceExts is the extension precedence used when resolving a specifier that
// omits its extension, mirroring TS/Node resolution order.
var sourceExts = []string{".ts", ".tsx", ".d.ts", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"}

// nodeBuiltins is the set of Node standard-library module names. Anything here
// (with or without the `node:` prefix) classifies as std; everything else bare
// is external. A specifier that literally starts with `node:` is always std.
var nodeBuiltins = map[string]bool{
	"assert": true, "async_hooks": true, "buffer": true, "child_process": true,
	"cluster": true, "console": true, "constants": true, "crypto": true,
	"dgram": true, "diagnostics_channel": true, "dns": true, "domain": true,
	"events": true, "fs": true, "http": true, "http2": true, "https": true,
	"inspector": true, "module": true, "net": true, "os": true, "path": true,
	"perf_hooks": true, "process": true, "punycode": true, "querystring": true,
	"readline": true, "repl": true, "stream": true, "string_decoder": true,
	"sys": true, "timers": true, "tls": true, "trace_events": true, "tty": true,
	"url": true, "util": true, "v8": true, "vm": true, "wasi": true,
	"worker_threads": true, "zlib": true,
}

// classify buckets a raw specifier into one of core's three classes and, for
// in-module targets, derives the module-relative directory of the target.
//
//   - relative (`./` `../`)      -> on-disk resolution; in-module if found,
//     external if not (a broken relative import can't invent a violation).
//   - tsconfig `paths` alias      -> baseUrl-relative on-disk resolution;
//     in-module if it lands under root, external otherwise.
//   - `node:`-prefixed or builtin -> std.
//   - anything else bare          -> external (no node_modules required).
//
// ok is always true here: classification never fails, it degrades. The bool is
// part of the signature so a future caller can distinguish "skip this edge"
// without a breaking change.
func classify(spec, fromDir, root string, cfg *tsconfig) (class core.Class, relDir string, ok bool) {
	// node: prefix is unconditionally std.
	if strings.HasPrefix(spec, "node:") {
		return core.ClassStd, "", true
	}
	// Bare builtin name (fs, path, ...) is std.
	if nodeBuiltins[spec] {
		return core.ClassStd, "", true
	}

	// Relative specifier: resolve against the importing file's directory.
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || spec == "." || spec == ".." {
		base := filepath.Join(fromDir, filepath.FromSlash(spec))
		if dir, found := resolveOnDisk(base); found {
			return core.ClassInModule, relDirOf(root, dir), true
		}
		// Unresolved relative import: degrade to external rather than fabricate.
		return core.ClassExternal, "", true
	}

	// tsconfig paths alias.
	if len(cfg.Paths) > 0 {
		if target, matched := applyPathAlias(spec, cfg); matched {
			base := filepath.Join(aliasBaseDir(root, cfg), filepath.FromSlash(target))
			if dir, found := resolveOnDisk(base); found && withinRoot(root, dir) {
				return core.ClassInModule, relDirOf(root, dir), true
			}
			// Alias matched a pattern but did not resolve on disk (or points
			// outside root): treat as external, never crash.
			return core.ClassExternal, "", true
		}
	}

	// Bare specifier that is neither relative, alias, nor builtin: external.
	return core.ClassExternal, "", true
}

// applyPathAlias tries the tsconfig `paths` patterns against spec. On a match
// it returns the first substituted target and true. Supports the common
// single-`*` wildcard form (`@app/*` -> `["src/*"]`) and exact keys.
func applyPathAlias(spec string, cfg *tsconfig) (string, bool) {
	// Exact (non-wildcard) match first.
	if targets, ok := cfg.Paths[spec]; ok && len(targets) > 0 {
		return targets[0], true
	}
	for pattern, targets := range cfg.Paths {
		if len(targets) == 0 {
			continue
		}
		star := strings.IndexByte(pattern, '*')
		if star < 0 {
			continue // exact patterns handled above
		}
		prefix := pattern[:star]
		suffix := pattern[star+1:]
		if strings.HasPrefix(spec, prefix) && strings.HasSuffix(spec, suffix) &&
			len(spec) >= len(prefix)+len(suffix) {
			captured := spec[len(prefix) : len(spec)-len(suffix)]
			return strings.Replace(targets[0], "*", captured, 1), true
		}
	}
	return "", false
}

// aliasBaseDir is the directory that tsconfig `paths` targets are resolved
// against: baseUrl relative to root, or root itself when baseUrl is unset.
func aliasBaseDir(root string, cfg *tsconfig) string {
	if cfg.BaseURL == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(cfg.BaseURL))
}

// resolveOnDisk tries base with each source extension, then base/index with
// each extension. It returns the containing directory of the resolved file.
func resolveOnDisk(base string) (dir string, ok bool) {
	// Exact file first (specifier already carried an extension).
	if isFile(base) {
		return filepath.Dir(base), true
	}
	// base + extension.
	for _, ext := range sourceExts {
		if isFile(base + ext) {
			return filepath.Dir(base + ext), true
		}
	}
	// base is a directory -> its index.*.
	if isDir(base) {
		for _, ext := range sourceExts {
			if isFile(filepath.Join(base, "index"+ext)) {
				return base, true
			}
		}
	}
	return "", false
}

// relDirOf derives the module-relative, slash-separated directory of dir,
// returning "." for the root itself — the same convention as the Go adapter.
func relDirOf(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "."
	}
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." {
		return "."
	}
	return rel
}

// withinRoot reports whether dir is root or lives under it.
func withinRoot(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (!strings.HasPrefix(rel, "../") && rel != "..")
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
