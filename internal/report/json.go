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
	Stats      jsonStats       `json:"stats"`
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
	Kind    string `json:"kind"`
	Package string `json:"package"`
	Dir     string `json:"dir"`
}

type jsonStats struct {
	Packages   int   `json:"packages"`
	Edges      int   `json:"edges"`
	DurationMS int64 `json:"duration_ms"`
}

func JSON(w io.Writer, res *core.Result, elapsed time.Duration) error {
	out := jsonReport{
		Module:     res.ModulePath,
		Violations: make([]jsonViolation, 0, len(res.Violations)),
		Warnings:   make([]jsonWarning, 0, len(res.Warnings)),
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
		out.Warnings = append(out.Warnings, jsonWarning{Kind: "unassigned", Package: wr.Package, Dir: wr.RelDir})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
