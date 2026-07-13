package cli

import (
	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/mcp"
)

func mcpCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server over stdio for in-loop architecture queries",
		Long: `mcp speaks the Model Context Protocol on stdin/stdout so any MCP-capable
agent (Claude, Cursor, …) can consult the architecture in the loop, not just
as a post-hoc CI gate. It is read-only: no rule mutation over MCP.

The server exposes tools — ` + "`check`" + ` (violations as JSON), ` + "`explain`" + ` and
` + "`can_import`" + ` (per-edge verdicts) — and resources ` + "`depdog://config`" + ` and
` + "`depdog://components`" + `. It is a thin protocol adapter over capability that
already exists (the rule set + the JSON reporter + the check entry points;
design and roadmap: docs/mcp.md).

Wire it into an agent as the command ` + "`depdog mcp`" + ` over stdio. Logs go to
stderr, never stdout.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// M1 ships the protocol shell: a nil handler makes tools/call and
			// resources/read answer "not wired yet" while the handshake and
			// catalogue work. M2 injects the real closures here — config
			// discovery, adapter selection, evaluation and JSON rendering stay
			// in cli, so internal/mcp remains a pure protocol layer.
			_ = configPath
			srv := mcp.NewServer(nil, Version)
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to the project marker)")
	return cmd
}
