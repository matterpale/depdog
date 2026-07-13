package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/report"
)

// memberEval is one member of a check run: its evaluation, or — when Eval is
// nil — a note that it was skipped for lack of a depdog.yaml. Suppressed and
// Fixed carry per-member baseline bookkeeping for --fail-on new.
type memberEval struct {
	Rel        string // workspace-relative dir ("app"); "" for a single module
	Lang       string // the adapter that checked this member ("go", "ts", …)
	Eval       *evaluation
	SkipReason string
	Suppressed int
	Fixed      []core.BaselineEntry
}

// checkRun is what `check` resolved to evaluate. Workspace is nil for a plain
// single-module run (Members holds exactly one analyzed entry) and non-nil for
// a go.work fan-out. Root is the walk-root's basename (the go.work dir for a
// workspace fan-out, the cwd for a polyglot --all/fallback run), labelling the
// JSON envelope's "root" field without leaking an absolute path; it is empty for
// a single-module run.
type checkRun struct {
	Workspace *config.Workspace
	Root      string
	Members   []memberEval
}

func (r *checkRun) hasAnalyzed() bool {
	for _, m := range r.Members {
		if m.Eval != nil {
			return true
		}
	}
	return false
}

// split partitions the members into the analyzed modules and the skipped ones,
// in the shapes the aggregate reporters consume.
func (r *checkRun) split() (mods []report.Module, skipped []report.Skipped) {
	for _, m := range r.Members {
		if m.Eval == nil {
			skipped = append(skipped, report.Skipped{Rel: m.Rel, Reason: m.SkipReason})
			continue
		}
		mods = append(mods, report.Module{
			Path:   m.Eval.Result.ModulePath,
			Rel:    m.Rel,
			Lang:   m.Lang,
			Result: m.Eval.Result,
			Rules:  m.Eval.Rules,
		})
	}
	return mods, skipped
}

// evaluateCheckTargets resolves what `check` should evaluate, keeping today's
// order and adding the polyglot branch plus the discovery fallback:
//
//	[a] --config           → single module (unchanged).
//	[b] --all              → polyglot fan-out over discovered units (D1).
//	[c] active go.work     → workspace fan-out (unchanged).
//	[d] single module; and ONLY on a resolution error (no project root / no
//	    depdog.yaml — not a parse failure or a violation) fall back to unit
//	    discovery under the cwd. ≥1 unit → polyglot fan-out; else the original
//	    resolution error, unchanged (D1 fallback).
func evaluateCheckTargets(cmd *cobra.Command, o checkOptions, args []string) (*checkRun, error) {
	if err := checkFlagConflicts(cmd, o, args); err != nil {
		return nil, err
	}

	// [a] --config → single module, unchanged.
	if o.configPath != "" {
		return singleRun(cmd, o.configPath, args)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	// [b] --all → explicit polyglot fan-out.
	if o.all {
		return evaluateUnits(cmd, cwd, o.units, args, true)
	}

	// [c] active go.work → workspace fan-out, unchanged.
	ws, err := config.FindWorkspace(cwd)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		// [d] single module, with the discovery fallback on a resolution error.
		run, err := singleRun(cmd, o.configPath, args)
		if err == nil {
			return run, nil
		}
		if errors.Is(err, errResolution) {
			if fb, ok, fbErr := fallbackUnits(cmd, cwd, args); fbErr != nil {
				return nil, fbErr
			} else if ok {
				return fb, nil
			}
		}
		return nil, err
	}
	return evaluateWorkspace(cmd, ws, o.modules, args)
}

// checkFlagConflicts rejects the flag combinations the polyglot mode outlaws
// (D7/D8), each an exit-2 usage error.
func checkFlagConflicts(cmd *cobra.Command, o checkOptions, args []string) error {
	if o.configPath != "" {
		if len(o.modules) > 0 {
			return fmt.Errorf("--module cannot be combined with --config")
		}
		if o.all || len(o.units) > 0 {
			return fmt.Errorf("--config cannot be combined with --all or --unit")
		}
	}
	if o.all {
		if lang, _ := cmd.Flags().GetString("lang"); lang != "" {
			return fmt.Errorf("--lang cannot be combined with --all: units auto-detect their language; pin one unit with `lang:` in its depdog.yaml")
		}
		if len(o.modules) > 0 {
			return fmt.Errorf("--module cannot be combined with --all (--module is go.work-only; use --unit to narrow an --all run)")
		}
		if len(args) > 0 {
			return fmt.Errorf("a [packages] filter has no cross-unit meaning under --all: use --unit to narrow an --all run")
		}
	}
	if !o.all && len(o.units) > 0 {
		return fmt.Errorf("--unit only applies with --all")
	}
	return nil
}

// fallbackUnits runs unit discovery under cwd for the D1 fallback: a bare
// `depdog check` whose single-project resolution errored. It returns (run,
// true, nil) when ≥1 unit is discovered, (nil, false, nil) when none is (the
// caller then surfaces the original resolution error), or an error if the
// discovery evaluation itself fails.
func fallbackUnits(cmd *cobra.Command, cwd string, args []string) (*checkRun, bool, error) {
	units, ungoverned, err := config.DiscoverUnits(cwd, registryMarkers())
	if err != nil {
		return nil, false, err
	}
	if len(units) == 0 {
		return nil, false, nil
	}
	run, err := runUnits(cmd, cwd, units, ungoverned, nil, args)
	if err != nil {
		return nil, false, err
	}
	return run, true, nil
}

func singleRun(cmd *cobra.Command, configPath string, args []string) (*checkRun, error) {
	ev, err := evaluateModule(cmd, configPath, args)
	if err != nil {
		return nil, err
	}
	return &checkRun{Members: []memberEval{{Eval: ev}}}, nil
}

// evaluateWorkspace fans out over the selected workspace members, evaluating
// each configured one and advisory-skipping those without a depdog.yaml.
func evaluateWorkspace(cmd *cobra.Command, ws *config.Workspace, modules, args []string) (*checkRun, error) {
	adapter, ok := adapterByName("go")
	if !ok {
		return nil, fmt.Errorf("internal error: go adapter not registered")
	}
	dirs, err := selectMembers(ws, modules)
	if err != nil {
		return nil, err
	}
	run := &checkRun{Workspace: ws, Root: filepath.Base(ws.Dir)}
	for _, dir := range dirs {
		rel := relSlash(ws.Dir, dir)
		cfgPath := filepath.Join(dir, config.DefaultName)
		if !fileExists(cfgPath) {
			run.Members = append(run.Members, memberEval{Rel: rel, SkipReason: "no " + config.DefaultName})
			continue
		}
		ev, err := evaluateAt(cmd, adapter, dir, cfgPath, args)
		if err != nil {
			return nil, fmt.Errorf("checking ./%s: %w", rel, err)
		}
		run.Members = append(run.Members, memberEval{Rel: rel, Lang: adapter.Name, Eval: ev})
	}
	if !run.hasAnalyzed() {
		return nil, fmt.Errorf("no workspace member has a %s — run `depdog init` in the members you want checked", config.DefaultName)
	}
	return run, nil
}

// evaluateUnits is the polyglot fan-out: discover every depdog.yaml under cwd,
// optionally narrow by --unit, and evaluate each with its own adapter (its
// `lang:` key or auto-detection). It mirrors evaluateWorkspace's shape with two
// substitutions — the member list comes from DiscoverUnits (not go.work) and
// the adapter is per-unit (not the hardcoded go). explicit distinguishes an
// explicit --all (empty discovery is a usage error naming `depdog init`) from
// the fallback path, which never reaches here with zero units.
func evaluateUnits(cmd *cobra.Command, cwd string, unitSelectors, args []string, explicit bool) (*checkRun, error) {
	units, ungoverned, err := config.DiscoverUnits(cwd, registryMarkers())
	if err != nil {
		return nil, err
	}
	if len(units) == 0 && explicit {
		return nil, fmt.Errorf("no %s found under %s — run `depdog init` in each subtree you want governed",
			config.DefaultName, cwd)
	}
	return runUnits(cmd, cwd, units, ungoverned, unitSelectors, args)
}

// runUnits evaluates already-discovered units (optionally narrowed by
// selectors) against their per-unit adapters, advisory-skipping the ungoverned
// marker directories. It fails fast (exit 2) on the first unit config/load
// error, matching evaluateWorkspace's contract.
func runUnits(cmd *cobra.Command, cwd string, units []config.Unit, ungoverned, selectors, args []string) (*checkRun, error) {
	selected, err := selectUnits(cwd, units, selectors)
	if err != nil {
		return nil, err
	}
	run := &checkRun{Root: filepath.Base(cwd)}
	for _, u := range selected {
		cfgPath := filepath.Join(u.Dir, config.DefaultName)
		rs, err := config.Load(cfgPath)
		if err != nil {
			return nil, fmt.Errorf("checking ./%s: %w", u.Rel, err)
		}
		adapter, err := adapterForUnit(u.Dir, rs.Lang)
		if err != nil {
			return nil, fmt.Errorf("checking ./%s: %w", u.Rel, err)
		}
		ev, err := evaluateWith(cmd, adapter, u.Dir, cfgPath, rs, args)
		if err != nil {
			return nil, fmt.Errorf("checking ./%s: %w", u.Rel, err)
		}
		run.Members = append(run.Members, memberEval{Rel: u.Rel, Lang: adapter.Name, Eval: ev})
	}
	// Advisory-skips: marker directories disjoint from every unit (D5). Only
	// shown when no --unit selection narrowed the run, so `--all --unit web`
	// still collapses to the plain single-project output.
	if len(selectors) == 0 {
		for _, dir := range ungoverned {
			run.Members = append(run.Members, memberEval{Rel: dir, SkipReason: "no " + config.DefaultName})
		}
	}
	if !run.hasAnalyzed() {
		return nil, fmt.Errorf("no %s found under %s — run `depdog init` in each subtree you want governed",
			config.DefaultName, cwd)
	}
	return run, nil
}

// selectUnits filters discovered units by --unit selectors, matching a unit's
// config directory (absolute, or relative to the cwd). It mirrors
// selectMembers/matchMember minus the go.mod module-path matching — a unit's
// directory is its only universal identity. No selectors means every unit.
func selectUnits(cwd string, units []config.Unit, selectors []string) ([]config.Unit, error) {
	if len(selectors) == 0 {
		return units, nil
	}
	out := make([]config.Unit, 0, len(selectors))
	for _, sel := range selectors {
		u, err := matchUnit(cwd, units, sel)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// matchUnit resolves a --unit selector to a discovered unit by directory
// (absolute, or relative to the cwd).
func matchUnit(cwd string, units []config.Unit, sel string) (config.Unit, error) {
	var cand string
	if filepath.IsAbs(sel) {
		cand = filepath.Clean(sel)
	} else {
		cand = filepath.Clean(filepath.Join(cwd, filepath.FromSlash(sel)))
	}
	for _, u := range units {
		if filepath.Clean(u.Dir) == cand {
			return u, nil
		}
	}
	return config.Unit{}, fmt.Errorf("--unit %q matches no discovered unit (want a directory holding a %s)", sel, config.DefaultName)
}

// selectMembers filters workspace modules by the --module selectors; no
// selectors means every member.
func selectMembers(ws *config.Workspace, selectors []string) ([]string, error) {
	if len(selectors) == 0 {
		return ws.Modules, nil
	}
	out := make([]string, 0, len(selectors))
	for _, sel := range selectors {
		dir, err := matchMember(ws, sel)
		if err != nil {
			return nil, err
		}
		out = append(out, dir)
	}
	return out, nil
}

// matchMember resolves a --module selector to a member directory, matching
// either the member's directory (absolute, or relative to the workspace or the
// cwd) or its go.mod module path.
func matchMember(ws *config.Workspace, sel string) (string, error) {
	var dirCands []string
	if filepath.IsAbs(sel) {
		dirCands = []string{filepath.Clean(sel)}
	} else {
		dirCands = append(dirCands, filepath.Join(ws.Dir, filepath.FromSlash(sel)))
		if cwd, err := os.Getwd(); err == nil {
			dirCands = append(dirCands, filepath.Join(cwd, filepath.FromSlash(sel)))
		}
	}
	for _, dir := range ws.Modules {
		for _, c := range dirCands {
			if filepath.Clean(c) == dir {
				return dir, nil
			}
		}
		if mp, err := config.ModulePathOf(dir); err == nil && mp == sel {
			return dir, nil
		}
	}
	return "", fmt.Errorf("--module %q matches no workspace member (want a module path or a go.work directory)", sel)
}

func relSlash(base, dir string) string {
	if r, err := filepath.Rel(base, dir); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(dir)
}

// reportCheck renders a resolved check run and returns the total violation
// count (for the exit code). A run with a single analyzed member and nothing
// skipped renders byte-identically to the pre-workspace single-module output —
// whether it is a plain module, `--module <one>`, or a one-member workspace.
// The aggregate reporters (and the JSON envelope) are reserved for runs that
// genuinely span members: more than one analyzed member, or a skipped-member
// advisory to report.
func reportCheck(cmd *cobra.Command, run *checkRun, format, color string, elapsed time.Duration) (int, error) {
	out := cmd.OutOrStdout()
	mods, skipped := run.split()

	if len(mods) == 1 && len(skipped) == 0 {
		m := mods[0]
		if err := renderSingle(out, format, m.Result, m.Rules, elapsed, color); err != nil {
			return 0, err
		}
		return len(m.Result.Violations), nil
	}

	total := 0
	for _, m := range mods {
		total += len(m.Result.Violations)
	}
	switch format {
	case "text":
		return total, report.TextWorkspace(out, mods, skipped, elapsed, color)
	case "json":
		return total, report.JSONWorkspace(out, run.Root, mods, skipped, elapsed)
	case "github":
		return total, report.GitHubWorkspace(out, mods)
	case "sarif":
		return total, report.SARIFWorkspace(out, mods, Version)
	default:
		return 0, fmt.Errorf("unknown --format %q (text, json, github or sarif)", format)
	}
}

func renderSingle(out io.Writer, format string, res *core.Result, rules *core.RuleSet, elapsed time.Duration, color string) error {
	switch format {
	case "text":
		return report.Text(out, res, elapsed, color)
	case "json":
		return report.JSON(out, res, rules, elapsed)
	case "github":
		return report.GitHub(out, res)
	case "sarif":
		return report.SARIF(out, res, Version)
	default:
		return fmt.Errorf("unknown --format %q (text, json, github or sarif)", format)
	}
}
