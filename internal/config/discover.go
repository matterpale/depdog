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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
