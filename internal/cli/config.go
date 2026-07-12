package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/report"
)

func configCmd() *cobra.Command {
	var (
		configPath string
		color      string
	)
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the compiled rule set",
		Long: `config loads depdog.yaml and prints the compiled rule set — the policy
and options, then each component with its patterns, inferred stance and rule —
to debug a configuration without running a full check.

Exit codes: 0 shown, 2 configuration or usage error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch color {
			case "auto", "always", "never":
			default:
				return fmt.Errorf("unknown --color %q (auto, always or never)", color)
			}
			cfgPath := configPath
			if cfgPath == "" {
				language, err := languageFlag(cmd)
				if err != nil {
					return err
				}
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				if _, _, cfgPath, err = resolveProject(cwd, language); err != nil {
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
			return report.RuleSet(cmd.OutOrStdout(), rs, color)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	cmd.Flags().StringVar(&color, "color", "auto", "colorize output: auto, always or never")
	return cmd
}
