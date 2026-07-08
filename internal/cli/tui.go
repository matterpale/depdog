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
	return tui.Run(ev.Result, pkgs,
		tui.WithRoot(root),
		tui.WithConfig(configRel, ev.Rules),
		tui.WithRefresh(refresh))
}

// configRelPath renders the config path relative to the module root for display,
// falling back to the base name when it lies outside the root.
func configRelPath(root, configPath string) string {
	if rel, err := filepath.Rel(root, configPath); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(configPath)
}

// runBare backs a plain `depdog` invocation: it opens the TUI when a terminal
// and a config are both present, and otherwise prints guidance. It never errors
// on a missing config — a first-time user gets pointed at `init` instead.
func runBare(cmd *cobra.Command) error {
	if isInteractive(cmd) {
		if cwd, err := os.Getwd(); err == nil {
			if _, _, ferr := config.Find(cwd); ferr == nil {
				ev, eerr := evaluateModule(cmd, "", nil)
				if eerr != nil {
					return eerr
				}
				return launch(cmd, "", nil, ev)
			}
		}
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "depdog keeps a project's internal imports pointing the right way.")
	fmt.Fprintln(out, "Run `depdog init` to create a depdog.yaml, `depdog check` to verify import rules,")
	fmt.Fprintln(out, "or `depdog tui` for the interactive view. `depdog --help` lists everything.")
	return nil
}
