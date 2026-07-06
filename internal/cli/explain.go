package cli

import (
	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/report"
)

func explainCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "explain <component-or-package> [import]",
		Short: "Explain why a package or component is red, or whether one import is allowed",
		Long: `explain answers "why is this red?" for a package — the rule that fired
and the offending edges with file:line — or, for a component, shows how it is
constrained and which packages it holds.

Given a second argument, it explains a single edge: whether the first package
may import the second (a package, component, or std/external), and which rule or
policy decides it.

Exit codes: 0 shown, 2 configuration or usage error.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ev, err := evaluateModule(cmd, configPath, nil)
			if err != nil {
				return err
			}
			views, err := core.BuildPackageViews(ev.Graph, ev.Rules)
			if err != nil {
				return err
			}
			if len(args) == 2 {
				return report.ExplainEdge(cmd.OutOrStdout(), args[0], args[1], ev.Rules, views, ev.Result)
			}
			return report.Explain(cmd.OutOrStdout(), args[0], ev.Rules, views, ev.Result)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	return cmd
}
