package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// evaluateWorkMode is the cross-unit governance run: a depdog.work.yaml at the
// checked root declares the units and the rules between them. The run is the
// polyglot fan-out (every discovered depdog.yaml intra-checked as usual) plus
// the cross pass — each declared unit's graph feeds core.EvaluateWork, whose
// verdicts land in run.CrossResult. A --unit narrowing skips the cross pass:
// cross-unit edges only mean something over the complete unit set.
func evaluateWorkMode(cmd *cobra.Command, workPath, cwd string, o checkOptions, args []string) (*checkRun, error) {
	w, err := config.LoadWork(workPath)
	if err != nil {
		return nil, err
	}
	for i := range w.Units {
		u := &w.Units[i]
		if u.Lang != "" {
			if _, ok := adapterByName(u.Lang); !ok {
				return nil, fmt.Errorf("%s: unit %q: unknown lang %q (one of: %s)",
					config.WorkFileName, u.Name, u.Lang, strings.Join(languageNames(), ", "))
			}
		}
		if fi, err := os.Stat(filepath.Join(cwd, filepath.FromSlash(u.Dir))); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("%s: unit %q: directory ./%s does not exist", config.WorkFileName, u.Name, u.Dir)
		}
	}

	// Intra-unit fan-out, exactly as --all: every discovered depdog.yaml is
	// checked with its own adapter and rules. Zero governed units is legal
	// here — the work file alone still governs the unit-level edges.
	units, ungoverned, err := config.DiscoverUnits(cwd, registryMarkers())
	if err != nil {
		return nil, err
	}
	run, err := runUnits(cmd, cwd, units, ungoverned, o.units, args, false)
	if err != nil {
		return nil, err
	}

	if len(o.units) > 0 {
		return run, nil
	}

	inputs, err := workUnitGraphs(cmd, cwd, w, run)
	if err != nil {
		return nil, err
	}
	for i := range w.Units {
		w.Units[i].Identities = config.UnitIdentities(filepath.Join(cwd, filepath.FromSlash(w.Units[i].Dir)))
	}
	cross, err := core.EvaluateWork(inputs, w)
	if err != nil {
		return nil, err
	}
	run.Work, run.CrossResult = w, cross
	amendWorkSkips(run, w)
	return run, nil
}

// workUnitGraphs collects each declared unit's import graph for the cross
// pass: the intra-check already produced one for units with their own
// depdog.yaml; a unit with a `config:` override is evaluated against that
// config (and joins the analyzed members); a unit with no config at all is
// scanned graph-only, so its outgoing edges are still governed.
func workUnitGraphs(cmd *cobra.Command, cwd string, w *core.WorkRules, run *checkRun) ([]core.UnitGraph, error) {
	byRel := make(map[string]*evaluation, len(run.Members))
	for _, m := range run.Members {
		if m.Eval != nil {
			byRel[m.Rel] = m.Eval
		}
	}
	inputs := make([]core.UnitGraph, 0, len(w.Units))
	for i := range w.Units {
		u := &w.Units[i]
		if ev, ok := byRel[u.Dir]; ok {
			inputs = append(inputs, core.UnitGraph{Unit: u.Name, Graph: ev.Graph})
			continue
		}
		dir := filepath.Join(cwd, filepath.FromSlash(u.Dir))

		if u.Config != "" {
			cfgPath := filepath.Join(cwd, filepath.FromSlash(u.Config))
			rs, err := config.Load(cfgPath)
			if err != nil {
				return nil, fmt.Errorf("unit %q: %w", u.Name, err)
			}
			adapter, err := adapterForWorkUnit(dir, u, rs.Lang)
			if err != nil {
				return nil, err
			}
			ev, err := evaluateWith(cmd, adapter, dir, cfgPath, rs, nil)
			if err != nil {
				return nil, fmt.Errorf("checking unit %q (./%s): %w", u.Name, u.Dir, err)
			}
			run.Members = append(run.Members, memberEval{Rel: u.Dir, Lang: adapter.Name, Eval: ev})
			inputs = append(inputs, core.UnitGraph{Unit: u.Name, Graph: ev.Graph})
			continue
		}

		adapter, err := adapterForWorkUnit(dir, u, "")
		if err != nil {
			return nil, err
		}
		graph, err := adapter.New(dir).Load(cmd.Context())
		if err != nil {
			return nil, fmt.Errorf("scanning unit %q (./%s): %w", u.Name, u.Dir, err)
		}
		inputs = append(inputs, core.UnitGraph{Unit: u.Name, Graph: graph})
	}
	return inputs, nil
}

// adapterForWorkUnit resolves a declared unit's adapter: its `lang:` pin in
// the work file, else the unit config's `lang:` key (cfgLang), else marker
// detection in the unit's own directory. Unlike detectLanguage this never
// walks upward — a work unit is exactly its declared subtree, and a marker
// found above it belongs to someone else.
func adapterForWorkUnit(dir string, u *core.WorkUnit, cfgLang string) (lang.Adapter, error) {
	effLang := u.Lang
	if effLang == "" {
		effLang = cfgLang
	}
	if effLang != "" {
		a, ok := adapterByName(effLang)
		if !ok {
			return lang.Adapter{}, fmt.Errorf("unit %q: %w", u.Name, unknownLangError(effLang))
		}
		return a, nil
	}
	var matched []lang.Adapter
	for _, a := range languages {
		if hasAnyMarker(dir, a.Markers) {
			matched = append(matched, a)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return lang.Adapter{}, fmt.Errorf("unit %q (./%s): no language marker found in its directory — pin the adapter with `lang:` in %s",
			u.Name, u.Dir, config.WorkFileName)
	default:
		names := make([]string, len(matched))
		for i, a := range matched {
			names[i] = a.Name
		}
		return lang.Adapter{}, fmt.Errorf("unit %q (./%s): ambiguous language (markers match %s) — pin one with `lang:` in %s",
			u.Name, u.Dir, strings.Join(names, " and "), config.WorkFileName)
	}
}

// amendWorkSkips rewrites the advisory-skip reason of directories that are
// declared work units: they were scanned for the cross pass, so "no
// depdog.yaml" alone would understate what happened.
func amendWorkSkips(run *checkRun, w *core.WorkRules) {
	byDir := make(map[string]string, len(w.Units))
	for _, u := range w.Units {
		byDir[u.Dir] = u.Name
	}
	covered := make(map[string]bool, len(run.Members))
	for i := range run.Members {
		m := &run.Members[i]
		covered[m.Rel] = true
		if m.Eval != nil {
			continue
		}
		if name, ok := byDir[m.Rel]; ok {
			m.SkipReason = fmt.Sprintf("unit %q — scanned for cross-unit governance only", name)
		}
	}
	// A declared unit with no config and no marker-bearing advisory entry of
	// its own still deserves a line: it was scanned, not silently ignored.
	for _, u := range w.Units {
		if !covered[u.Dir] {
			run.Members = append(run.Members, memberEval{
				Rel:        u.Dir,
				SkipReason: fmt.Sprintf("unit %q — scanned for cross-unit governance only", u.Name),
			})
		}
	}
}
