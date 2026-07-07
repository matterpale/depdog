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
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/wizard"
)

func initCmd() *cobra.Command {
	var (
		presetName string
		policy     string
		assumeYes  bool
		force      bool
		merge      bool
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

When a depdog.yaml already exists, init refuses to touch it unless you pass
--force (overwrite it) or --merge (keep it). --merge rescans the module and
appends a component — and, under policy deny, a starter rule — for every
directory no existing component pattern covers, editing the file in place
without disturbing its comments, ordering or formatting. It lists what it
would add and asks for confirmation; --yes applies the additions
non-interactively. When everything is already covered it changes nothing.

Exit codes: 0 written (or nothing to merge), 2 configuration or usage error.`,
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
			if merge {
				if force {
					return errors.New("--merge and --force conflict — merge edits the existing file, force overwrites it; pick one")
				}
				if presetName != "" || policy != "" {
					return errors.New("drop --preset and --policy — they have no effect with --merge; the existing file keeps its layout and policy")
				}
				if !assumeYes && !isInteractive(cmd) {
					return errors.New("depdog init --merge needs an interactive terminal; pass --yes to apply the additions non-interactively")
				}
				return runMerge(cmd, root, dest, !assumeYes)
			}
			if _, err := os.Stat(dest); err == nil && !force {
				return fmt.Errorf("%s already exists — rerun with --force to overwrite it, or --merge to add newly-scanned components to it", dest)
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
	cmd.Flags().BoolVar(&merge, "merge", false, "add components for uncovered directories to an existing depdog.yaml, keeping its comments and formatting")
	cmd.Flags().StringVar(&outPath, "config", "", "path to write (default: depdog.yaml next to go.mod)")
	return cmd
}

// runMerge implements `init --merge`: rescan the module, propose a component
// (plus, under policy deny, a starter rule) for every directory no existing
// component pattern covers, and splice them into the existing file via
// config.MergeComponents — a yaml.Node-located edit that leaves the user's
// comments, ordering and formatting untouched. The merged bytes must pass
// config.Parse before anything is written; when nothing is uncovered the file
// is left alone.
func runMerge(cmd *cobra.Command, root, dest string, interactive bool) error {
	data, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s does not exist — run `depdog init` without --merge to create it", dest)
		}
		return err
	}
	rs, err := config.Parse(data)
	if err != nil {
		return fmt.Errorf("%s does not validate — fix it before merging: %w", relTo(root, dest), err)
	}
	taken, err := config.DeclaredNames(data)
	if err != nil {
		return err
	}
	scan, err := wizard.ScanModule(root)
	if err != nil {
		return err
	}

	existing := make([]wizard.Component, len(rs.Components))
	for i, c := range rs.Components {
		existing[i] = wizard.Component{Name: c.Name, Patterns: c.Patterns}
	}
	policy := wizard.PolicyDeny
	if rs.Policy == core.PolicyAllow {
		policy = wizard.PolicyAllow
	}

	proposed := wizard.ProposeMissing(existing, taken, scan, policy)
	out := cmd.OutOrStdout()
	if len(proposed) == 0 {
		fmt.Fprintf(out, "Nothing to merge — every scanned directory is already covered by %s.\n", relTo(root, dest))
		return nil
	}

	add := make([]config.MergeComponent, len(proposed))
	names := make([]string, len(proposed))
	for i, c := range proposed {
		add[i] = config.MergeComponent{
			Name:     c.Name,
			Patterns: c.Patterns,
			Comment:  c.Comment,
			Rule:     wizard.RuleBody(c, policy),
		}
		names[i] = c.Name
	}

	merged, err := config.MergeComponents(data, add)
	if err != nil {
		return err
	}
	// Never write a file the checker would reject; the original stays intact.
	if _, err := config.Parse(merged); err != nil {
		return fmt.Errorf("internal error: merged config did not validate (%s left untouched): %w", relTo(root, dest), err)
	}

	if interactive {
		fmt.Fprintf(out, "New components for directories no pattern covers:\n")
		for _, c := range proposed {
			fmt.Fprintf(out, "  + %s  %s\n", c.Name, strings.Join(c.Patterns, ", "))
		}
		ok, err := confirmWrite(cmd, dest, merged)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Aborted — nothing written.")
			return nil
		}
	}

	if err := os.WriteFile(dest, merged, 0o644); err != nil {
		return err
	}
	noun := "components"
	if len(names) == 1 {
		noun = "component"
	}
	fmt.Fprintf(out, "Added %d %s to %s: %s.\n", len(names), noun, relTo(root, dest), strings.Join(names, ", "))
	fmt.Fprintln(out, "Review the additions, then run `depdog check`.")
	return nil
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

// reviewComponents lets the user drop suggested components, then rename the
// kept ones or tweak their patterns, before writing.
func reviewComponents(cmd *cobra.Command, cfg wizard.Config) (wizard.Config, error) {
	if len(cfg.Components) > 1 {
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
		cfg = cfg.Keep(selected)
	}
	return editComponents(cmd, cfg)
}

// editComponents offers an optional edit pass over the kept components: pick
// one, change its name and patterns (validated as you type), repeat until
// done. Rule refs to a renamed component are rewritten by wizard's Rename.
func editComponents(cmd *cobra.Command, cfg wizard.Config) (wizard.Config, error) {
	out := cmd.OutOrStdout()
	for {
		const done = ""
		opts := make([]huh.Option[string], 0, len(cfg.Components)+1)
		opts = append(opts, huh.NewOption("no — continue", done))
		for _, c := range cfg.Components {
			opts = append(opts, huh.NewOption(c.Name+"  "+strings.Join(c.Patterns, ", "), c.Name))
		}
		pick := done
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Edit a component?").
				Description("Pick a component to rename or re-pattern, or continue.").
				Options(opts...).
				Value(&pick),
		)).WithOutput(out)
		if err := form.Run(); err != nil {
			return cfg, err
		}
		if pick == done {
			return cfg, nil
		}

		cur, _ := cfg.Component(pick)
		name := cur.Name
		patterns := strings.Join(cur.Patterns, ", ")
		form = huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Name").
				Description("The component's name; rules that mention it follow the rename.").
				Validate(func(s string) error {
					return cfg.ValidateRename(pick, strings.TrimSpace(s))
				}).
				Value(&name),
			huh.NewInput().
				Title("Patterns").
				Description("Comma-separated module-relative globs, e.g. internal/api/**").
				Validate(func(s string) error {
					_, err := wizard.ParsePatterns(s)
					return err
				}).
				Value(&patterns),
		)).WithOutput(out)
		if err := form.Run(); err != nil {
			return cfg, err
		}

		pats, err := wizard.ParsePatterns(patterns)
		if err != nil {
			return cfg, err
		}
		if cfg, err = cfg.SetPatterns(pick, pats); err != nil {
			return cfg, err
		}
		if cfg, err = cfg.Rename(pick, strings.TrimSpace(name)); err != nil {
			return cfg, err
		}
	}
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
