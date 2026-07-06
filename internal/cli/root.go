// Package cli assembles depdog's command tree.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is stamped by the release build; the default marks dev builds.
var Version = "0.0.0-dev"

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:     "depdog",
		Short:   "Keep a project's internal dependencies pointing in the right direction",
		Long: `depdog checks a codebase's import edges against the architecture rules
declared in depdog.yaml: which components exist, and who may import whom.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bare `depdog` will open the TUI (milestone M4); guide until then.
			fmt.Fprintln(cmd.OutOrStdout(), "The depdog TUI is not built yet (planned: milestone M4).")
			fmt.Fprintln(cmd.OutOrStdout(), "Run `depdog check` to verify import rules, or `depdog --help` for everything else.")
			return nil
		},
	}
	root.AddCommand(checkCmd())
	return root
}
