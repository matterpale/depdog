package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/tui"
	"github.com/matterpale/depdog/internal/wizard"
)

func tuiCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "tui [packages]",
		Short: "Explore the check result in an interactive terminal UI",
		Long: `tui runs the check and opens an interactive view: a component summary
dashboard and a browsable list of violations.

Exit codes: 0 on quit, 2 configuration or usage error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isInteractive(cmd) {
				return errors.New("depdog tui needs an interactive terminal; use `depdog check` for non-interactive output")
			}
			ev, err := evaluateModule(cmd, configPath, args)
			if err != nil {
				return err
			}
			return launch(cmd, configPath, args, ev)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	return cmd
}

// launch builds the package-navigation views and opens the UI, wiring in the
// module root (so `e` can open module-relative positions in $EDITOR), the
// config path and compiled rule set (for the Config tab), and a refresh hook
// that re-runs the same load+check pipeline for `r`. The hook hands back the
// recompiled rule set too, since core.Result does not carry it.
func launch(cmd *cobra.Command, configPath string, args []string, ev *evaluation) error {
	pkgs, err := core.BuildPackageViews(ev.Graph, ev.Rules)
	if err != nil {
		return err
	}
	root := filepath.Dir(ev.ConfigPath)
	configRel := configRelPath(root, ev.ConfigPath)
	refresh := func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
		ev, err := evaluateModule(cmd, configPath, args)
		if err != nil {
			return nil, nil, nil, err
		}
		pkgs, err := core.BuildPackageViews(ev.Graph, ev.Rules)
		if err != nil {
			return nil, nil, nil, err
		}
		return ev.Result, pkgs, ev.Rules, nil
	}
	// edit backs the Matrix tab's cell toggles: rewrite one component's
	// allow/deny in depdog.yaml (comment-preserving), leaving the caller to
	// re-run the check via the refresh hook so every screen reflects the change.
	edit := func(from, target, verdict string) error {
		data, err := os.ReadFile(ev.ConfigPath)
		if err != nil {
			return err
		}
		out, err := config.SetComponentRule(data, from, target, verdict)
		if err != nil {
			return err
		}
		return os.WriteFile(ev.ConfigPath, out, 0o644)
	}
	// addComponent backs the Matrix tab's "add component" form: validate the name
	// and pattern the same way `init` does, then splice a new component into
	// depdog.yaml via MergeComponents (which preserves the rest of the file and
	// rejects name collisions).
	addComponent := func(name, pattern string) error {
		if err := wizard.ValidateName(name); err != nil {
			return err
		}
		if err := core.ValidatePattern(pattern); err != nil {
			return err
		}
		data, err := os.ReadFile(ev.ConfigPath)
		if err != nil {
			return err
		}
		out, err := config.MergeComponents(data, []config.MergeComponent{{Name: name, Patterns: []string{pattern}}})
		if err != nil {
			return err
		}
		return os.WriteFile(ev.ConfigPath, out, 0o644)
	}
	// repath backs the Matrix tab's re-path form: validate each glob (as `init`
	// does) then rewrite the component's path in depdog.yaml, comment-preserving.
	repath := func(component string, patterns []string) error {
		for _, p := range patterns {
			if err := core.ValidatePattern(p); err != nil {
				return err
			}
		}
		data, err := os.ReadFile(ev.ConfigPath)
		if err != nil {
			return err
		}
		out, err := config.SetComponentPath(data, component, patterns)
		if err != nil {
			return err
		}
		return os.WriteFile(ev.ConfigPath, out, 0o644)
	}
	// rename backs the Matrix tab's rename form: validate the new name (as `init`
	// does) then rename the component and every reference to it in depdog.yaml.
	rename := func(oldName, newName string) error {
		if err := wizard.ValidateName(newName); err != nil {
			return err
		}
		data, err := os.ReadFile(ev.ConfigPath)
		if err != nil {
			return err
		}
		out, err := config.RenameComponent(data, oldName, newName)
		if err != nil {
			return err
		}
		return os.WriteFile(ev.ConfigPath, out, 0o644)
	}
	// boundary membership: add/remove a member from a boundary in depdog.yaml.
	writeBoundary := func(fn func([]byte, string, string) ([]byte, error)) func(string, string) error {
		return func(boundary, member string) error {
			data, err := os.ReadFile(ev.ConfigPath)
			if err != nil {
				return err
			}
			out, err := fn(data, boundary, member)
			if err != nil {
				return err
			}
			return os.WriteFile(ev.ConfigPath, out, 0o644)
		}
	}
	return tui.Run(ev.Result, pkgs,
		tui.WithRoot(root),
		tui.WithConfig(configRel, ev.Rules),
		tui.WithRefresh(refresh),
		tui.WithEdit(edit),
		tui.WithAddComponent(addComponent),
		tui.WithRepath(repath),
		tui.WithRename(rename),
		tui.WithBoundaryMembers(
			writeBoundary(config.AddBoundaryMember),
			writeBoundary(config.RemoveBoundaryMember)))
}

// configRelPath renders the config path relative to the module root for display,
// falling back to the base name when it lies outside the root.
func configRelPath(root, configPath string) string {
	if rel, err := filepath.Rel(root, configPath); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(configPath)
}

// runBare backs a plain `depdog` invocation: it opens the TUI when a terminal
// and a config are both present, and otherwise prints guidance. It never errors
// on a missing config — a first-time user gets pointed at `init` instead.
func runBare(cmd *cobra.Command) error {
	if isInteractive(cmd) {
		if cwd, err := os.Getwd(); err == nil {
			language, lerr := languageFlag(cmd)
			if lerr == nil {
				if _, _, _, ferr := resolveProject(cwd, language); ferr == nil {
					ev, eerr := evaluateModule(cmd, "", nil)
					if eerr != nil {
						return eerr
					}
					return launch(cmd, "", nil, ev)
				}
			}
		}
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "depdog keeps a project's internal imports pointing the right way.")
	fmt.Fprintln(out, "Run `depdog init` to create a depdog.yaml, `depdog check` to verify import rules,")
	fmt.Fprintln(out, "or `depdog tui` for the interactive view. `depdog --help` lists everything.")
	return nil
}
