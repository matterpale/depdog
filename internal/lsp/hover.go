package lsp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matterpale/depdog/internal/core"
)

// hoverState is what one successful check round leaves behind for
// textDocument/hover: the Check snapshot, the root its file URIs were built
// against (the client's announced root when given, else Check.Root — the same
// choice publish made for diagnostics), and a file→edge index over the
// graph's import positions. A failed re-check keeps the previous hoverState,
// mirroring how stale diagnostics stay visible after a failed round.
type hoverState struct {
	check     *Check
	root      string
	configURI string // "" when the server has no configBase to link (see Server.configBase)
	index     map[string][]edgeRef
}

// edgeRef locates one import edge for hover: the importing package's
// module-relative dir and import path, plus the edge itself (whose Positions
// say which lines it occupies).
type edgeRef struct {
	fromRelDir     string
	fromImportPath string
	imp            core.Import
}

// buildEdgeIndex maps every module-relative source file to the import edges
// whose Positions mention it, skipping rs.Skipped packages and edges into
// skipped targets — exactly the edges Evaluate and BuildPackageViews judge.
// Each file's slice is sorted by import path (then importing package) so a
// multi-edge hover renders deterministically. An import with several
// positions in one file is indexed once for that file; edgesAt re-checks the
// exact line against imp.Positions.
func buildEdgeIndex(g *core.Graph, rs *core.RuleSet) map[string][]edgeRef {
	if g == nil || rs == nil {
		return nil
	}
	index := make(map[string][]edgeRef)
	for _, p := range g.Packages {
		if rs.Skipped(p.RelDir) {
			continue
		}
		for _, imp := range p.Imports {
			if imp.Class == core.ClassInModule && rs.Skipped(imp.RelDir) {
				continue
			}
			seen := make(map[string]bool, len(imp.Positions))
			for _, pos := range imp.Positions {
				if seen[pos.File] {
					continue
				}
				seen[pos.File] = true
				index[pos.File] = append(index[pos.File], edgeRef{
					fromRelDir:     p.RelDir,
					fromImportPath: p.ImportPath,
					imp:            imp,
				})
			}
		}
	}
	for file := range index {
		es := index[file]
		sort.Slice(es, func(i, j int) bool {
			if es[i].imp.Path != es[j].imp.Path {
				return es[i].imp.Path < es[j].imp.Path
			}
			return es[i].fromImportPath < es[j].fromImportPath
		})
	}
	return index
}

// edgesAt returns the indexed edges of file that have an import statement on
// the given 1-based line, preserving the index's import-path order.
func edgesAt(index map[string][]edgeRef, file string, line int) []edgeRef {
	var out []edgeRef
	for _, e := range index[file] {
		for _, pos := range e.imp.Positions {
			if pos.File == file && pos.Line == line {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// relativeFile inverts diagnostics.go's fileURI: it maps a file:// URI back
// to the module-relative path the edge index is keyed by. ok is false when
// the URI is not a file URI or does not sit under root.
func relativeFile(root, uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	r := filepath.ToSlash(filepath.Clean(root))
	if !strings.HasPrefix(r, "/") {
		r = "/" + r // a Windows drive root gains the same slash fileURI adds
	}
	if !strings.HasPrefix(u.Path, r+"/") {
		return "", false
	}
	return strings.TrimPrefix(u.Path, r+"/"), true
}

type hoverParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	} `json:"position"`
}

type hoverResult struct {
	Contents markupContent `json:"contents"`
	Range    lspRange      `json:"range"`
}

type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// hoverResponse answers one textDocument/hover request from the last
// successful check round. A null result — legal for hover — covers every
// no-answer case: no successful round yet, a URI outside the project or
// unknown to the index, or a line without an import. The character position
// is ignored: core positions are line-granular (the same no-column limitation
// as diagnostics), so the returned range spans the hovered line at
// character 0.
func (s *Server) hoverResponse(logger *log.Logger, msg *message, hs *hoverState) (*message, error) {
	null := &message{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage("null")}
	if hs == nil {
		return null, nil
	}
	var p hoverParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		logger.Printf("hover with unparseable params: %v", err)
		return null, nil
	}
	rel, ok := relativeFile(hs.root, p.TextDocument.URI)
	if !ok {
		return null, nil
	}
	// LSP lines are 0-based, core.Position is 1-based.
	edges := edgesAt(hs.index, rel, p.Position.Line+1)
	if len(edges) == 0 {
		return null, nil
	}
	value, err := hoverMarkdown(hs.check.Rules, edges, hs.configURI)
	if err != nil {
		// Unreachable after a successful Evaluate (the same assignments
		// already resolved); answer null rather than fail the request.
		logger.Printf("hover verdict failed: %v", err)
		return null, nil
	}
	return response(msg.ID, hoverResult{
		Contents: markupContent{Kind: "markdown", Value: value},
		Range: lspRange{
			Start: lspPosition{Line: p.Position.Line},
			End:   lspPosition{Line: p.Position.Line},
		},
	})
}

// hoverMarkdown renders one markdown block per edge (already sorted by import
// path): a header naming the edge, a blank line, then the verdict in the
// explain vocabulary. TestOnly edges gain the same " [test]" suffix as
// diagnostics messages. configURI ("" if the server has none) is appended as
// a trailing link so hover offers the same "open the config" jump-off point
// as a diagnostic's relatedInformation (see diagnostics.go).
func hoverMarkdown(rs *core.RuleSet, edges []edgeRef, configURI string) (string, error) {
	blocks := make([]string, 0, len(edges))
	for _, e := range edges {
		v, err := verdictFor(rs, e.fromRelDir, e.imp)
		if err != nil {
			return "", err
		}
		comp := v.Component
		if comp == "" {
			comp = "unassigned"
		}
		var verdict string
		switch {
		case v.Reason == "":
			verdict = "the source package is unassigned — no rule governs its imports"
		case v.Allowed:
			verdict = "allowed by `" + v.Reason + "`"
		default:
			verdict = "denied by `" + v.Reason + "`"
		}
		if e.imp.TestOnly {
			verdict += " [test]"
		}
		block := fmt.Sprintf("**depdog** — `%s` (%s) → `%s` (%s)\n\n%s",
			e.fromImportPath, comp, e.imp.Path, v.Target, verdict)
		// A denied edge gains the same plain-English WHY + fix every other
		// surface carries (one source of wording: core.Explanation), rendered as
		// a trailing paragraph. Allowed and unassigned edges need none.
		if !v.Allowed && v.Reason != "" {
			prose := core.Explanation(core.ExplainViolation(core.Violation{
				FromPackage: e.fromImportPath, FromComponent: v.Component,
				ImportPath: e.imp.Path, Target: v.Target,
				Reason: v.Kind, Boundary: v.Boundary,
			}, rs))
			block += "\n\n" + prose
		}
		blocks = append(blocks, block)
	}
	if configURI != "" {
		blocks = append(blocks, fmt.Sprintf("[depdog.yaml](%s)", configURI))
	}
	return strings.Join(blocks, "\n\n"), nil
}

// edgeVerdict is verdictFor's answer: the resolved source component (""
// when unassigned), the display target (a component name, "unassigned",
// "std" or "external") and — for an assigned source — whether the edge
// passes plus the rule/policy/boundary text that decided it, in exactly the
// vocabulary report.ExplainEdge prints, so hover and `depdog explain` never
// diverge in wording. Reason is "" only for an unassigned source, where no
// rule governs the imports at all.
type edgeVerdict struct {
	Component string
	Target    string
	Allowed   bool
	Reason    string
	// Kind and Boundary carry the machine-readable classification a denied edge
	// needs to phrase its prose explanation: ReasonRule for an ordinary
	// allow/deny/stance deny, a boundary kind (with Boundary set) for a boundary
	// crossing. They let hoverMarkdown reuse core.Explanation without re-deciding.
	Kind     core.ReasonKind
	Boundary string
}

// verdictFor mirrors report.ExplainEdge for one concrete import edge, calling
// the same core primitives directly (this package must not import
// internal/report). The boundary gate runs first — a boundary deny is a hard
// deny that wins over any component allow, the same order Evaluate and
// ExplainEdge use — then Decide/DecideModule settles the component decision.
func verdictFor(rs *core.RuleSet, fromRelDir string, imp core.Import) (edgeVerdict, error) {
	var v edgeVerdict
	comp, err := rs.AssignComponent(fromRelDir)
	if err != nil {
		return v, err
	}
	v.Component = comp

	switch imp.Class {
	case core.ClassInModule:
		target, err := rs.AssignComponent(imp.RelDir)
		if err != nil {
			return v, err
		}
		v.Target = target
		if target == "" {
			v.Target = "unassigned"
		}
	default:
		v.Target = imp.Class.String()
	}

	if comp == "" {
		return v, nil // unassigned source: no rule governs its imports
	}

	if imp.Class == core.ClassInModule {
		ok, boundary, sealed, err := rs.DecideBoundary(fromRelDir, imp.RelDir)
		if err != nil {
			return v, err
		}
		if !ok {
			suffix := ""
			v.Kind = core.ReasonBoundary
			if sealed {
				suffix = " (sealed)"
				v.Kind = core.ReasonBoundarySealed
			}
			v.Boundary = boundary
			v.Reason = fmt.Sprintf("boundary %q%s", boundary, suffix)
			return v, nil
		}
	}

	switch imp.Class {
	case core.ClassStd:
		v.Allowed, v.Reason = rs.Decide(comp, "std")
	case core.ClassExternal:
		v.Allowed, v.Reason = rs.DecideModule(comp, imp.Path)
	default:
		v.Allowed, v.Reason = rs.Decide(comp, v.Target)
	}
	return v, nil
}
