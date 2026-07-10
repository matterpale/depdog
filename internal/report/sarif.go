package report

import (
	"encoding/json"
	"io"
	"sort"

	"github.com/matterpale/depdog/internal/core"
)

// SARIF renders the result as a SARIF 2.1.0 log, the format GitHub code
// scanning and other tools ingest. Violations are error-level results keyed by
// the component whose rule fired; unassigned packages are note-level results.
// version stamps the tool driver. Output is deterministic given a sorted
// Result.
func SARIF(w io.Writer, res *core.Result, version string) error {
	return encodeSARIF(w, []sarifRun{sarifRunFor(res, version, "")})
}

// SARIFWorkspace merges the workspace's members into one SARIF log with one run
// per member (SARIF's runs array is built for exactly this). Each member's file
// URIs are prefixed with its workspace-relative directory so code-scanning
// resolves them from the repo root.
func SARIFWorkspace(w io.Writer, mods []Module, version string) error {
	runs := make([]sarifRun, 0, len(mods))
	for _, m := range mods {
		runs = append(runs, sarifRunFor(m.Result, version, m.Rel))
	}
	return encodeSARIF(w, runs)
}

// sarifRunFor builds one SARIF run for a result. prefix (a member's workspace-
// relative dir, "" for a single module) is joined onto every file URI.
func sarifRunFor(res *core.Result, version, prefix string) sarifRun {
	const unassignedRule = "unassigned-package"

	descriptions := map[string]string{}
	for _, v := range res.Violations {
		if _, ok := descriptions[v.FromComponent]; !ok {
			descriptions[v.FromComponent] = "Import rules for component " + v.FromComponent
		}
	}
	if len(res.Warnings) > 0 {
		descriptions[unassignedRule] = "Package not covered by any component"
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

	results := make([]sarifResult, 0, len(res.Violations)+len(res.Warnings))
	for _, v := range res.Violations {
		locs := make([]sarifLocation, 0, len(v.Positions))
		for _, p := range v.Positions {
			locs = append(locs, sarifLocation{PhysicalLocation: sarifPhysical{
				ArtifactLocation: sarifArtifact{URI: joinPrefix(prefix, p.File)},
				Region:           &sarifRegion{StartLine: p.Line},
			}})
		}
		results = append(results, sarifResult{
			RuleID:    v.FromComponent,
			Level:     "error",
			Message:   sarifText{Text: v.FromComponent + " imports " + v.ImportPath + " (" + v.Rule + ")"},
			Locations: locs,
		})
	}
	for _, wr := range res.Warnings {
		results = append(results, sarifResult{
			RuleID:  unassignedRule,
			Level:   "note",
			Message: sarifText{Text: wr.Package + " is not covered by any component"},
			Locations: []sarifLocation{{PhysicalLocation: sarifPhysical{
				ArtifactLocation: sarifArtifact{URI: joinPrefix(prefix, wr.RelDir)},
			}}},
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

func encodeSARIF(w io.Writer, runs []sarifRun) error {
	doc := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs:    runs,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string    `json:"id"`
	ShortDescription sarifText `json:"shortDescription"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

type sarifText struct {
	Text string `json:"text"`
}
