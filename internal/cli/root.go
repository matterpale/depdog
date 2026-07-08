// Package cli assembles depdog's command tree.
package cli

import (
	"github.com/spf13/cobra"
)

// Version is stamped by the release build; the default marks dev builds.
var Version = "0.0.0-dev"

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "depdog",
		Short: "Keep a project's internal dependencies pointing in the right direction",
		Long: `depdog checks a codebase's import edges against the architecture rules
declared in depdog.yaml: which components exist, and who may import whom.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bare `depdog` opens the TUI on a terminal, or guides otherwise.
			return runBare(cmd)
		},
	}
	// --lang selects the language adapter for every subcommand; empty means
	// auto-detect by marker file (go.mod ⇒ go, tsconfig/package.json ⇒ ts).
	root.PersistentFlags().String("lang", "", "language adapter: go or ts (default: auto-detect by go.mod / tsconfig.json)")
	root.AddCommand(initCmd())
	root.AddCommand(checkCmd())
	root.AddCommand(configCmd())
	root.AddCommand(baselineCmd())
	root.AddCommand(graphCmd())
	root.AddCommand(explainCmd())
	root.AddCommand(tuiCmd())
	return root
}
