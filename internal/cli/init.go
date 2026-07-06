package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/wizard"
)

func initCmd() *cobra.Command {
	var (
		presetName string
		policy     string
		assumeYes  bool
		force      bool
		outPath    string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a depdog.yaml for this module",
		Long: `init inspects the module layout, matches it against an architecture
preset (ddd, hexagonal, layered or flat) and writes a starter depdog.yaml.

Run it with no flags for an interactive wizard, or with --yes to accept the
suggestion non-interactively (for scripts and CI bootstrapping). --preset and
--policy pin those choices; without them --yes defaults to ddd + deny.

Exit codes: 0 written, 2 configuration or usage error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := config.ModuleRoot(cwd)
			if err != nil {
				return err
			}

			dest := outPath
			if dest == "" {
				dest = filepath.Join(root, config.DefaultName)
			} else if dest, err = filepath.Abs(dest); err != nil {
				return err
			}
			if _, err := os.Stat(dest); err == nil && !force {
				return fmt.Errorf("%s already exists — rerun with --force to overwrite it", dest)
			}

			// Reject bad flag values before scanning so the message is prompt.
			if presetName != "" {
				if _, err := wizard.PresetByName(presetName); err != nil {
					return err
				}
			}
			if policy != "" && policy != wizard.PolicyDeny && policy != wizard.PolicyAllow {
				return fmt.Errorf("invalid --policy %q — use %q (whitelist) or %q (blacklist)",
					policy, wizard.PolicyDeny, wizard.PolicyAllow)
			}

			interactive := !assumeYes
			if interactive && !isInteractive(cmd) {
				return errors.New("depdog init needs an interactive terminal; pass --yes to accept the suggested config (optionally with --preset and --policy)")
			}

			scan, err := wizard.ScanModule(root)
			if err != nil {
				return err
			}

			chosenPreset, chosenPolicy := presetName, policy
			if chosenPreset == "" {
				chosenPreset = "ddd"
			}
			if chosenPolicy == "" {
				chosenPolicy = wizard.PolicyDeny
			}
			if interactive {
				if chosenPreset, chosenPolicy, err = askPresetPolicy(cmd,
					chosenPreset, chosenPolicy, presetName == "", policy == ""); err != nil {
					return err
				}
			}

			preset, err := wizard.PresetByName(chosenPreset)
			if err != nil {
				return err
			}
			cfg := wizard.Suggest(preset, scan, chosenPolicy)

			if interactive {
				if cfg, err = reviewComponents(cmd, cfg); err != nil {
					return err
				}
			}

			data, err := cfg.Marshal()
			if err != nil {
				return err
			}
			// Never write a file the checker would reject.
			if _, err := config.Parse(data); err != nil {
				return fmt.Errorf("internal error: generated config did not validate: %w", err)
			}

			if interactive {
				ok, err := confirmWrite(cmd, dest, data)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted — nothing written.")
					return nil
				}
			}

			if err := os.WriteFile(dest, data, 0o644); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Wrote %s — %d components, policy: %s.\n", relTo(root, dest), len(cfg.Components), cfg.Policy)
			fmt.Fprintln(out, "Review it, then run `depdog check`.")
			return nil
		},
	}
	cmd.Flags().StringVar(&presetName, "preset", "", "architecture preset: "+strings.Join(wizard.PresetNames(), ", "))
	cmd.Flags().StringVar(&policy, "policy", "", "rule stance: deny (whitelist) or allow (blacklist)")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "accept the suggestion without prompting")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing depdog.yaml")
	cmd.Flags().StringVar(&outPath, "config", "", "path to write (default: depdog.yaml next to go.mod)")
	return cmd
}

// isInteractive reports whether stdin is a terminal we can drive huh with.
func isInteractive(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}

// askPresetPolicy prompts only for the choices not already pinned by a flag.
func askPresetPolicy(cmd *cobra.Command, preset, policy string, askPreset, askPolicy bool) (string, string, error) {
	var groups []*huh.Group
	if askPreset {
		opts := make([]huh.Option[string], 0, len(wizard.Presets()))
		for _, p := range wizard.Presets() {
			opts = append(opts, huh.NewOption(p.Name+" — "+p.Description, p.Name))
		}
		groups = append(groups, huh.NewGroup(
			huh.NewSelect[string]().
				Title("Architecture preset").
				Description("The starting layer layout; depdog matches it against your directories.").
				Options(opts...).
				Value(&preset),
		))
	}
	if askPolicy {
		groups = append(groups, huh.NewGroup(
			huh.NewSelect[string]().
				Title("Rule stance").
				Description("What happens to an import edge no rule mentions.").
				Options(
					huh.NewOption("deny — whitelist: only imports you allow pass (strict)", wizard.PolicyDeny),
					huh.NewOption("allow — blacklist: everything passes except what you deny", wizard.PolicyAllow),
				).
				Value(&policy),
		))
	}
	if len(groups) == 0 {
		return preset, policy, nil
	}
	if err := huh.NewForm(groups...).WithOutput(cmd.OutOrStdout()).Run(); err != nil {
		return "", "", err
	}
	return preset, policy, nil
}

// reviewComponents lets the user drop suggested components before writing.
func reviewComponents(cmd *cobra.Command, cfg wizard.Config) (wizard.Config, error) {
	if len(cfg.Components) <= 1 {
		return cfg, nil
	}
	opts := make([]huh.Option[string], len(cfg.Components))
	selected := make([]string, 0, len(cfg.Components))
	for i, c := range cfg.Components {
		opts[i] = huh.NewOption(c.Name+"  "+strings.Join(c.Patterns, ", "), c.Name).Selected(true)
		selected = append(selected, c.Name)
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Components to include").
			Description("Space toggles a component; unchecked ones are left out. You can still edit the file afterward.").
			Options(opts...).
			Validate(func(sel []string) error {
				if len(sel) == 0 {
					return errors.New("keep at least one component")
				}
				return nil
			}).
			Value(&selected),
	)).WithOutput(cmd.OutOrStdout())
	if err := form.Run(); err != nil {
		return cfg, err
	}
	return cfg.Keep(selected), nil
}

// confirmWrite previews the generated file and asks for the go-ahead.
func confirmWrite(cmd *cobra.Command, dest string, data []byte) (bool, error) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n%s\n", data)
	var ok bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Write " + dest + "?").
			Affirmative("Write").
			Negative("Cancel").
			Value(&ok),
	)).WithOutput(out)
	if err := form.Run(); err != nil {
		return false, err
	}
	return ok, nil
}

func relTo(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}
