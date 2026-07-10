package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// Workspace is a resolved Go workspace: the directory holding go.work and the
// absolute directories of the modules it `use`s (each containing a go.mod), in
// go.work order.
type Workspace struct {
	Dir     string   // directory containing go.work
	File    string   // absolute path to go.work
	Modules []string // absolute member module dirs, in `use` order
}

// FindWorkspace locates the active Go workspace for startDir, mirroring the go
// tool's GOWORK rules:
//
//   - GOWORK=off        → workspaces disabled; returns (nil, nil).
//   - GOWORK=<file>     → that go.work is used.
//   - GOWORK unset/""   → the nearest go.work walking up from startDir, or
//     (nil, nil) when there is none.
//
// A resolved workspace whose `use` entries do not hold a go.mod is an error, so
// callers fail loudly rather than silently analyzing nothing.
func FindWorkspace(startDir string) (*Workspace, error) {
	switch gw := os.Getenv("GOWORK"); {
	case gw == "off":
		return nil, nil
	case gw != "":
		abs, err := filepath.Abs(gw)
		if err != nil {
			return nil, err
		}
		return parseWorkspace(abs)
	default:
		abs, err := filepath.Abs(startDir)
		if err != nil {
			return nil, err
		}
		work := findWorkFileUp(abs)
		if work == "" {
			return nil, nil
		}
		return parseWorkspace(work)
	}
}

// OwningModule returns the member module directory that contains path — the
// nearest member that is path itself or an ancestor of it — reporting false
// when path lies outside every member. It lets a caller resolve the workspace
// member a given file belongs to, so an edit inside ./app is checked as app
// rather than the workspace root.
func (w *Workspace) OwningModule(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	best := ""
	for _, m := range w.Modules {
		if abs == m || strings.HasPrefix(abs, m+string(filepath.Separator)) {
			if len(m) > len(best) {
				best = m
			}
		}
	}
	return best, best != ""
}

// ModulePathOf reads the module directive from the go.mod in moduleDir.
func ModulePathOf(moduleDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return "", err
	}
	if mp := modfile.ModulePath(data); mp != "" {
		return mp, nil
	}
	return "", fmt.Errorf("no module directive in %s", filepath.Join(moduleDir, "go.mod"))
}

func parseWorkspace(workFile string) (*Workspace, error) {
	data, err := os.ReadFile(workFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", workFile, err)
	}
	wf, err := modfile.ParseWork(workFile, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", workFile, err)
	}
	dir := filepath.Dir(workFile)
	ws := &Workspace{Dir: dir, File: workFile}
	for _, u := range wf.Use {
		md := filepath.FromSlash(u.Path)
		if !filepath.IsAbs(md) {
			md = filepath.Join(dir, md)
		}
		if !exists(filepath.Join(md, "go.mod")) {
			return nil, fmt.Errorf("workspace %s uses %q, which has no go.mod", workFile, u.Path)
		}
		ws.Modules = append(ws.Modules, filepath.Clean(md))
	}
	return ws, nil
}

func findWorkFileUp(dir string) string {
	for d := dir; ; {
		if w := filepath.Join(d, "go.work"); exists(w) {
			return w
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}
