package cli

import (
	"context"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/lsp"
)

func lspCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "LSP server over stdio for inline architecture diagnostics",
		Long: `lsp speaks the Language Server Protocol on stdin/stdout so any LSP-capable
editor can show depdog's rule violations as inline diagnostics (squiggles).

The server runs one check when the editor finishes initializing and again on
every save, publishing every violation at its import statement's file and line
and clearing diagnostics from files that went clean. Hovering an import line
shows the ` + "`depdog explain`" + ` verdict for that edge — the rule or boundary that
allows or denies it (design and roadmap: docs/lsp.md).

Wire it into an editor as the command ` + "`depdog lsp`" + ` for your project's language;
all diagnostics carry source "depdog". Logs go to stderr, never stdout.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The check closure hands internal/lsp everything it needs while
			// keeping config discovery and adapter selection here in cli. The
			// full snapshot (result + graph + rules) lets diagnostics and
			// hover describe the same check round.
			check := func(ctx context.Context) (*lsp.Check, error) {
				ev, err := evaluateModule(cmd, configPath, nil)
				if err != nil {
					return nil, err
				}
				return &lsp.Check{
					Result: ev.Result,
					Graph:  ev.Graph,
					Rules:  ev.Rules,
					Root:   filepath.Dir(ev.ConfigPath),
				}, nil
			}
			srv := lsp.NewServer(check, Version)
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to the project marker)")
	return cmd
}
