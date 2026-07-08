package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ModuleRoot walks up from startDir to the nearest go.mod and returns its
// directory. It refuses to run inside a Go workspace: v1 works on exactly one
// module. This is the module discovery shared by `check` and `init`.
func ModuleRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	root := ""
	for d := dir; ; {
		if root == "" && exists(filepath.Join(d, "go.mod")) {
			root = d
		}
		if w := filepath.Join(d, "go.work"); exists(w) && os.Getenv("GOWORK") != "off" {
			return "", fmt.Errorf("go workspaces are not supported yet: found %s — depdog v1 works on a single module (run with GOWORK=off to bypass the workspace)", w)
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	if gw := os.Getenv("GOWORK"); gw != "" && gw != "off" {
		return "", fmt.Errorf("go workspaces are not supported yet: GOWORK=%s — depdog v1 works on a single module", gw)
	}
	if root == "" {
		return "", fmt.Errorf("no go.mod found from %s upward — depdog runs inside a Go module", dir)
	}
	return root, nil
}

// Find locates the module root and expects the config beside its go.mod. It
// refuses to run inside a Go workspace: v1 checks exactly one module.
func Find(startDir string) (configPath, moduleRoot string, err error) {
	root, err := ModuleRoot(startDir)
	if err != nil {
		return "", "", err
	}
	cfg := filepath.Join(root, DefaultName)
	if !exists(cfg) {
		return "", "", fmt.Errorf("no %s next to %s — run `depdog init` to create one", DefaultName, filepath.Join(root, "go.mod"))
	}
	return cfg, root, nil
}

// DetectLanguage walks up from startDir and reports which language adapter the
// project needs, along with the directory of the root marker file it found:
//
//   - "go" when a go.mod is present at/above startDir.
//   - "ts" when a tsconfig.json or package.json is present at/above startDir.
//
// When both a Go and a TS marker exist, the marker nearest startDir wins. A
// genuine tie (both kinds of marker in the same directory) is not guessed at:
// it is an actionable error telling the user to pass --lang. Finding no marker
// at all is likewise an error naming both the Go and the TS/JS markers.
//
// It is a language-neutral companion to ModuleRoot/Find; the Go discovery path
// is left byte-identical for callers that only ever handle Go.
func DetectLanguage(startDir string) (language, root string, err error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", err
	}
	for d := abs; ; {
		goMarker := exists(filepath.Join(d, "go.mod"))
		tsMarker := exists(filepath.Join(d, "tsconfig.json")) || exists(filepath.Join(d, "package.json"))
		switch {
		case goMarker && tsMarker:
			return "", "", fmt.Errorf("ambiguous project language: %s has both a go.mod and a tsconfig.json/package.json — pass --lang go or --lang ts to choose the adapter", d)
		case goMarker:
			return "go", d, nil
		case tsMarker:
			return "ts", d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", "", fmt.Errorf("no project root found from %s upward — depdog runs inside a Go module (go.mod) or a TypeScript/JavaScript project (tsconfig.json or package.json)", abs)
}

// FindWithLanguage resolves the config path and root for a given language. When
// language is empty it auto-detects via DetectLanguage. For "go" it defers to
// Find so the Go path (workspace refusal, error wording) stays byte-identical;
// for "ts" it locates the TS/JS root by marker and expects the config beside
// it. The returned language is the resolved one ("go" or "ts").
func FindWithLanguage(startDir, language string) (configPath, root, resolved string, err error) {
	switch language {
	case "", "auto":
		resolved, root, err = DetectLanguage(startDir)
		if err != nil {
			return "", "", "", err
		}
	case "go", "ts":
		resolved = language
	default:
		return "", "", "", fmt.Errorf("unknown --lang %q (go or ts)", language)
	}

	if resolved == "go" {
		configPath, root, err = Find(startDir)
		return configPath, root, "go", err
	}

	// TypeScript/JavaScript: locate the marker root when auto-detect did not
	// already give us one (explicit --lang ts).
	if root == "" {
		if _, root, err = DetectLanguage(startDir); err != nil {
			// Re-derive a TS-specific root when detection was skipped by an
			// explicit flag; a Go-only tree with --lang ts should still fail
			// with a TS-flavored message.
			r, rerr := tsRoot(startDir)
			if rerr != nil {
				return "", "", "", rerr
			}
			root = r
		}
	}
	cfg := filepath.Join(root, DefaultName)
	if !exists(cfg) {
		return "", "", "", fmt.Errorf("no %s in %s — run `depdog init` to create one", DefaultName, root)
	}
	return cfg, root, "ts", nil
}

// tsRoot walks up from startDir to the nearest tsconfig.json, else the nearest
// package.json — the TS analogue of ModuleRoot, used when --lang ts is forced.
func tsRoot(startDir string) (string, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	firstPkgJSON := ""
	for d := abs; ; {
		if exists(filepath.Join(d, "tsconfig.json")) {
			return d, nil
		}
		if firstPkgJSON == "" && exists(filepath.Join(d, "package.json")) {
			firstPkgJSON = d
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	if firstPkgJSON != "" {
		return firstPkgJSON, nil
	}
	return "", fmt.Errorf("no tsconfig.json or package.json found from %s upward — depdog --lang ts runs inside a TypeScript/JavaScript project", abs)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
