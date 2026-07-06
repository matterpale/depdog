package cli

import (
	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/report"
)

func graphCmd() *cobra.Command {
	var (
		configPath     string
		format         string
		level          string
		violationsOnly bool
		focus          string
	)
	cmd := &cobra.Command{
		Use:   "graph [packages]",
		Short: "Emit the dependency graph (dot or mermaid)",
		Long: `graph prints the module's in-module dependency graph for READMEs and
reviews, with violation edges highlighted. Choose the notation with --format
(dot, mermaid) and the granularity with --level (component, package).
--violations-only trims the graph to just the offending edges, and --focus
limits it to one component and its direct neighbours.

Exit codes: 0 written, 2 configuration or usage error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ev, err := evaluateModule(cmd, configPath, args)
			if err != nil {
				return err
			}
			views, err := core.BuildPackageViews(ev.Graph, ev.Rules)
			if err != nil {
				return err
			}
			return report.Graph(cmd.OutOrStdout(), ev.Result.ModulePath, views, ev.Result.Violations,
				report.GraphOptions{Format: format, Level: level, ViolationsOnly: violationsOnly, Focus: focus})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	cmd.Flags().StringVarP(&format, "format", "f", "dot", "output format: dot or mermaid")
	cmd.Flags().StringVar(&level, "level", "component", "granularity: component or package")
	cmd.Flags().BoolVar(&violationsOnly, "violations-only", false, "show only violation edges and their endpoints")
	cmd.Flags().StringVar(&focus, "focus", "", "limit the graph to a component and its direct neighbours")
	return cmd
}
