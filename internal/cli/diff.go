package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
	"github.com/matterpale/depdog/internal/report"
)

func diffCmd() *cobra.Command {
	var (
		configPath string
		since      string
		format     string
	)
	cmd := &cobra.Command{
		Use:   "diff [packages]",
		Short: "Show how a change moved the architecture, relative to a git ref",
		Long: `diff reports how the working tree's architecture differs from a git ref:
the cross-component import edges added and removed, each flagged if it crosses a
boundary. It is informational (unlike ` + "`check`" + `, which gates on
violations) — surfacing new cross-component structure a review should notice.

The "before" graph is the ref materialized in a temporary git worktree; the
"after" graph is the current working tree. Both are mapped to components under
the current depdog.yaml, so the diff reflects structural movement, not a config
change.

Exit codes: 0 diff written, 2 usage, git or scan error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, args, configPath, since, format)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	cmd.Flags().StringVar(&since, "since", "", "git ref to diff against (required)")
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text, github or json (github/json land later)")
	return cmd
}

func runDiff(cmd *cobra.Command, args []string, configPath, since, format string) error {
	if since == "" {
		return fmt.Errorf("--since <ref> is required (the git ref to diff against)")
	}
	switch format {
	case "text":
	case "github", "json":
		return fmt.Errorf("--format %q is not yet implemented (text only for now)", format)
	default:
		return fmt.Errorf("unknown --format %q (text, github or json)", format)
	}

	// Resolve the project exactly as check does: adapter, module root, config.
	adapter, root, cfgPath, err := resolveModule(cmd, configPath)
	if err != nil {
		return err
	}
	rs, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// After = the current working tree, evaluated against the current config.
	after, err := evaluateWith(cmd, adapter, root, cfgPath, rs, args)
	if err != nil {
		return err
	}

	// Before = the ref materialized in a throwaway worktree, scanned with the
	// same adapter and the current config's RuleSet.
	before, err := beforeGraph(cmd, adapter, root, cfgPath, rs, since, args)
	if err != nil {
		return err
	}

	d, err := report.Diff(before.Graph, after.Graph, rs)
	if err != nil {
		return err
	}
	return report.DiffText(cmd.OutOrStdout(), d, since)
}

// beforeGraph materializes the git ref in a detached worktree, locates the
// module inside it (the current module root's repo-relative path) and scans it
// with the same adapter and the current config's RuleSet, so both graphs are
// assigned components under one architecture definition (D1). The worktree is
// always removed, even on error.
func beforeGraph(cmd *cobra.Command, adapter lang.Adapter, root, cfgPath string, rs *core.RuleSet, since string, args []string) (*evaluation, error) {
	repoRoot, err := gitRepoRoot(root)
	if err != nil {
		return nil, err
	}
	moduleRel, err := filepath.Rel(repoRoot, root)
	if err != nil {
		return nil, fmt.Errorf("locating module inside the repo: %w", err)
	}

	tmp, err := os.MkdirTemp("", "depdog-diff-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	if err := gitWorktreeAdd(repoRoot, tmp, since); err != nil {
		return nil, err
	}
	// Always tear the worktree down, even on scan error or panic.
	defer gitWorktreeRemove(cmd, repoRoot, tmp)

	beforeRoot := filepath.Join(tmp, filepath.FromSlash(moduleRel))
	return evaluateWith(cmd, adapter, beforeRoot, cfgPath, rs, args)
}

// resolveModule resolves the single module a command runs against — adapter,
// module root and config path — mirroring evaluateModule's resolution without
// loading the graph, so a command can drive two scans (current tree + a git
// ref) off one resolution.
func resolveModule(cmd *cobra.Command, configPath string) (adapter lang.Adapter, root, cfgPath string, err error) {
	language, err := languageFlag(cmd)
	if err != nil {
		return lang.Adapter{}, "", "", err
	}
	if configPath != "" {
		if cfgPath, err = filepath.Abs(configPath); err != nil {
			return lang.Adapter{}, "", "", err
		}
		root = filepath.Dir(cfgPath)
		effLang := language
		if effLang == "" {
			effLang = config.PeekLang(cfgPath)
		}
		if adapter, err = pickAdapter(root, effLang); err != nil {
			return lang.Adapter{}, "", "", err
		}
		return adapter, root, cfgPath, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return lang.Adapter{}, "", "", err
	}
	return resolveProject(cwd, language)
}

// gitRepoRoot returns the repository top-level dir for the tree at dir, or an
// actionable error when dir is not inside a git repo.
func gitRepoRoot(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository (%s) — `depdog diff` needs git to materialize the --since ref: %w", dir, err)
	}
	return strings.TrimSpace(out), nil
}

func gitWorktreeAdd(repoRoot, tmp, ref string) error {
	if _, err := runGit(repoRoot, "worktree", "add", "--detach", tmp, ref); err != nil {
		return fmt.Errorf("cannot check out --since %q (unknown ref, or the worktree is dirty?) — try a commit SHA, tag or branch: %w", ref, err)
	}
	return nil
}

// gitWorktreeRemove force-removes the throwaway worktree. Failures are reported
// on stderr but never mask the diff's own outcome — the temp dir is removed by
// the caller's defer regardless.
func gitWorktreeRemove(cmd *cobra.Command, repoRoot, tmp string) {
	if _, err := runGit(repoRoot, "worktree", "remove", "--force", tmp); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "depdog: could not remove temp git worktree %s: %v\n", tmp, err)
	}
}

// runGit runs a git subcommand in dir and returns its stdout. On failure the
// error carries git's stderr so messages stay actionable.
func runGit(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}
