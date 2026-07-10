package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ModuleRoot walks up from startDir to the nearest go.mod and returns its
// directory — the single module a command operates on. Workspaces are handled a
// layer up (see config.FindWorkspace and the CLI's workspace fan-out); this
// function always resolves exactly one module, which is also the module a
// workspace member is checked as. Shared by `check`, `init`, and friends.
func ModuleRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for d := dir; ; {
		if exists(filepath.Join(d, "go.mod")) {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", fmt.Errorf("no go.mod found from %s upward — depdog runs inside a Go module", dir)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
