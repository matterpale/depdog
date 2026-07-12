package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/tui"
	"github.com/matterpale/depdog/internal/wizard"
)

func tuiCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "tui [packages]",
		Short: "Explore the check result in an interactive terminal UI",
		Long: `tui runs the check and opens an interactive view: a component summary
dashboard and a browsable list of violations.

Exit codes: 0 on quit, 2 configuration or usage error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isInteractive(cmd) {
				return errors.New("depdog tui needs an interactive terminal; use `depdog check` for non-interactive output")
			}
			ev, err := evaluateModule(cmd, configPath, args)
			if err != nil {
				return err
			}
			return launch(cmd, configPath, args, ev)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	return cmd
}

// launch builds the package-navigation views and opens the UI, wiring in the
// module root (so `e` can open module-relative positions in $EDITOR), the
// config path and compiled rule set (for the Config tab), and a refresh hook
// that re-runs the same load+check pipeline for `r`. The hook hands back the
// recompiled rule set too, since core.Result does not carry it.
func launch(cmd *cobra.Command, configPath string, args []string, ev *evaluation) error {
	pkgs, err := core.BuildPackageViews(ev.Graph, ev.Rules)
	if err != nil {
		return err
	}
	root := filepath.Dir(ev.ConfigPath)
	configRel := configRelPath(root, ev.ConfigPath)
	refresh := func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
		ev, err := evaluateModule(cmd, configPath, args)
		if err != nil {
			return nil, nil, nil, err
		}
		pkgs, err := core.BuildPackageViews(ev.Graph, ev.Rules)
		if err != nil {
			return nil, nil, nil, err
		}
		return ev.Result, pkgs, ev.Rules, nil
	}
	// The visual editor stages edits in memory: the transformers are pure config
	// rewriters, Eval recompiles a candidate config against the loaded graph (code
	// doesn't change during a session, so the graph is reused), and only Save
	// writes the file. Validation mirrors `init` (ValidateName / ValidatePattern).
	editor := tui.Editor{
		Load: func() ([]byte, error) { return os.ReadFile(ev.ConfigPath) },
		Save: func(data []byte) error { return os.WriteFile(ev.ConfigPath, data, 0o644) },
		Eval: func(data []byte) (*core.Result, []core.PackageView, *core.RuleSet, error) {
			rs, err := config.Parse(data)
			if err != nil {
				return nil, nil, nil, err
			}
			res, err := core.Evaluate(ev.Graph, rs)
			if err != nil {
				return nil, nil, nil, err
			}
			views, err := core.BuildPackageViews(ev.Graph, rs)
			if err != nil {
				return nil, nil, nil, err
			}
			return res, views, rs, nil
		},
		SetRule: config.SetComponentRule,
		AddComponent: func(data []byte, name, pattern string) ([]byte, error) {
			if err := wizard.ValidateName(name); err != nil {
				return nil, err
			}
			if err := core.ValidatePattern(pattern); err != nil {
				return nil, err
			}
			return config.MergeComponents(data, []config.MergeComponent{{Name: name, Patterns: []string{pattern}}})
		},
		Repath: func(data []byte, component string, patterns []string) ([]byte, error) {
			for _, p := range patterns {
				if err := core.ValidatePattern(p); err != nil {
					return nil, err
				}
			}
			return config.SetComponentPath(data, component, patterns)
		},
		Rename: func(data []byte, oldName, newName string) ([]byte, error) {
			if err := wizard.ValidateName(newName); err != nil {
				return nil, err
			}
			return config.RenameComponent(data, oldName, newName)
		},
		AddMember:    config.AddBoundaryMember,
		RemoveMember: config.RemoveBoundaryMember,
	}
	return tui.Run(ev.Result, pkgs,
		tui.WithRoot(root),
		tui.WithConfig(configRel, ev.Rules),
		tui.WithRefresh(refresh),
		tui.WithEditor(editor))
}

// configRelPath renders the config path relative to the module root for display,
// falling back to the base name when it lies outside the root.
func configRelPath(root, configPath string) string {
	if rel, err := filepath.Rel(root, configPath); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(configPath)
}

// runBare backs a plain `depdog` invocation: it runs the check (like
// `depdog check`), so a bare `depdog` yields the real 0/1/2 exit code, on a
// terminal or in a pipe. `depdog tui` opens the interactive view.
//
// A first-time user in a directory with no depdog project is pointed at `init`
// rather than shown a config error — but only when they named no explicit target
// (a package arg or --config); an explicit target always runs the check.
func runBare(cmd *cobra.Command, args []string, o checkOptions) error {
	if len(args) == 0 && o.configPath == "" && !projectResolves(cmd) {
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "depdog keeps a project's internal imports pointing the right way.")
		fmt.Fprintln(out, "Run `depdog init` to create a depdog.yaml, `depdog check` to verify import rules,")
		fmt.Fprintln(out, "or `depdog tui` for the interactive view. `depdog --help` lists everything.")
		return nil
	}
	return runCheck(cmd, args, o)
}

// projectResolves reports whether a depdog project (an adapter and a config) can
// be found from the current directory — the signal that separates a first-time
// user from a real check run.
func projectResolves(cmd *cobra.Command) bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	language, err := languageFlag(cmd)
	if err != nil {
		return false
	}
	_, _, _, err = resolveProject(cwd, language)
	return err == nil
}
