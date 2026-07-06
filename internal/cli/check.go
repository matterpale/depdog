package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/report"
)

func checkCmd() *cobra.Command {
	var (
		configPath string
		format     string
		failOn     string
	)
	cmd := &cobra.Command{
		Use:   "check [packages]",
		Short: "Evaluate the module's imports against depdog.yaml",
		Long: `check loads the module's package graph, maps packages to the components
declared in depdog.yaml and evaluates every import edge against the rules.

With --fail-on new, violations already recorded in depdog.baseline.yaml are
suppressed and only new ones fail the run (see ` + "`depdog baseline`" + `).

Exit codes: 0 clean, 1 violations found, 2 configuration or usage error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if failOn != "any" && failOn != "new" {
				return fmt.Errorf("unknown --fail-on %q (any or new)", failOn)
			}
			start := time.Now()

			res, cfgPath, err := evaluateModule(cmd, configPath, args)
			if err != nil {
				return err
			}

			suppressed := 0
			if failOn == "new" {
				base, err := config.LoadBaselineOrEmpty(filepath.Join(filepath.Dir(cfgPath), config.BaselineName))
				if err != nil {
					return err
				}
				res, suppressed = base.Filter(res)
			}
			elapsed := time.Since(start)

			out := cmd.OutOrStdout()
			switch format {
			case "text":
				err = report.Text(out, res, elapsed)
			case "json":
				err = report.JSON(out, res, elapsed)
			case "github":
				err = report.GitHub(out, res)
			case "sarif":
				err = report.SARIF(out, res, Version)
			default:
				return fmt.Errorf("unknown --format %q (text, json, github or sarif)", format)
			}
			if err != nil {
				return err
			}
			if suppressed > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "depdog: %d baselined violation(s) suppressed (%s)\n",
					suppressed, config.BaselineName)
			}

			if len(res.Violations) > 0 {
				// The report already told the story; exit 1 without an
				// error banner so CI output stays clean.
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text, json, github or sarif")
	cmd.Flags().StringVar(&failOn, "fail-on", "any", "which violations fail the run: any or new (honors depdog.baseline.yaml)")
	return cmd
}
