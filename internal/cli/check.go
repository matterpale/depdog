package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
	"github.com/matterpale/depdog/internal/lang/golang"
	"github.com/matterpale/depdog/internal/report"
)

func checkCmd() *cobra.Command {
	var (
		configPath string
		format     string
	)
	cmd := &cobra.Command{
		Use:   "check [packages]",
		Short: "Evaluate the module's imports against depdog.yaml",
		Long: `check loads the module's package graph, maps packages to the components
declared in depdog.yaml and evaluates every import edge against the rules.

Exit codes: 0 clean, 1 violations found, 2 configuration or usage error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()

			var (
				cfgPath string
				root    string
				err     error
			)
			if configPath != "" {
				if cfgPath, err = filepath.Abs(configPath); err != nil {
					return err
				}
				root = filepath.Dir(cfgPath)
			} else {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				if cfgPath, root, err = config.Find(cwd); err != nil {
					return err
				}
			}

			rs, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			var loader lang.Loader = &golang.Loader{Dir: root}
			graph, err := loader.Load(cmd.Context(), args...)
			if err != nil {
				return err
			}

			res, err := core.Evaluate(graph, rs)
			if err != nil {
				return err
			}
			elapsed := time.Since(start)

			out := cmd.OutOrStdout()
			switch format {
			case "text":
				err = report.Text(out, res, elapsed)
			case "json":
				err = report.JSON(out, res, elapsed)
			default:
				return fmt.Errorf("unknown --format %q (text or json)", format)
			}
			if err != nil {
				return err
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
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text or json")
	return cmd
}
