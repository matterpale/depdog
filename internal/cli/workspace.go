package cli

import (
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
	Eval       *evaluation
	SkipReason string
	Suppressed int
	Fixed      []core.BaselineEntry
}

// checkRun is what `check` resolved to evaluate. Workspace is nil for a plain
// single-module run (Members holds exactly one analyzed entry) and non-nil for
// a workspace fan-out.
type checkRun struct {
	Workspace *config.Workspace
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
			Result: m.Eval.Result,
			Rules:  m.Eval.Rules,
		})
	}
	return mods, skipped
}

// evaluateCheckTargets resolves what `check` should evaluate: every configured
// member of the active workspace (optionally narrowed by --module), or — when
// there is no workspace, GOWORK=off, or an explicit --config — the single
// module resolved the classic way.
func evaluateCheckTargets(cmd *cobra.Command, configPath string, modules, args []string) (*checkRun, error) {
	if configPath != "" {
		if len(modules) > 0 {
			return nil, fmt.Errorf("--module cannot be combined with --config")
		}
		return singleRun(cmd, configPath, args)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	ws, err := config.FindWorkspace(cwd)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		if len(modules) > 0 {
			return nil, fmt.Errorf("--module only applies inside a Go workspace (go.work); none is active here")
		}
		return singleRun(cmd, configPath, args)
	}
	return evaluateWorkspace(cmd, ws, modules, args)
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
	run := &checkRun{Workspace: ws}
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
		run.Members = append(run.Members, memberEval{Rel: rel, Eval: ev})
	}
	if !run.hasAnalyzed() {
		return nil, fmt.Errorf("no workspace member has a %s — run `depdog init` in the members you want checked", config.DefaultName)
	}
	return run, nil
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
// count (for the exit code). A single-module run renders byte-identically to
// the pre-workspace output; a workspace run uses the aggregate reporters.
func reportCheck(cmd *cobra.Command, run *checkRun, format, color string, elapsed time.Duration) (int, error) {
	out := cmd.OutOrStdout()
	if run.Workspace == nil {
		ev := run.Members[0].Eval
		if err := renderSingle(out, format, ev.Result, ev.Rules, elapsed, color); err != nil {
			return 0, err
		}
		return len(ev.Result.Violations), nil
	}

	mods, skipped := run.split()
	total := 0
	for _, m := range mods {
		total += len(m.Result.Violations)
	}
	switch format {
	case "text":
		return total, report.TextWorkspace(out, mods, skipped, elapsed, color)
	case "json":
		return total, report.JSONWorkspace(out, mods, skipped, elapsed)
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
