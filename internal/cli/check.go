package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
)

func checkCmd() *cobra.Command {
	var (
		configPath string
		format     string
		failOn     string
		color      string
		modules    []string
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
			switch color {
			case "auto", "always", "never":
			default:
				return fmt.Errorf("unknown --color %q (auto, always or never)", color)
			}
			start := time.Now()

			run, err := evaluateCheckTargets(cmd, configPath, modules, args)
			if err != nil {
				return err
			}

			// Per-member baseline filtering for --fail-on new: each member's
			// baseline sits next to its own config.
			if failOn == "new" {
				for i := range run.Members {
					m := &run.Members[i]
					if m.Eval == nil {
						continue
					}
					base, err := config.LoadBaselineOrEmpty(filepath.Join(filepath.Dir(m.Eval.ConfigPath), config.BaselineName))
					if err != nil {
						return err
					}
					m.Fixed = base.Fixed(m.Eval.Result)
					m.Eval.Result, m.Suppressed = base.Filter(m.Eval.Result)
				}
			}
			elapsed := time.Since(start)

			violations, err := reportCheck(cmd, run, format, color, elapsed)
			if err != nil {
				return err
			}

			suppressed, fixed := 0, 0
			for _, m := range run.Members {
				suppressed += m.Suppressed
				fixed += len(m.Fixed)
			}
			if suppressed > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "depdog: %d baselined violation(s) suppressed (%s)\n",
					suppressed, config.BaselineName)
			}
			if fixed > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"depdog: %d baselined violation(s) now fixed — run `depdog baseline` to shrink %s\n",
					fixed, config.BaselineName)
			}

			if violations > 0 {
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
	cmd.Flags().StringVar(&color, "color", "auto", "colorize text output: auto, always or never")
	cmd.Flags().StringArrayVar(&modules, "module", nil, "in a Go workspace, restrict the check to these members (module path or directory; repeatable)")
	return cmd
}
