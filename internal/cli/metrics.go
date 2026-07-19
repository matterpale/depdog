package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/report"
)

func metricsCmd() *cobra.Command {
	var (
		configPath string
		format     string
	)
	cmd := &cobra.Command{
		Use:   "metrics [packages]",
		Short: "Report architecture-health metrics (coupling, instability)",
		Long: `metrics reports architecture-health numbers over the component graph: per
component, its afferent coupling (fan-in), efferent coupling (fan-out) and
instability I = fan-out / (fan-in + fan-out); plus repo-level totals
(components, cross-component edges, boundary crossings and component cycles).

It answers "is my architecture getting better or worse?" outside a red check —
it is informational and always exits 0. Choose --format text (a table) or json
(a stable, snake_case object for agents and CI).

Exit codes: 0 written, 2 configuration or usage error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "text", "json":
			default:
				return fmt.Errorf("unknown --format %q (choose text or json)", format)
			}
			ev, err := evaluateModule(cmd, configPath, args)
			if err != nil {
				return err
			}
			m, err := report.Metrics(ev.Graph, ev.Rules, len(ev.Result.Cycles))
			if err != nil {
				return err
			}
			if format == "json" {
				return report.MetricsJSON(cmd.OutOrStdout(), m, ev.Result.ModulePath)
			}
			return report.MetricsText(cmd.OutOrStdout(), m, ev.Result.ModulePath)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text or json")
	return cmd
}
