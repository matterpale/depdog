package cli

import (
	"context"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lsp"
)

func lspCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "LSP server over stdio for inline architecture diagnostics",
		Long: `lsp speaks the Language Server Protocol on stdin/stdout so any LSP-capable
editor can show depdog's rule violations as inline diagnostics (squiggles).

The server runs one check when the editor finishes initializing and publishes
every violation at its import statement's file and line. Re-checking on save
is a coming increment — restart the session to re-check for now (design and
roadmap: docs/lsp.md).

Wire it into an editor as the command ` + "`depdog lsp`" + ` for your project's language;
all diagnostics carry source "depdog". Logs go to stderr, never stdout.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The check closure hands internal/lsp everything it needs while
			// keeping config discovery and adapter selection here in cli.
			check := func(ctx context.Context) (*core.Result, string, error) {
				ev, err := evaluateModule(cmd, configPath, nil)
				if err != nil {
					return nil, "", err
				}
				return ev.Result, filepath.Dir(ev.ConfigPath), nil
			}
			srv := lsp.NewServer(check, Version)
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to the project marker)")
	return cmd
}
