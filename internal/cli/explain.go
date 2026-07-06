package cli

import (
	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/report"
)

func explainCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "explain <component-or-package>",
		Short: "Explain why a package or component is red, or how it's constrained",
		Long: `explain answers "why is this red?" for a package — the rule that fired
and the offending edges with file:line — or, for a component, shows how it is
constrained and which packages it holds.

Exit codes: 0 shown, 2 configuration or usage error.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			ev, err := evaluateModule(cmd, configPath, args[1:])
			if err != nil {
				return err
			}
			views, err := core.BuildPackageViews(ev.Graph, ev.Rules)
			if err != nil {
				return err
			}
			return report.Explain(cmd.OutOrStdout(), target, ev.Rules, views, ev.Result)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	return cmd
}
