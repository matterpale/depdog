package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// hookMarker identifies a pre-commit hook depdog wrote, so re-running
// install-hook is idempotent and we never clobber a hook we didn't create.
const hookMarker = "# depdog pre-commit hook — installed by `depdog install-hook`"

// hookScript is the pre-commit hook body. It runs the check via PATH (depdog is
// expected to be installed); a failing check (exit 1) or config error (exit 2)
// blocks the commit.
const hookScript = "#!/bin/sh\n" +
	hookMarker + "\n" +
	"# Enforces architecture rules before each commit. Remove this file to uninstall.\n" +
	"exec depdog check\n"

func installHookCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install-hook",
		Short: "Install a git pre-commit hook that runs `depdog check`",
		Long: `install-hook writes a git pre-commit hook that runs depdog check before
each commit, so a change that breaks the architecture is caught locally instead
of in CI. depdog must be on PATH when the hook runs.

It is idempotent — re-running it refreshes depdog's own hook — and it refuses to
overwrite a pre-commit hook it did not write unless you pass --force.

Exit codes: 0 installed, 2 not a git repository, a foreign hook exists, or an IO
error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstallHook(cmd, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing pre-commit hook not written by depdog")
	return cmd
}

func runInstallHook(cmd *cobra.Command, force bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path, err := preCommitHookPath(cwd)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if existing, err := os.ReadFile(path); err == nil {
		switch {
		case strings.Contains(string(existing), hookMarker):
			// Our own hook: refresh it (idempotent).
			if err := writeHook(path); err != nil {
				return err
			}
			fmt.Fprintf(out, "depdog pre-commit hook refreshed at %s\n", path)
			return nil
		case !force:
			return fmt.Errorf("a pre-commit hook depdog did not write already exists at %s; "+
				"re-run with --force to replace it (or remove that hook first)", path)
		}
		// force: fall through and overwrite the foreign hook.
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := writeHook(path); err != nil {
		return err
	}
	fmt.Fprintf(out, "installed depdog pre-commit hook at %s\n", path)
	return nil
}

// writeHook writes the executable hook script atomically-enough for a local hook
// (truncate + write, then chmod), leaving 0o755 so git will run it.
func writeHook(path string) error {
	if err := os.WriteFile(path, []byte(hookScript), 0o755); err != nil {
		return err
	}
	// WriteFile only applies the mode on create; ensure it is executable even
	// when overwriting an existing non-executable file.
	return os.Chmod(path, 0o755)
}

// preCommitHookPath resolves where the pre-commit hook belongs for the git repo
// containing dir: a repo-local core.hooksPath when configured, else the repo's
// hooks dir (via `git rev-parse --git-path`, which is correct for worktrees and
// submodules where .git is a file). It reads core.hooksPath at --local scope only
// — a *global* hooksPath is deliberately ignored so install-hook never writes a
// hook that affects every one of the user's repos. It errors actionably when dir
// is not in a git repo.
func preCommitHookPath(dir string) (string, error) {
	if hp, err := runGit(dir, "config", "--local", "--get", "core.hooksPath"); err == nil {
		if hp = strings.TrimSpace(hp); hp != "" {
			if !filepath.IsAbs(hp) {
				hp = filepath.Join(dir, hp)
			}
			return filepath.Join(hp, "pre-commit"), nil
		}
	}
	out, err := runGit(dir, "rev-parse", "--git-path", "hooks/pre-commit")
	if err != nil {
		return "", fmt.Errorf("not a git repository (%s) — `depdog install-hook` needs one to install a pre-commit hook: %w", dir, err)
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return p, nil
}
