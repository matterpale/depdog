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
	Violations []jsonViolation `json:"violations"`
	Warnings   []jsonWarning   `json:"warnings"`
	Components []jsonComponent `json:"components"`
	Cycles     [][]string      `json:"cycles"`
	Stats      jsonStats       `json:"stats"`
}

type jsonComponent struct {
	Name       string `json:"name"`
	Packages   int    `json:"packages"`
	Edges      int    `json:"edges"`
	Violations int    `json:"violations"`
}

type jsonViolation struct {
	FromPackage   string         `json:"from_package"`
	FromComponent string         `json:"from_component"`
	Import        string         `json:"import"`
	Target        string         `json:"target"`
	Rule          string         `json:"rule"`
	TestOnly      bool           `json:"test_only"`
	Positions     []jsonPosition `json:"positions"`
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
}

type jsonStats struct {
	Packages   int   `json:"packages"`
	Edges      int   `json:"edges"`
	DurationMS int64 `json:"duration_ms"`
}

// emptyIfNil ensures an absent cycle list encodes as [] rather than null,
// matching the schema convention for the other collections.
func emptyIfNil(c [][]string) [][]string {
	if c == nil {
		return [][]string{}
	}
	return c
}

func JSON(w io.Writer, res *core.Result, elapsed time.Duration) error {
	out := jsonReport{
		Module:     res.ModulePath,
		Violations: make([]jsonViolation, 0, len(res.Violations)),
		Warnings:   make([]jsonWarning, 0, len(res.Warnings)),
		Components: make([]jsonComponent, 0, len(res.Components)),
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
			Kind: kind, Package: wr.Package, Dir: wr.RelDir, Component: wr.Component,
		})
	}
	for _, c := range res.Components {
		out.Components = append(out.Components, jsonComponent{
			Name: c.Name, Packages: c.Packages, Edges: c.Edges, Violations: c.Violations,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
