package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestInstallHook exercises `depdog install-hook` end to end in a throwaway git
// repo: fresh install, idempotent re-install, refuse-to-clobber a foreign hook,
// and --force override.
func TestInstallHook(t *testing.T) {
	// Ignore the developer's global/system git config so a global core.hooksPath
	// can't redirect the hook out of the temp repo (the binary's own git calls
	// inherit these).
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	dir := t.TempDir()
	gitInit(t, dir)
	hookPath := filepath.Join(dir, ".git", "hooks", "pre-commit")

	// Fresh install.
	out, stderr, exit := run(t, dir, "install-hook")
	if exit != 0 {
		t.Fatalf("install-hook exit %d\nstdout: %s\nstderr: %s", exit, out, stderr)
	}
	body, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("hook not written: %v", err)
	}
	if !strings.Contains(string(body), "depdog check") {
		t.Errorf("hook does not run depdog check:\n%s", body)
	}
	// Windows has no Unix executable bit (Go always reports -rw-rw-rw-), and git
	// runs pre-commit hooks via sh there regardless, so only assert it off Windows.
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(hookPath); fi.Mode()&0o111 == 0 {
			t.Errorf("hook is not executable: %v", fi.Mode())
		}
	}

	// Idempotent: re-running succeeds and keeps our hook.
	if _, _, exit := run(t, dir, "install-hook"); exit != 0 {
		t.Errorf("re-install should be idempotent (exit 0), got %d", exit)
	}

	// A foreign pre-commit hook is not clobbered without --force.
	writeExec(t, hookPath, "#!/bin/sh\necho custom-hook\n")
	_, stderr, exit = run(t, dir, "install-hook")
	if exit != 2 {
		t.Errorf("foreign hook should be refused (exit 2), got %d\n%s", exit, stderr)
	}
	if !strings.Contains(strings.ToLower(stderr), "force") {
		t.Errorf("refusal should mention --force: %s", stderr)
	}
	if b, _ := os.ReadFile(hookPath); !strings.Contains(string(b), "custom-hook") {
		t.Errorf("foreign hook was overwritten without --force:\n%s", b)
	}

	// --force replaces the foreign hook.
	if _, _, exit := run(t, dir, "install-hook", "--force"); exit != 0 {
		t.Errorf("--force install should succeed (exit 0), got %d", exit)
	}
	if b, _ := os.ReadFile(hookPath); !strings.Contains(string(b), "depdog check") {
		t.Errorf("--force did not install the depdog hook:\n%s", b)
	}
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestInstallHookRelativeHooksPathFromSubdir: with a repo-local, *relative*
// core.hooksPath, install-hook run from a subdirectory must write the hook at
// the worktree top level (where git looks), not under the cwd.
func TestInstallHookRelativeHooksPathFromSubdir(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	repo := t.TempDir()
	gitInit(t, repo)
	git(t, repo, "config", "core.hooksPath", ".githooks")
	sub := filepath.Join(repo, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, stderr, exit := run(t, sub, "install-hook"); exit != 0 {
		t.Fatalf("install-hook from subdir exit %d\n%s", exit, stderr)
	}
	if _, err := os.Stat(filepath.Join(repo, ".githooks", "pre-commit")); err != nil {
		t.Errorf("hook not written at the worktree top-level .githooks (git's location): %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".githooks", "pre-commit")); err == nil {
		t.Errorf("hook wrongly written under the subdir cwd rather than the worktree top")
	}
}
