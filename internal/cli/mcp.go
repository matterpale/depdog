package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/mcp"
	"github.com/matterpale/depdog/internal/report"
)

func mcpCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server over stdio for in-loop architecture queries",
		Long: `mcp speaks the Model Context Protocol on stdin/stdout so any MCP-capable
agent (Claude, Cursor, …) can consult the architecture in the loop, not just
as a post-hoc CI gate. It is read-only: no rule mutation over MCP.

The server exposes tools — ` + "`check`" + ` (violations as JSON), ` + "`explain`" + ` and
` + "`can_import`" + ` (per-edge verdicts) — and resources ` + "`depdog://config`" + ` and
` + "`depdog://components`" + `. It is a thin protocol adapter over capability that
already exists (the rule set + the JSON reporter + the check entry points;
design and roadmap: docs/mcp.md).

Wire it into an agent as the command ` + "`depdog mcp`" + ` over stdio. Logs go to
stderr, never stdout.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The handler closes over config discovery, adapter selection,
			// evaluation and JSON rendering, all of which stay here in cli so
			// internal/mcp remains a pure protocol layer (D1). The server only
			// speaks protocol; every tool/resource dispatches to these methods.
			srv := mcp.NewServer(newMCPHandler(cmd, configPath), Version)
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to the project marker)")
	return cmd
}

// mcpHandler is the injected mcp.Handler: it runs the same evaluate*/RuleSet/
// reporter machinery the CLI does, rendering each answer as the JSON bytes an
// MCP client wants. It carries the invoking command (for its context and the
// --lang flag) and the --config flag the server was started with — the project
// resolution mirrors `depdog lsp` / `depdog check` (D7).
type mcpHandler struct {
	cmd        *cobra.Command
	configPath string
}

// newMCPHandler builds the handler over the mcp command's context and flags.
func newMCPHandler(cmd *cobra.Command, configPath string) *mcpHandler {
	return &mcpHandler{cmd: cmd, configPath: configPath}
}

// startDir is the directory a query resolves its project from: the explicit
// path when the tool passed one, else the server's working directory. The
// --config flag (when set) pins the project regardless and takes precedence in
// the resolvers below.
func (h *mcpHandler) startDir(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	return os.Getwd()
}

// Check runs an architecture check and returns the exact `--format json` bytes.
// A bare check resolves a single project (from path/--config); all fans out
// over every discovered language project, emitting the aggregate envelope. The
// single-vs-envelope collapse matches `depdog check --format json` exactly: a
// lone analyzed unit with nothing skipped renders as the flat single report.
func (h *mcpHandler) Check(_ context.Context, path string, all bool) ([]byte, error) {
	dir, err := h.startDir(path)
	if err != nil {
		return nil, err
	}

	if all {
		// --config and --all are mutually exclusive on the CLI; honour --all's
		// polyglot fan-out over the resolved directory, ignoring --config. A
		// depdog.work.yaml at the root upgrades the fan-out to the cross-unit
		// work mode, exactly as it does for `depdog check`.
		if wp, ok := config.FindWorkFile(dir); ok {
			run, err := evaluateWorkMode(h.cmd, wp, dir, checkOptions{}, nil)
			if err != nil {
				return nil, err
			}
			return renderCheckJSON(run)
		}
		run, err := evaluateUnits(h.cmd, dir, nil, nil, true)
		if err != nil {
			return nil, err
		}
		return renderCheckJSON(run)
	}

	var ev *evaluation
	if h.configPath != "" {
		ev, err = evaluateModule(h.cmd, h.configPath, nil)
	} else {
		ev, err = evaluateDiscovered(h.cmd, dir)
	}
	if err != nil {
		return nil, err
	}
	run := &checkRun{Members: []memberEval{{Eval: ev}}}
	return renderCheckJSON(run)
}

// renderCheckJSON encodes a resolved run as the `--format json` payload, using
// the same collapse rule as reportCheck: a single analyzed member and no skips
// is the flat single-module report; anything spanning members is the envelope.
// Duration is fixed at 0 so the output is deterministic (the CLI's duration_ms
// varies per run and is regex-scrubbed in the e2e goldens).
func renderCheckJSON(run *checkRun) ([]byte, error) {
	var buf bytes.Buffer
	mods, skipped := run.split()
	if run.CrossResult == nil && len(mods) == 1 && len(skipped) == 0 {
		m := mods[0]
		if err := report.JSON(&buf, m.Result, m.Rules, 0); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	var cross *report.CrossUnit
	if run.CrossResult != nil {
		cross = &report.CrossUnit{Result: run.CrossResult, Work: run.Work}
	}
	if err := report.JSONWorkspace(&buf, run.Root, mods, skipped, cross, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Explain returns the verdict for the edge from → to, matching `depdog explain`
// (the deciding rule/boundary, and file:line for any offending edge already in
// the graph). It loads the graph so it can report positions and resolve
// packages by trailing-segment; can_import is the cheaper, graph-free sibling.
func (h *mcpHandler) Explain(_ context.Context, from, to string) ([]byte, error) {
	ev, err := h.evaluateForQuery()
	if err != nil {
		return nil, err
	}
	views, err := core.BuildPackageViews(ev.Graph, ev.Rules)
	if err != nil {
		return nil, err
	}

	pv, ok := findPackageView(views, from, ev.Result.ModulePath)
	if !ok {
		return nil, fmt.Errorf("no package matches %q", from)
	}
	fromComp := pv.Component

	out := explainResult{
		From:          pv.ImportPath,
		FromComponent: emptyToUnassigned(fromComp),
		To:            to,
	}
	if fromComp == "" {
		out.Allowed = true
		out.DecidedBy = "policy"
		out.Reason = "the source package is unassigned — no rule governs its imports"
		return marshalIndent(out)
	}

	// A boundary crossing is a hard deny that wins over any component allow, so
	// it is decided first — via the same DecideBoundary path check uses, so the
	// verdict never drifts from what check flags. Only in-module package edges
	// carry boundary membership.
	if tpv, ok := findPackageView(views, to, ev.Result.ModulePath); ok {
		allowed, boundary, sealed, berr := ev.Rules.DecideBoundary(
			moduleRelDir(pv.ImportPath, ev.Result.ModulePath),
			moduleRelDir(tpv.ImportPath, ev.Result.ModulePath))
		if berr != nil {
			return nil, berr
		}
		if !allowed {
			out.Allowed = false
			out.DecidedBy = "boundary"
			out.Boundary = boundary
			out.Sealed = sealed
			out.Reason = boundaryReason(boundary, sealed)
			out.Explanation = core.Explanation(core.ExplainViolation(core.Violation{
				FromPackage: pv.ImportPath, FromComponent: fromComp,
				ImportPath: tpv.ImportPath, Target: emptyToUnassigned(tpv.Component),
				Reason: boundaryKind(sealed), Boundary: boundary,
			}, ev.Rules))
			out.Positions = edgePositions(ev.Result, pv.ImportPath, tpv.ImportPath)
			return marshalIndent(out)
		}
	}

	target, isModule, err := resolveExplainTarget(to, ev.Rules, views, ev.Result.ModulePath)
	if err != nil {
		return nil, err
	}
	var (
		allowed bool
		reason  string
	)
	if isModule {
		allowed, reason = ev.Rules.DecideModule(fromComp, to)
	} else {
		allowed, reason = ev.Rules.Decide(fromComp, target)
	}
	out.Allowed = allowed
	out.DecidedBy = "rule"
	out.Reason = reason
	out.Target = target
	if !allowed {
		imp := target
		if isModule {
			imp = to
		}
		out.Explanation = core.Explanation(core.ExplainViolation(core.Violation{
			FromPackage: pv.ImportPath, FromComponent: fromComp,
			ImportPath: imp, Target: target,
		}, ev.Rules))
	}
	// Positions only exist for an edge that is actually in the graph.
	if tpv, ok := findPackageView(views, to, ev.Result.ModulePath); ok {
		out.Positions = edgePositions(ev.Result, pv.ImportPath, tpv.ImportPath)
	}
	return marshalIndent(out)
}

// CanImport is the cheap in-loop pre-check: it loads only the compiled rule set
// (config.Load — no graph scan) and asks it whether from may import to. from
// and to are component names, module-relative package dirs, or std/external/
// unassigned refs (to may also be an external module import path). The verdict
// mirrors what check/explain would flag for the same edge, minus positions.
func (h *mcpHandler) CanImport(_ context.Context, from, to string) ([]byte, error) {
	rs, err := h.loadRuleSet()
	if err != nil {
		return nil, err
	}

	fromComp, fromRel, err := resolveSource(rs, from)
	if err != nil {
		return nil, err
	}
	out := canImportResult{From: from, To: to}
	if fromComp == "" {
		out.Allowed = true
		out.DecidedBy = "policy"
		out.Reason = "unassigned: no rule governs this package"
		return marshalIndent(out)
	}

	target, isModule, toRel := resolveDestination(rs, to)

	// Boundary first (hard deny), only when both endpoints are in-module dirs.
	if fromRel != "" && toRel != "" {
		allowed, boundary, sealed, berr := rs.DecideBoundary(fromRel, toRel)
		if berr != nil {
			return nil, berr
		}
		if !allowed {
			out.Allowed = false
			out.DecidedBy = "boundary"
			out.Reason = boundaryReason(boundary, sealed)
			out.Explanation = core.Explanation(core.ExplainViolation(core.Violation{
				FromPackage: from, FromComponent: fromComp,
				ImportPath: to, Target: target,
				Reason: boundaryKind(sealed), Boundary: boundary,
			}, rs))
			return marshalIndent(out)
		}
	}

	var (
		allowed bool
		reason  string
	)
	if isModule {
		allowed, reason = rs.DecideModule(fromComp, to)
	} else {
		allowed, reason = rs.Decide(fromComp, target)
	}
	out.Allowed = allowed
	out.DecidedBy = "rule"
	out.Reason = reason
	if !allowed {
		imp := target
		if isModule {
			imp = to
		}
		out.Explanation = core.Explanation(core.ExplainViolation(core.Violation{
			FromPackage: from, FromComponent: fromComp,
			ImportPath: imp, Target: target,
		}, rs))
	}
	return marshalIndent(out)
}

// Config returns the compiled rule set as JSON — the machine-readable form of
// the `depdog config` dump: the default policy, options and every component
// with its patterns, inferred stance and allow/deny refs, plus any boundaries.
func (h *mcpHandler) Config(_ context.Context) ([]byte, error) {
	rs, err := h.loadRuleSet()
	if err != nil {
		return nil, err
	}
	return marshalIndent(buildConfigView(rs))
}

// Components returns just the component list — each component's path patterns
// and inferred stance — as JSON, a lighter resource than the full config.
func (h *mcpHandler) Components(_ context.Context) ([]byte, error) {
	rs, err := h.loadRuleSet()
	if err != nil {
		return nil, err
	}
	view := buildConfigView(rs)
	return marshalIndent(componentsView{Components: view.Components})
}

// evaluateForQuery resolves and evaluates the single project explain/config/
// components consult: the --config module when pinned, else the module
// discovered from the server's working directory (D7).
func (h *mcpHandler) evaluateForQuery() (*evaluation, error) {
	if h.configPath != "" {
		return evaluateModule(h.cmd, h.configPath, nil)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return evaluateDiscovered(h.cmd, cwd)
}

// loadRuleSet resolves and loads just the compiled rule set (no graph) — the
// cheap primitive behind can_import and the config/components resources.
func (h *mcpHandler) loadRuleSet() (*core.RuleSet, error) {
	cfgPath := h.configPath
	if cfgPath == "" {
		language, err := languageFlag(h.cmd)
		if err != nil {
			return nil, err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if _, _, cfgPath, err = resolveProject(cwd, language); err != nil {
			return nil, err
		}
	} else {
		var err error
		if cfgPath, err = filepath.Abs(cfgPath); err != nil {
			return nil, err
		}
	}
	return config.Load(cfgPath)
}

// explainResult is the explain tool's JSON payload: the resolved edge, the
// verdict, and what decided it (a rule, a boundary, or the default policy),
// with positions for any offending edge that exists in the graph.
type explainResult struct {
	From          string `json:"from"`
	FromComponent string `json:"from_component"`
	To            string `json:"to"`
	Target        string `json:"target,omitempty"`
	Allowed       bool   `json:"allowed"`
	DecidedBy     string `json:"decided_by"`
	Reason        string `json:"reason"`
	Boundary      string `json:"boundary,omitempty"`
	Sealed        bool   `json:"sealed,omitempty"`
	// Explanation is the plain-English WHY + fix for a denied edge (additive; the
	// machine-readable reason/decided_by above stay unchanged). Omitted when the
	// edge is allowed — the verdict already says it passes.
	Explanation string         `json:"explanation,omitempty"`
	Positions   []jsonPosition `json:"positions,omitempty"`
}

// canImportResult is the can_import tool's JSON payload: the boolean verdict,
// the deciding facet, and the human-readable reason — the compiled-rule-set
// answer, no positions (no graph is loaded).
type canImportResult struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Allowed   bool   `json:"allowed"`
	DecidedBy string `json:"decided_by"`
	Reason    string `json:"reason"`
	// Explanation is the plain-English WHY + fix for a denied edge (additive;
	// reason/decided_by above stay unchanged). Omitted when the edge is allowed.
	Explanation string `json:"explanation,omitempty"`
}

type jsonPosition struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// configView is the config resource's JSON shape: the default policy, the
// resolved options, the components and any boundaries.
type configView struct {
	Default    string          `json:"default"`
	TestFiles  string          `json:"test_files"`
	Skip       []string        `json:"skip,omitempty"`
	Components []componentView `json:"components"`
	Boundaries []boundaryView  `json:"boundaries"`
}

// componentsView is the components resource's JSON shape.
type componentsView struct {
	Components []componentView `json:"components"`
}

type componentView struct {
	Name     string   `json:"name"`
	Stance   string   `json:"stance"`
	Patterns []string `json:"patterns"`
	Allow    []string `json:"allow,omitempty"`
	Deny     []string `json:"deny,omitempty"`
}

type boundaryView struct {
	Name    string   `json:"name"`
	Sealed  bool     `json:"sealed"`
	Members []string `json:"members"`
}

// buildConfigView renders the compiled rule set into the config resource shape,
// reusing core's own inferred stance and ref string forms so the JSON never
// drifts from `depdog config`. Components and boundaries are already sorted.
func buildConfigView(rs *core.RuleSet) configView {
	out := configView{
		Default:    policyWord(rs.Policy),
		TestFiles:  testFilesWord(rs.TestFiles),
		Skip:       rs.Skip,
		Components: make([]componentView, 0, len(rs.Components)),
		Boundaries: make([]boundaryView, 0, len(rs.Boundaries)),
	}
	for _, c := range rs.Components {
		cv := componentView{
			Name:     c.Name,
			Stance:   stanceWord(rs.Stance(c.Name)),
			Patterns: c.Patterns,
		}
		if r, ok := rs.Rules[c.Name]; ok {
			cv.Allow = refsToStrings(r.Allow)
			cv.Deny = refsToStrings(r.Deny)
		}
		out.Components = append(out.Components, cv)
	}
	for _, b := range rs.Boundaries {
		bv := boundaryView{Name: b.Name, Sealed: b.Sealed, Members: make([]string, 0, len(b.Members))}
		for _, m := range b.Members {
			bv.Members = append(bv.Members, m.Label)
		}
		out.Boundaries = append(out.Boundaries, bv)
	}
	return out
}

// resolveSource maps a can_import `from` to a component name and a module-
// relative dir for the boundary check. A package-dir `from` is its own dir; a
// bare component name resolves to a representative dir derived from the
// component's pattern, so a component-named endpoint is classified into the
// same boundary member its packages would be (without loading the graph).
func resolveSource(rs *core.RuleSet, from string) (component, relDir string, err error) {
	if isComponentName(rs, from) {
		return from, componentRepDir(rs, from), nil
	}
	comp, err := rs.AssignComponent(from)
	if err != nil {
		return "", "", err
	}
	return comp, from, nil
}

// resolveDestination maps a can_import `to` to a rule target: a std/external/
// unassigned literal, a component name, an in-module package dir (resolved to
// its component, returning the dir for the boundary check), or an external
// module import path.
func resolveDestination(rs *core.RuleSet, to string) (target string, isModule bool, relDir string) {
	switch to {
	case "std", "external", "unassigned":
		return to, false, ""
	}
	if isComponentName(rs, to) {
		return to, false, componentRepDir(rs, to)
	}
	// A path-shaped ref that matches no component pattern is treated as an
	// external module (a bare word cannot be a module and is left unassigned).
	if comp, err := rs.AssignComponent(to); err == nil && comp != "" {
		return comp, false, to
	}
	if strings.ContainsAny(to, "/.") {
		return "external module", true, ""
	}
	return "unassigned", false, ""
}

func isComponentName(rs *core.RuleSet, name string) bool {
	for _, c := range rs.Components {
		if c.Name == name {
			return true
		}
	}
	return false
}

// componentRepDir returns a representative module-relative dir for a component:
// the concrete prefix of its first pattern with the glob tail trimmed (e.g.
// "cmd/service-a/**" -> "cmd/service-a", "internal/**" -> "internal"). This lets
// a component-named can_import endpoint be classified for the boundary check
// exactly as its packages would be, without loading the graph. Returns "" when
// no concrete prefix can be derived (a bare or interior glob), in which case the
// boundary check is skipped for that endpoint (same as before).
func componentRepDir(rs *core.RuleSet, name string) string {
	for _, c := range rs.Components {
		if c.Name != name {
			continue
		}
		for _, p := range c.Patterns {
			if d := trimGlobTail(p); d != "" {
				return d
			}
		}
		return ""
	}
	return ""
}

// trimGlobTail drops a trailing "/**" or "/*" from a dir-glob pattern and
// returns the concrete prefix, or "" if what remains is empty or still contains
// glob metacharacters (an interior or bare glob has no single representative
// dir).
func trimGlobTail(pattern string) string {
	p := pattern
	if strings.HasSuffix(p, "/**") {
		p = strings.TrimSuffix(p, "/**")
	} else if strings.HasSuffix(p, "/*") {
		p = strings.TrimSuffix(p, "/*")
	}
	if p == "" || strings.ContainsAny(p, "*?[") {
		return ""
	}
	return p
}

// resolveExplainTarget mirrors report's resolveTarget: it maps explain's `to`
// to a rule target (component / std / external / unassigned / external module).
func resolveExplainTarget(to string, rs *core.RuleSet, views []core.PackageView, module string) (target string, isModule bool, err error) {
	switch to {
	case "std", "external", "unassigned":
		return to, false, nil
	}
	if isComponentName(rs, to) {
		return to, false, nil
	}
	if pv, ok := findPackageView(views, to, module); ok {
		return emptyToUnassigned(pv.Component), false, nil
	}
	if strings.ContainsAny(to, "/.") {
		return "external module", true, nil
	}
	return "", false, fmt.Errorf("cannot resolve %q to a component, package, module, or std/external", to)
}

// findPackageView resolves a target to a package view by exact import path,
// module-relative path, or a unique trailing-segment match — the same
// resolution report.findPackage uses.
func findPackageView(views []core.PackageView, target, module string) (core.PackageView, bool) {
	rel := module + "/" + target
	for _, pv := range views {
		if pv.ImportPath == target || pv.ImportPath == rel {
			return pv, true
		}
	}
	for _, pv := range views {
		if strings.HasSuffix(pv.ImportPath, "/"+target) {
			return pv, true
		}
	}
	return core.PackageView{}, false
}

// edgePositions returns the file:line positions of the offending edge from →
// to, drawn from the check result's violations (the same positions the JSON
// report emits). Empty when the edge is allowed or not in the graph.
func edgePositions(res *core.Result, fromPkg, toPkg string) []jsonPosition {
	for _, v := range res.Violations {
		if v.FromPackage == fromPkg && v.ImportPath == toPkg {
			out := make([]jsonPosition, 0, len(v.Positions))
			for _, p := range v.Positions {
				out = append(out, jsonPosition{File: p.File, Line: p.Line})
			}
			return out
		}
	}
	return nil
}

// moduleRelDir maps a package import path back to its module-relative dir, the
// form boundary patterns match against — the same mapping report.relDir does.
func moduleRelDir(importPath, module string) string {
	switch {
	case module == "" || importPath == module:
		return "."
	case strings.HasPrefix(importPath, module+"/"):
		return strings.TrimPrefix(importPath, module+"/")
	default:
		return importPath
	}
}

func boundaryReason(boundary string, sealed bool) string {
	if sealed {
		return fmt.Sprintf("denied by boundary %q (sealed)", boundary)
	}
	return fmt.Sprintf("denied by boundary %q", boundary)
}

// boundaryKind maps a boundary deny's sealed flag to the ReasonKind the prose
// generator branches on.
func boundaryKind(sealed bool) core.ReasonKind {
	if sealed {
		return core.ReasonBoundarySealed
	}
	return core.ReasonBoundary
}

func refsToStrings(refs []core.Ref) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.String()
	}
	return out
}

func policyWord(p core.Policy) string {
	if p == core.PolicyAllow {
		return "allow"
	}
	return "deny"
}

func stanceWord(p core.Policy) string {
	if p == core.PolicyAllow {
		return "blacklist"
	}
	return "whitelist"
}

// testFilesWord names the test-file mode, matching report's config dump.
func testFilesWord(m core.TestFileMode) string {
	switch m {
	case core.TestSameRules:
		return "same-rules"
	case core.TestRelaxed:
		return "relaxed"
	default:
		return "hybrid"
	}
}

func emptyToUnassigned(s string) string {
	if s == "" {
		return "unassigned"
	}
	return s
}

// marshalIndent encodes v as pretty JSON with a trailing newline, matching the
// json.Encoder output the CLI reporters produce.
func marshalIndent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
