package config

import (
	"os"
	"path/filepath"
	"testing"
)

func wsWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mkWorkspaceTree lays down a go.work with app + libs members and returns the
// workspace dir.
func mkWorkspaceTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	wsWriteFile(t, filepath.Join(dir, "go.work"), "go 1.26\n\nuse ./app\nuse ./libs\n")
	wsWriteFile(t, filepath.Join(dir, "app", "go.mod"), "module example.test/app\n\ngo 1.26\n")
	wsWriteFile(t, filepath.Join(dir, "libs", "go.mod"), "module example.test/libs\n\ngo 1.26\n")
	return dir
}

func TestFindWorkspace(t *testing.T) {
	t.Setenv("GOWORK", "")
	dir := mkWorkspaceTree(t)
	ws, err := FindWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ws == nil {
		t.Fatal("expected a workspace")
	}
	if len(ws.Modules) != 2 {
		t.Fatalf("modules = %v, want 2", ws.Modules)
	}
	// `use` order is preserved.
	if filepath.Base(ws.Modules[0]) != "app" || filepath.Base(ws.Modules[1]) != "libs" {
		t.Errorf("members out of order: %v", ws.Modules)
	}
}

func TestFindWorkspaceFromMemberSubdir(t *testing.T) {
	t.Setenv("GOWORK", "")
	dir := mkWorkspaceTree(t)
	ws, err := FindWorkspace(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatal(err)
	}
	if ws == nil || len(ws.Modules) != 2 {
		t.Fatalf("walking up from a member should find the workspace: %+v", ws)
	}
}

func TestFindWorkspaceGOWORKOff(t *testing.T) {
	t.Setenv("GOWORK", "off")
	dir := mkWorkspaceTree(t)
	ws, err := FindWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ws != nil {
		t.Errorf("GOWORK=off must disable workspaces, got %+v", ws)
	}
}

func TestFindWorkspaceExplicitGOWORK(t *testing.T) {
	dir := mkWorkspaceTree(t)
	t.Setenv("GOWORK", filepath.Join(dir, "go.work"))
	// Resolving from an unrelated dir still uses the pointed-at go.work.
	ws, err := FindWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if ws == nil || len(ws.Modules) != 2 {
		t.Fatalf("explicit GOWORK should resolve that workspace: %+v", ws)
	}
}

func TestFindWorkspaceNone(t *testing.T) {
	t.Setenv("GOWORK", "")
	dir := t.TempDir()
	wsWriteFile(t, filepath.Join(dir, "go.mod"), "module example.test/solo\n\ngo 1.26\n")
	ws, err := FindWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ws != nil {
		t.Errorf("no go.work: want nil, got %+v", ws)
	}
}

func TestFindWorkspaceMissingModule(t *testing.T) {
	t.Setenv("GOWORK", "")
	dir := t.TempDir()
	wsWriteFile(t, filepath.Join(dir, "go.work"), "go 1.26\n\nuse ./ghost\n")
	if err := os.MkdirAll(filepath.Join(dir, "ghost"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := FindWorkspace(dir); err == nil {
		t.Fatal("a use dir without go.mod should error")
	}
}

func TestModuleRootInWorkspaceResolvesMember(t *testing.T) {
	// Regression: ModuleRoot used to refuse when a go.work sat above; it must now
	// return the nearest module — what a workspace member (and the LSP rooted in
	// it) is checked as.
	t.Setenv("GOWORK", "")
	dir := mkWorkspaceTree(t)
	root, err := ModuleRoot(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatalf("ModuleRoot must not refuse inside a workspace: %v", err)
	}
	if filepath.Base(root) != "app" {
		t.Errorf("root = %s, want the app member", root)
	}
}

func TestWorkspaceOwningModule(t *testing.T) {
	t.Setenv("GOWORK", "")
	dir := mkWorkspaceTree(t)
	ws, err := FindWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "app")
	libsDir := filepath.Join(dir, "libs")

	cases := []struct {
		name string
		path string
		want string // "" means: expect ok=false
	}{
		{"file inside app", filepath.Join(appDir, "internal", "handler", "handler.go"), appDir},
		{"the app dir itself", appDir, appDir},
		{"file inside libs", filepath.Join(libsDir, "store", "store.go"), libsDir},
		{"the workspace root owns no member", dir, ""},
		{"a sibling outside the workspace", filepath.Dir(dir), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ws.OwningModule(tc.path)
			if tc.want == "" {
				if ok {
					t.Errorf("OwningModule(%s) = %s, want no owner", tc.path, got)
				}
				return
			}
			if !ok || got != tc.want {
				t.Errorf("OwningModule(%s) = %q, %v; want %q, true", tc.path, got, ok, tc.want)
			}
		})
	}
}

func TestModulePathOf(t *testing.T) {
	dir := mkWorkspaceTree(t)
	mp, err := ModulePathOf(filepath.Join(dir, "libs"))
	if err != nil {
		t.Fatal(err)
	}
	if mp != "example.test/libs" {
		t.Errorf("module path = %q", mp)
	}
}
