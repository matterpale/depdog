// Package cli assembles depdog's command tree.
package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// Version is stamped by the release build; the default marks dev builds.
var Version = "0.0.0-dev"

func Root() *cobra.Command {
	// Bare `depdog` runs the check, so it takes the same flags as `depdog check`.
	var bare checkOptions
	root := &cobra.Command{
		Use:   "depdog [packages]",
		Short: "Keep a project's internal dependencies pointing in the right direction",
		Long: `depdog checks a codebase's import edges against the architecture rules
declared in depdog.yaml: which components exist, and who may import whom.

Run bare, depdog evaluates the check (like ` + "`depdog check`" + `) and exits
0 clean / 1 on violations. ` + "`depdog tui`" + ` opens the interactive view.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: false,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBare(cmd, args, bare)
		},
	}
	bare.bind(root)
	// --lang selects the language adapter for every subcommand; empty means
	// auto-detect from each adapter's marker files (see the languages registry).
	root.PersistentFlags().String("lang", "",
		"language adapter: "+strings.Join(languageNames(), " or ")+" (default: auto-detect from marker files)")
	root.AddCommand(initCmd())
	root.AddCommand(checkCmd())
	root.AddCommand(configCmd())
	root.AddCommand(baselineCmd())
	root.AddCommand(graphCmd())
	root.AddCommand(explainCmd())
	root.AddCommand(lspCmd())
	root.AddCommand(mcpCmd())
	root.AddCommand(tuiCmd())
	return root
}
