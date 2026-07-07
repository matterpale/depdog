package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
// module root (so `e` can open module-relative positions in $EDITOR) and a
// refresh hook that re-runs the same load+check pipeline for `r`.
func launch(cmd *cobra.Command, configPath string, args []string, ev *evaluation) error {
	pkgs, err := core.BuildPackageViews(ev.Graph, ev.Rules)
	if err != nil {
		return err
	}
	refresh := func() (*core.Result, []core.PackageView, error) {
		ev, err := evaluateModule(cmd, configPath, args)
		if err != nil {
			return nil, nil, err
		}
		pkgs, err := core.BuildPackageViews(ev.Graph, ev.Rules)
		if err != nil {
			return nil, nil, err
		}
		return ev.Result, pkgs, nil
	}
	return tui.Run(ev.Result, pkgs,
		tui.WithRoot(filepath.Dir(ev.ConfigPath)),
		tui.WithRefresh(refresh))
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
