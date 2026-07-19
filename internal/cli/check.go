package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
)

// checkOptions holds the flags that drive a check run. They are shared by the
// `check` subcommand and the bare `depdog` invocation (which runs the check), so
// both offer the same flags via bind and the same behaviour via runCheck.
type checkOptions struct {
	configPath string
	format     string
	failOn     string
	color      string
	modules    []string
	all        bool
	units      []string
	watch      bool
}

// bind registers the check flags on cmd, writing into o.
func (o *checkOptions) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	cmd.Flags().StringVarP(&o.format, "format", "f", "text", "output format: text, json, github or sarif")
	cmd.Flags().StringVar(&o.failOn, "fail-on", "any", "which violations fail the run: any or new (honors depdog.baseline.yaml)")
	cmd.Flags().StringVar(&o.color, "color", "auto", "colorize text output: auto, always or never")
	cmd.Flags().StringArrayVar(&o.modules, "module", nil, "in a Go workspace, restrict the check to these members (module path or directory; repeatable)")
	cmd.Flags().BoolVar(&o.all, "all", false, "discover and check every depdog.yaml under the cwd (polyglot monorepo mode)")
	cmd.Flags().StringArrayVar(&o.units, "unit", nil, "with --all, restrict the check to these units (config directory; repeatable)")
	cmd.Flags().BoolVar(&o.watch, "watch", false, "re-run the check whenever a file changes (text output only; Ctrl-C to stop)")
}

func checkCmd() *cobra.Command {
	var o checkOptions
	cmd := &cobra.Command{
		Use:   "check [packages]",
		Short: "Evaluate the module's imports against depdog.yaml",
		Long: `check loads the module's package graph, maps packages to the components
declared in depdog.yaml and evaluates every import edge against the rules.

With --fail-on new, violations already recorded in depdog.baseline.yaml are
suppressed and only new ones fail the run (see ` + "`depdog baseline`" + `).

Exit codes: 0 clean, 1 violations found, 2 configuration or usage error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runCheck(cmd, args, o) },
	}
	o.bind(cmd)
	return cmd
}

// runCheck evaluates the module's imports against depdog.yaml, prints the report
// and exits 1 when violations remain. It backs both `depdog check` and the bare
// `depdog` invocation. With --watch it instead loops, re-running on every file
// change and never exiting non-zero (a dev loop, not a gate).
func runCheck(cmd *cobra.Command, args []string, o checkOptions) error {
	if o.failOn != "any" && o.failOn != "new" {
		return fmt.Errorf("unknown --fail-on %q (any or new)", o.failOn)
	}
	switch o.color {
	case "auto", "always", "never":
	default:
		return fmt.Errorf("unknown --color %q (auto, always or never)", o.color)
	}
	if o.watch {
		return runWatch(cmd, args, o)
	}

	errorViolations, err := runCheckOnce(cmd, args, o)
	if err != nil {
		return err
	}
	if errorViolations > 0 {
		// The report already told the story; exit 1 without an
		// error banner so CI output stays clean.
		os.Exit(1)
	}
	return nil
}

// runCheckOnce runs one check pipeline — evaluate, baseline-filter, report — and
// returns the number of error-severity violations (the exit-code count). It never
// calls os.Exit, so both the one-shot path and the watch loop can drive it.
func runCheckOnce(cmd *cobra.Command, args []string, o checkOptions) (int, error) {
	start := time.Now()

	run, err := evaluateCheckTargets(cmd, o, args)
	if err != nil {
		return 0, err
	}

	// Per-member baseline filtering for --fail-on new: each member's
	// baseline sits next to its own config.
	if o.failOn == "new" {
		for i := range run.Members {
			m := &run.Members[i]
			if m.Eval == nil {
				continue
			}
			base, err := config.LoadBaselineOrEmpty(filepath.Join(filepath.Dir(m.Eval.ConfigPath), config.BaselineName))
			if err != nil {
				return 0, err
			}
			m.Fixed = base.Fixed(m.Eval.Result)
			m.Eval.Result, m.Suppressed = base.Filter(m.Eval.Result)
		}
	}
	elapsed := time.Since(start)

	violations, err := reportCheck(cmd, run, o.format, o.color, elapsed)
	if err != nil {
		return 0, err
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

	return violations, nil
}
