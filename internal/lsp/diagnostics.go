package lsp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// LSP DiagnosticSeverity. Every depdog violation fails `depdog check`, so all
// diagnostics are errors — including TestOnly ones: if the test_files policy
// let a test-only edge through it never becomes a Violation, and if it didn't,
// the edge fails the build like any other. docs/lsp.md records the choice.
const severityError = 1

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

type diagnostic struct {
	Range              lspRange             `json:"range"`
	Severity           int                  `json:"severity"`
	Code               string               `json:"code"`
	Source             string               `json:"source"`
	Message            string               `json:"message"`
	RelatedInformation []relatedInformation `json:"relatedInformation,omitempty"`
}

// relatedInformation points a diagnostic back at the rule that decided it, in
// depdog.yaml. Editors render it as a clickable link under the diagnostic
// (or in its hover), the same "open the config" affordance the TUI's config
// tab already offers via `e` — this is that affordance for the LSP surface.
// The location is always line 0 of the config file: depdog does not track
// which yaml line declared a given component/rule (config.Parse discards
// that), so this opens the file rather than jumping to the exact rule.
type relatedInformation struct {
	Location location `json:"location"`
	Message  string   `json:"message"`
}

type location struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// diagnosticsFor maps a check result to one publishDiagnostics payload per
// file that carries a violation. Positions are module-root-relative (the same
// contract internal/report/sarif.go relies on), so URIs are root joined with
// Position.File. LSP lines are 0-based; core.Position has no column, so the
// range covers character 0 of the line only (line-level squiggle).
//
// Output is deterministic: payloads sorted by URI, diagnostics by line, then
// message. core.Warnings carry no positions and are not mapped. configURI is
// the depdog.yaml this check ran against ("" suppresses relatedInformation,
// e.g. when the server has no config file to link).
func diagnosticsFor(res *core.Result, root, configURI string) []publishDiagnosticsParams {
	byURI := make(map[string][]diagnostic)
	for _, v := range res.Violations {
		msg := fmt.Sprintf("%s imports %s (%s): %s", v.FromComponent, v.ImportPath, v.Target, v.Rule)
		if v.TestOnly {
			msg += " [test]"
		}
		var related []relatedInformation
		if configURI != "" {
			related = []relatedInformation{{
				Location: location{URI: configURI},
				Message:  "rule: " + v.Rule,
			}}
		}
		for _, p := range v.Positions {
			uri := fileURI(root, p.File)
			byURI[uri] = append(byURI[uri], diagnostic{
				Range: lspRange{
					Start: lspPosition{Line: p.Line - 1},
					End:   lspPosition{Line: p.Line - 1},
				},
				Severity:           severityError,
				Code:               v.Rule,
				Source:             "depdog",
				Message:            msg,
				RelatedInformation: related,
			})
		}
	}

	uris := make([]string, 0, len(byURI))
	for uri := range byURI {
		uris = append(uris, uri)
	}
	sort.Strings(uris)

	out := make([]publishDiagnosticsParams, 0, len(uris))
	for _, uri := range uris {
		ds := byURI[uri]
		sort.Slice(ds, func(i, j int) bool {
			if ds[i].Range.Start.Line != ds[j].Range.Start.Line {
				return ds[i].Range.Start.Line < ds[j].Range.Start.Line
			}
			return ds[i].Message < ds[j].Message
		})
		out = append(out, publishDiagnosticsParams{URI: uri, Diagnostics: ds})
	}
	return out
}

// fileURI joins the project root with a module-root-relative file and renders
// it as a file:// URI with forward slashes. A Windows drive path (C:\...)
// gains the leading slash file URIs require.
func fileURI(root, rel string) string {
	p := filepath.ToSlash(filepath.Join(root, filepath.FromSlash(rel)))
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}
