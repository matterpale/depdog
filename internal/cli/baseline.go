package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
)

func baselineCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "baseline [packages]",
		Short: "Record current violations as a tolerated baseline",
		Long: `baseline runs the check and writes every current violation to
depdog.baseline.yaml. With that file present, ` + "`depdog check --fail-on new`" + ` only
fails on violations that are not already recorded — a ratchet for adopting
rules on a codebase that does not pass yet. Shrink the file over time.

Exit codes: 0 written, 2 configuration or usage error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, cfgPath, err := evaluateModule(cmd, configPath, args)
			if err != nil {
				return err
			}
			b := core.BaselineFrom(res)

			path := filepath.Join(filepath.Dir(cfgPath), config.BaselineName)
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			if err := config.WriteBaseline(f, b); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Wrote %s — %d violations recorded.\n", config.BaselineName, len(b.Entries))
			fmt.Fprintln(out, "Run `depdog check --fail-on new` to fail only on new violations.")
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	return cmd
}
