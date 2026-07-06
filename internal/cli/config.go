package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/report"
)

func configCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the compiled rule set",
		Long: `config loads depdog.yaml and prints the compiled rule set — the policy
and options, then each component with its patterns, inferred stance and rule —
to debug a configuration without running a full check.

Exit codes: 0 shown, 2 configuration or usage error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := configPath
			if cfgPath == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				if cfgPath, _, err = config.Find(cwd); err != nil {
					return err
				}
			} else {
				var err error
				if cfgPath, err = filepath.Abs(cfgPath); err != nil {
					return err
				}
			}
			rs, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			return report.RuleSet(cmd.OutOrStdout(), rs)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	return cmd
}
