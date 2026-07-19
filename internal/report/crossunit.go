package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// CrossUnit bundles the cross-unit governance pass of a depdog.work.yaml run
// for the aggregate reporters: the unit-level verdicts and the work rules that
// judged them (needed for stances, boundaries, surfaces and explanations).
// Positions inside Result are already walk-root-relative, so no prefixing.
type CrossUnit struct {
	Result *core.Result
	Work   *core.WorkRules
}

// textCrossUnit renders the cross-unit section of the aggregate human report:
// a header naming the work file, the violations grouped by fired rule (the
// same layout as a unit section's violations), and the unit-cycle advisory.
func textCrossUnit(b io.Writer, st styles, cu *CrossUnit) {
	res := cu.Result
	fmt.Fprintf(b, "%s\n", st.rule.Render(fmt.Sprintf("▸ cross-unit (%s, %s governed)",
		"depdog.work.yaml", plural(len(cu.Work.Units), "unit"))))

	var order []string
	groups := make(map[string][]core.Violation)
	for _, v := range res.Violations {
		if _, ok := groups[v.Rule]; !ok {
			order = append(order, v.Rule)
		}
		groups[v.Rule] = append(groups[v.Rule], v)
	}
	for _, rule := range order {
		vs := groups[rule]
		fmt.Fprintf(b, "\n%s %s  (%s)\n", st.bad.Render("✗"), st.rule.Render(rule), plural(len(vs), "violation"))
		width := 0
		for _, v := range vs {
			if len(v.ImportPath) > width {
				width = len(v.ImportPath)
			}
		}
		lastUnit := ""
		for _, v := range vs {
			if v.FromPackage != lastUnit {
				fmt.Fprintf(b, "    %s\n", v.FromPackage)
				lastUnit = v.FromPackage
			}
			pos := ""
			if len(v.Positions) > 0 {
				pos = fmt.Sprintf("  %s:%d", v.Positions[0].File, v.Positions[0].Line)
				if len(v.Positions) > 1 {
					pos += fmt.Sprintf(" (+%d more)", len(v.Positions)-1)
				}
			}
			marker := ""
			if v.TestOnly {
				marker = " [test]"
			}
			fmt.Fprintf(b, "      → %s%s%s\n",
				st.imp.Render(fmt.Sprintf("%-*s", width, v.ImportPath)), st.pos.Render(pos), marker)
		}
	}
	if len(res.Cycles) > 0 {
		fmt.Fprintf(b, "\n%s %s:\n", st.warn.Render("!"), plural(len(res.Cycles), "unit cycle"))
		for _, c := range res.Cycles {
			fmt.Fprintf(b, "    %s\n", strings.Join(c, " ↔ "))
		}
	}
	if len(res.Violations) == 0 {
		fmt.Fprintf(b, "%s\n", st.good.Render("✓ no cross-unit violations"))
	}
}

// jsonCrossUnit is the machine shape of the cross pass, nested under the
// envelope's "cross_unit" key (present only on work-file runs — purely
// additive to the frozen envelope). Collections encode as [], never null.
type jsonCrossUnit struct {
	Default    string               `json:"default"`
	Units      []jsonWorkUnit       `json:"units"`
	Violations []jsonCrossViolation `json:"violations"`
	Boundaries []jsonBoundary       `json:"boundaries"`
	Cycles     [][]string           `json:"cycles"`
	Stats      jsonCrossStats       `json:"stats"`
}

type jsonWorkUnit struct {
	Name       string   `json:"name"`
	Dir        string   `json:"dir"`
	Lang       string   `json:"lang,omitempty"`
	Identities []string `json:"identities,omitempty"`
}

type jsonCrossViolation struct {
	FromUnit    string         `json:"from_unit"`
	ToUnit      string         `json:"to_unit"`
	Path        string         `json:"path"` // the target unit for unit-level denials; the offending target path for surface denials
	Rule        string         `json:"rule"`
	Severity    string         `json:"severity"` // always "error" (units can't opt into warn); present for consistency with intra-unit JSON
	Reason      string         `json:"reason"`
	Boundary    string         `json:"boundary,omitempty"`
	TestOnly    bool           `json:"test_only"`
	Explanation string         `json:"explanation"`
	Positions   []jsonPosition `json:"positions"`
}

type jsonCrossStats struct {
	Units int `json:"units"`
	Edges int `json:"edges"`
}

func buildCrossUnit(cu *CrossUnit) *jsonCrossUnit {
	res, w := cu.Result, cu.Work
	out := &jsonCrossUnit{
		Default:    policyName(w.Rules.Policy),
		Units:      make([]jsonWorkUnit, 0, len(w.Units)),
		Violations: make([]jsonCrossViolation, 0, len(res.Violations)),
		Boundaries: make([]jsonBoundary, 0, len(w.Rules.Boundaries)),
		Cycles:     emptyIfNil(res.Cycles),
		Stats:      jsonCrossStats{Units: res.Stats.Packages, Edges: res.Stats.Edges},
	}
	for _, u := range w.Units {
		out.Units = append(out.Units, jsonWorkUnit{Name: u.Name, Dir: u.Dir, Lang: u.Lang, Identities: u.Identities})
	}
	for _, v := range res.Violations {
		jv := jsonCrossViolation{
			FromUnit:    v.FromPackage,
			ToUnit:      v.Target,
			Path:        v.ImportPath,
			Rule:        v.Rule,
			Severity:    v.Severity.String(),
			Reason:      string(v.Reason),
			Boundary:    v.Boundary,
			TestOnly:    v.TestOnly,
			Explanation: core.Explanation(core.ExplainWorkViolation(v, w)),
			Positions:   make([]jsonPosition, 0, len(v.Positions)),
		}
		for _, p := range v.Positions {
			jv.Positions = append(jv.Positions, jsonPosition{File: p.File, Line: p.Line})
		}
		out.Violations = append(out.Violations, jv)
	}
	for _, b := range w.Rules.Boundaries {
		jb := jsonBoundary{Name: b.Name, Sealed: b.Sealed, Members: make([]jsonBoundaryMember, 0, len(b.Members))}
		for _, m := range b.Members {
			jb.Members = append(jb.Members, jsonBoundaryMember{Component: m.Component})
		}
		out.Boundaries = append(out.Boundaries, jb)
	}
	return out
}

// githubCrossUnit writes one ::error:: annotation per source position of each
// cross-unit violation. Positions are already repo-root-relative.
func githubCrossUnit(b *strings.Builder, cu *CrossUnit) {
	for _, v := range cu.Result.Violations {
		msg := fmt.Sprintf("cross-unit: %s depends on %s (%s)", v.FromPackage, v.ImportPath, v.Rule)
		if v.TestOnly {
			msg += " [test]"
		}
		msg += " — " + core.Explanation(core.ExplainWorkViolation(v, cu.Work))
		if len(v.Positions) == 0 {
			fmt.Fprintf(b, "::error::%s\n", ghData(msg))
			continue
		}
		for _, p := range v.Positions {
			fmt.Fprintf(b, "::error file=%s,line=%d::%s\n", ghProp(p.File), p.Line, ghData(msg))
		}
	}
}

// sarifCrossRun builds the extra SARIF run carrying the cross-unit verdicts,
// one rule per source unit (mirroring sarifRunFor's per-component rules).
func sarifCrossRun(cu *CrossUnit, version string) sarifRun {
	res := cu.Result
	descriptions := map[string]string{}
	for _, v := range res.Violations {
		if _, ok := descriptions[v.FromComponent]; !ok {
			descriptions[v.FromComponent] = "Cross-unit dependency rules for unit " + v.FromComponent
		}
	}
	ids := make([]string, 0, len(descriptions))
	for id := range descriptions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rules := make([]sarifRule, 0, len(ids))
	for _, id := range ids {
		rules = append(rules, sarifRule{ID: id, ShortDescription: sarifText{Text: descriptions[id]}})
	}

	results := make([]sarifResult, 0, len(res.Violations))
	for _, v := range res.Violations {
		locs := make([]sarifLocation, 0, len(v.Positions))
		for _, p := range v.Positions {
			locs = append(locs, sarifLocation{PhysicalLocation: sarifPhysical{
				ArtifactLocation: sarifArtifact{URI: p.File},
				Region:           &sarifRegion{StartLine: p.Line},
			}})
		}
		results = append(results, sarifResult{
			RuleID: v.FromComponent,
			Level:  "error",
			Message: sarifText{Text: "cross-unit: " + v.FromComponent + " depends on " + v.ImportPath + " (" + v.Rule + ") — " +
				core.Explanation(core.ExplainWorkViolation(v, cu.Work))},
			Locations: locs,
		})
	}

	return sarifRun{
		Tool: sarifTool{Driver: sarifDriver{
			Name:           "depdog",
			InformationURI: "https://github.com/matterpale/depdog",
			Version:        version,
			Rules:          rules,
		}},
		Results: results,
	}
}

// violationCount is the cross pass's contribution to the aggregate summary
// counts; nil-safe so reporters can call it unconditionally.
func (cu *CrossUnit) violationCount() int {
	if cu == nil || cu.Result == nil {
		return 0
	}
	return len(cu.Result.Violations)
}
