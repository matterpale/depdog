package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
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
and clearing diagnostics from files that went clean. Editors that support
client-side file watching also trigger a re-check when depdog.yaml changes
outside the editor (git checkout, terminal edits). Hovering an import line
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
			check := func(ctx context.Context, path string) (*lsp.Check, error) {
				ev, rel, err := evaluateForLSP(cmd, configPath, path)
				if err != nil {
					return nil, err
				}
				return &lsp.Check{
					Result: ev.Result,
					Graph:  ev.Graph,
					Rules:  ev.Rules,
					Root:   filepath.Dir(ev.ConfigPath),
					Rel:    rel,
				}, nil
			}
			// The watcher registration needs the config basename before the
			// first check runs: an explicit --config names it directly, and
			// discovery only ever resolves the default name.
			configBase := config.DefaultName
			if configPath != "" {
				configBase = filepath.Base(configPath)
			}
			srv := lsp.NewServer(check, Version, configBase)
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to the project marker)")
	return cmd
}

// evaluateForLSP resolves the module the LSP should check for the file that
// triggered a round, returning the evaluation and the checked module's
// workspace-relative dir ("" outside a workspace). An explicit --config pins
// one module, workspaces ignored, exactly as on the CLI. Otherwise, inside a Go
// workspace the triggering file's owning member is checked — an edit in ./app
// checks app, not the workspace root — and its relative dir lets the server
// rebase diagnostic paths onto the client's root. With no workspace it falls
// back to classic single-module discovery from the file's directory (or the
// process working directory when no file named the round, e.g. initialize).
func evaluateForLSP(cmd *cobra.Command, configPath, triggerPath string) (*evaluation, string, error) {
	if configPath != "" {
		ev, err := evaluateModule(cmd, configPath, nil)
		return ev, "", err
	}
	startDir, err := lspStartDir(triggerPath)
	if err != nil {
		return nil, "", err
	}
	ws, err := config.FindWorkspace(startDir)
	if err != nil {
		return nil, "", err
	}
	if ws != nil {
		dir, cfgPath, rel, err := lspWorkspaceTarget(ws, startDir)
		if err != nil {
			return nil, "", err
		}
		adapter, ok := adapterByName("go")
		if !ok {
			return nil, "", fmt.Errorf("internal error: go adapter not registered")
		}
		ev, err := evaluateAt(cmd, adapter, dir, cfgPath, nil)
		return ev, rel, err
	}
	ev, err := evaluateDiscovered(cmd, startDir)
	return ev, "", err
}

// lspWorkspaceTarget resolves the workspace member that owns startDir: its
// directory, its depdog.yaml path, and its workspace-relative dir (for rebasing
// diagnostics). It errors when startDir lies outside every member or the owning
// member has no config. Pure (no project loading), so it is unit-testable.
func lspWorkspaceTarget(ws *config.Workspace, startDir string) (dir, cfgPath, rel string, err error) {
	dir, ok := ws.OwningModule(startDir)
	if !ok {
		return "", "", "", fmt.Errorf("no workspace member owns %s — open a file inside a member module to check it", startDir)
	}
	cfgPath = filepath.Join(dir, config.DefaultName)
	if !fileExists(cfgPath) {
		return "", "", "", fmt.Errorf("workspace member ./%s has no %s", relSlash(ws.Dir, dir), config.DefaultName)
	}
	return dir, cfgPath, relSlash(ws.Dir, dir), nil
}

// lspStartDir is the directory the LSP resolves a check from: the folder of the
// file the client named (a didSave / didChangeWatchedFiles URI), else the
// process working directory (the initialize round, before any file is named).
func lspStartDir(triggerPath string) (string, error) {
	if triggerPath == "" {
		return os.Getwd()
	}
	abs, err := filepath.Abs(triggerPath)
	if err != nil {
		return "", err
	}
	return filepath.Dir(abs), nil
}
