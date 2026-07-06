package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
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
			res, _, err := evaluateModule(cmd, configPath, args)
			if err != nil {
				return err
			}
			return tui.Run(res)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	return cmd
}

// runBare backs a plain `depdog` invocation: it opens the TUI when a terminal
// and a config are both present, and otherwise prints guidance. It never errors
// on a missing config — a first-time user gets pointed at `init` instead.
func runBare(cmd *cobra.Command) error {
	if isInteractive(cmd) {
		if cwd, err := os.Getwd(); err == nil {
			if _, _, ferr := config.Find(cwd); ferr == nil {
				res, _, eerr := evaluateModule(cmd, "", nil)
				if eerr != nil {
					return eerr
				}
				return tui.Run(res)
			}
		}
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "depdog keeps a project's internal imports pointing the right way.")
	fmt.Fprintln(out, "Run `depdog init` to create a depdog.yaml, `depdog check` to verify import rules,")
	fmt.Fprintln(out, "or `depdog tui` for the interactive view. `depdog --help` lists everything.")
	return nil
}
