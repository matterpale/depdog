package report

import (
	"encoding/json"
	"io"
	"time"

	"github.com/matterpale/depdog/internal/core"
)

// The JSON schema is part of depdog's public interface: field names are
// stable, absent collections encode as [] rather than null.

type jsonReport struct {
	Module     string          `json:"module"`
	Default    string          `json:"default"`
	Violations []jsonViolation `json:"violations"`
	Warnings   []jsonWarning   `json:"warnings"`
	Components []jsonComponent `json:"components"`
	Boundaries []jsonBoundary  `json:"boundaries"`
	Cycles     [][]string      `json:"cycles"`
	Stats      jsonStats       `json:"stats"`
}

type jsonComponent struct {
	Name       string   `json:"name"`
	Stance     string   `json:"stance"`
	Allow      []string `json:"allow,omitempty"`
	Deny       []string `json:"deny,omitempty"`
	Packages   int      `json:"packages"`
	Edges      int      `json:"edges"`
	Violations int      `json:"violations"`
}

type jsonViolation struct {
	FromPackage   string         `json:"from_package"`
	FromComponent string         `json:"from_component"`
	Import        string         `json:"import"`
	Target        string         `json:"target"`
	Rule          string         `json:"rule"`
	TestOnly      bool           `json:"test_only"`
	Boundary      string         `json:"boundary,omitempty"` // boundary name for boundary violations
	Reason        string         `json:"reason,omitempty"`   // "boundary" / "boundary-sealed"
	Explanation   string         `json:"explanation"`        // plain-English WHY + fix (additive; the machine-readable reason/kind stay above)
	Positions     []jsonPosition `json:"positions"`
}

// jsonBoundary is one declared boundary: its members and sealed flag. A stable
// top-level array, encoded as [] when absent.
type jsonBoundary struct {
	Name    string               `json:"name"`
	Sealed  bool                 `json:"sealed"`
	Members []jsonBoundaryMember `json:"members"`
}

// jsonBoundaryMember is one member of a boundary: a component name or a set of
// glob patterns. Exactly one of component/path is populated.
type jsonBoundaryMember struct {
	Component string   `json:"component,omitempty"` // set for component members
	Path      []string `json:"path,omitempty"`      // set for glob members
}

type jsonPosition struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type jsonWarning struct {
	Kind      string `json:"kind"`
	Package   string `json:"package,omitempty"`
	Dir       string `json:"dir,omitempty"`
	Component string `json:"component,omitempty"`
	Boundary  string `json:"boundary,omitempty"`
}

type jsonStats struct {
	Packages   int   `json:"packages"`
	Edges      int   `json:"edges"`
	DurationMS int64 `json:"duration_ms"`
}

// refStrings renders rule refs as plain strings, or nil when empty so the
// allow/deny fields are omitted.
func refStrings(refs []core.Ref) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.String()
	}
	return out
}

// emptyIfNil ensures an absent cycle list encodes as [] rather than null,
// matching the schema convention for the other collections.
func emptyIfNil(c [][]string) [][]string {
	if c == nil {
		return [][]string{}
	}
	return c
}

func JSON(w io.Writer, res *core.Result, rs *core.RuleSet, elapsed time.Duration) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(buildReport(res, rs, elapsed))
}

// jsonWorkspaceReport is the aggregate envelope for a multi-unit check (a go.work
// fan-out or a polyglot --all run): the walk-root's basename, one jsonUnit per
// analyzed unit, the marker directories skipped for lack of a config, and
// rolled-up stats. A single-unit run keeps emitting jsonReport at the top level
// (no envelope), so existing consumers are unaffected; the presence of the
// "units" array is the discriminator.
type jsonWorkspaceReport struct {
	Root    string     `json:"root"`
	Units   []jsonUnit `json:"units"`
	Skipped []jsonSkip `json:"skipped"`
	// CrossUnit carries the cross-unit governance pass of a depdog.work.yaml
	// run; absent on every other run (purely additive to the envelope).
	CrossUnit *jsonCrossUnit `json:"cross_unit,omitempty"`
	Stats     jsonStats      `json:"stats"`
}

// jsonUnit is one analyzed unit in the envelope: its walk-root-relative
// directory (a stable key), the adapter that checked it, and the same per-unit
// jsonReport fields a single-unit run emits ("module", "violations", …).
type jsonUnit struct {
	Dir  string `json:"dir"`
	Lang string `json:"lang"`
	jsonReport
}

type jsonSkip struct {
	Dir    string `json:"dir"`
	Reason string `json:"reason"`
}

// JSONWorkspace encodes the aggregate envelope. Per-unit duration is left at 0
// (only the aggregate carries elapsed); root is the walk-root's basename and
// each unit self-identifies by its dir + module path, so no machine-specific
// absolute path leaks into the output. cross (nil outside work-file runs)
// nests the cross-unit pass under "cross_unit".
func JSONWorkspace(w io.Writer, root string, mods []Module, skipped []Skipped, cross *CrossUnit, elapsed time.Duration) error {
	out := jsonWorkspaceReport{
		Root:    root,
		Units:   make([]jsonUnit, 0, len(mods)),
		Skipped: make([]jsonSkip, 0, len(skipped)),
	}
	if cross != nil {
		out.CrossUnit = buildCrossUnit(cross)
	}
	for _, m := range mods {
		out.Units = append(out.Units, jsonUnit{
			Dir:        m.Rel,
			Lang:       m.Lang,
			jsonReport: buildReport(m.Result, m.Rules, 0),
		})
		out.Stats.Packages += m.Result.Stats.Packages
		out.Stats.Edges += m.Result.Stats.Edges
	}
	for _, s := range skipped {
		out.Skipped = append(out.Skipped, jsonSkip{Dir: "./" + s.Rel, Reason: s.Reason})
	}
	out.Stats.DurationMS = elapsed.Milliseconds()
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func buildReport(res *core.Result, rs *core.RuleSet, elapsed time.Duration) jsonReport {
	out := jsonReport{
		Module:     res.ModulePath,
		Default:    policyName(rs.Policy),
		Violations: make([]jsonViolation, 0, len(res.Violations)),
		Warnings:   make([]jsonWarning, 0, len(res.Warnings)),
		Components: make([]jsonComponent, 0, len(res.Components)),
		Boundaries: make([]jsonBoundary, 0, len(rs.Boundaries)),
		Cycles:     emptyIfNil(res.Cycles),
		Stats: jsonStats{
			Packages:   res.Stats.Packages,
			Edges:      res.Stats.Edges,
			DurationMS: elapsed.Milliseconds(),
		},
	}
	for _, v := range res.Violations {
		jv := jsonViolation{
			FromPackage:   v.FromPackage,
			FromComponent: v.FromComponent,
			Import:        v.ImportPath,
			Target:        v.Target,
			Rule:          v.Rule,
			TestOnly:      v.TestOnly,
			Boundary:      v.Boundary,
			Reason:        string(v.Reason),
			Explanation:   core.Explanation(core.ExplainViolation(v, rs)),
			Positions:     make([]jsonPosition, 0, len(v.Positions)),
		}
		for _, p := range v.Positions {
			jv.Positions = append(jv.Positions, jsonPosition{File: p.File, Line: p.Line})
		}
		out.Violations = append(out.Violations, jv)
	}
	for _, wr := range res.Warnings {
		kind := wr.Kind
		if kind == "" {
			kind = "unassigned"
		}
		out.Warnings = append(out.Warnings, jsonWarning{
			Kind: kind, Package: wr.Package, Dir: wr.RelDir, Component: wr.Component, Boundary: wr.Boundary,
		})
	}
	for _, b := range rs.Boundaries {
		jb := jsonBoundary{Name: b.Name, Sealed: b.Sealed, Members: make([]jsonBoundaryMember, 0, len(b.Members))}
		for _, m := range b.Members {
			jm := jsonBoundaryMember{}
			if m.Component != "" {
				jm.Component = m.Component
			} else {
				jm.Path = m.Patterns
			}
			jb.Members = append(jb.Members, jm)
		}
		out.Boundaries = append(out.Boundaries, jb)
	}
	for _, c := range res.Components {
		stance := "whitelist"
		if rs.Stance(c.Name) == core.PolicyAllow {
			stance = "blacklist"
		}
		rule := rs.Rules[c.Name]
		out.Components = append(out.Components, jsonComponent{
			Name: c.Name, Stance: stance,
			Allow: refStrings(rule.Allow), Deny: refStrings(rule.Deny),
			Packages: c.Packages, Edges: c.Edges, Violations: c.Violations,
		})
	}
	return out
}
