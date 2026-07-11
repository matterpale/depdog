package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/muesli/termenv"

	"github.com/matterpale/depdog/internal/core"
)

// TestMain forces a plain color profile so rendered views are deterministic and
// assertions can match on unstyled text.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

func fixtureResult() *core.Result {
	return &core.Result{
		ModulePath: "example.test/shop",
		Violations: []core.Violation{
			{
				FromPackage: "example.test/shop/internal/domain", FromComponent: "domain",
				ImportPath: "example.test/shop/internal/repo", Target: "repository",
				Rule: "domain: allow [std]", Positions: []core.Position{{File: "internal/domain/x.go", Line: 4}},
			},
			{
				FromPackage: "example.test/shop/internal/handler", FromComponent: "handler",
				ImportPath: "example.test/shop/internal/service", Target: "service",
				Rule: "handler: allow [domain, std]", Positions: []core.Position{{File: "internal/handler/h.go", Line: 7}},
			},
		},
		Warnings: []core.Warning{{Package: "example.test/shop/internal/util", RelDir: "internal/util"}},
		Components: []core.ComponentStat{
			{Name: "domain", Packages: 1, Edges: 3, Violations: 1},
			{Name: "handler", Packages: 1, Edges: 2, Violations: 1},
		},
		Stats: core.Stats{Packages: 3, Edges: 5},
	}
}

func fixturePkgs() []core.PackageView {
	return []core.PackageView{
		{ImportPath: "example.test/shop/internal/domain", Component: "domain", Imports: []core.ImportView{
			{Path: "fmt", Class: core.ClassStd},
			{Path: "example.test/shop/internal/repo", Class: core.ClassInModule, Component: "repository"},
		}, Importers: []string{"example.test/shop/internal/handler"}},
		{ImportPath: "example.test/shop/internal/handler", Component: "handler", Imports: []core.ImportView{
			{Path: "example.test/shop/internal/domain", Class: core.ClassInModule, Component: "domain"},
			{Path: "example.test/shop/internal/service", Class: core.ClassInModule, Component: "service"},
		}},
	}
}

// fixtureRuleSet is the compiled config the Config tab renders via
// report.RuleSet. It matches the shape of fixtureResult's two components.
func fixtureRuleSet() *core.RuleSet {
	return &core.RuleSet{
		Components: []core.Component{
			{Name: "domain", Patterns: []string{"internal/domain/**"}},
			{Name: "handler", Patterns: []string{"internal/handler/**"}},
		},
		Rules: map[string]core.Rule{
			"domain":  {Allow: []core.Ref{{Kind: core.RefStd}}},
			"handler": {Allow: []core.Ref{{Kind: core.RefComponent, Name: "domain"}, {Kind: core.RefStd}}},
		},
		Policy:    core.PolicyDeny,
		TestFiles: core.TestHybrid,
		Skip:      []string{"internal/legacy/**"},
	}
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestDashboardView(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	for _, want := range []string{
		"depdog", "example.test/shop", "Dashboard", "Violations",
		"✗ 2 violations", "Component", "domain", "handler", "unassigned package",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("dashboard missing %q\n%s", want, v)
		}
	}
	if strings.Contains(v, "\x1b") {
		t.Errorf("ANSI leaked into forced-plain view:\n%q", v)
	}
}

func TestSwitchToViolations(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), runes("2"))
	v := m.View()
	if !strings.Contains(v, "domain → example.test/shop/internal/repo") {
		t.Errorf("violations list missing entry:\n%s", v)
	}
	if !strings.Contains(v, "rule: domain: allow [std]") {
		t.Errorf("detail missing rule text:\n%s", v)
	}
}

func TestViolationSelectionMoves(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), runes("2"))
	if !strings.Contains(m.View(), "internal/domain/x.go:4") {
		t.Errorf("detail should show the first violation's position:\n%s", m.View())
	}
	m = update(m, runes("j")) // down
	if m.selected != 1 {
		t.Fatalf("selected = %d, want 1 after down", m.selected)
	}
	if !strings.Contains(m.View(), "internal/handler/h.go:7") {
		t.Errorf("detail should show the second violation's position:\n%s", m.View())
	}
	m = update(m, runes("j")) // clamp at the last row
	if m.selected != 1 {
		t.Errorf("selected past end = %d, want clamp at 1", m.selected)
	}
}

func TestTabWraps(t *testing.T) {
	m := New(fixtureResult(), fixturePkgs())
	for _, want := range []tab{tabViolations, tabPackages, tabConfig, tabMatrix, tabDashboard} {
		m = update(m, tea.KeyMsg{Type: tea.KeyTab})
		if m.active != want {
			t.Fatalf("tab sequence: active = %d, want %d", m.active, want)
		}
	}
}

func TestTabWrapsBackward(t *testing.T) {
	m := New(fixtureResult(), fixturePkgs())
	for _, want := range []tab{tabMatrix, tabConfig, tabPackages, tabViolations, tabDashboard} {
		m = update(m, tea.KeyMsg{Type: tea.KeyShiftTab})
		if m.active != want {
			t.Fatalf("shift+tab sequence: active = %d, want %d", m.active, want)
		}
	}
}

func TestPackagesView(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), runes("3"))
	v := m.View()
	for _, want := range []string{
		// Both fixture packages are violation sources, so they land in the
		// leading "▸ violations" group rather than their component groups.
		"Packages", "▸ violations", "internal/domain", "internal/handler",
		"imports:", "[repository]", "✗", "imported by:",
		"[external] third-party", // the class legend
	} {
		if !strings.Contains(v, want) {
			t.Errorf("packages view missing %q\n%s", want, v)
		}
	}
	if strings.Contains(v, "\x1b") {
		t.Errorf("ANSI leaked into forced-plain view:\n%q", v)
	}
}

func TestPackageSelectionMoves(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), runes("3"))
	if m.selPkg != 0 {
		t.Fatalf("selPkg = %d, want 0", m.selPkg)
	}
	m = update(m, runes("j"))
	if m.selPkg != 1 {
		t.Fatalf("selPkg = %d, want 1 after down", m.selPkg)
	}
	if !strings.Contains(m.View(), "example.test/shop/internal/service") {
		t.Errorf("detail should show the handler package's imports:\n%s", m.View())
	}
	m = update(m, runes("j")) // clamp at the last package
	if m.selPkg != 1 {
		t.Errorf("selPkg past end = %d, want clamp at 1", m.selPkg)
	}
}

// manyPkgs builds n packages spread across four components, giving every fifth
// package a wide (40-edge) fan-out so the detail pane would overflow a small
// screen if it were not bounded.
func manyPkgs(n int) (*core.Result, []core.PackageView) {
	comps := []string{"a", "b", "c", "d"}
	var pkgs []core.PackageView
	for i := 0; i < n; i++ {
		k := 3
		if i%5 == 0 {
			k = 40
		}
		var imports []core.ImportView
		var importers []string
		for j := 0; j < k; j++ {
			imports = append(imports, core.ImportView{Path: fmt.Sprintf("m/dep%02d", j), Class: core.ClassInModule, Component: "d"})
			importers = append(importers, fmt.Sprintf("m/imp%02d", j))
		}
		pkgs = append(pkgs, core.PackageView{
			ImportPath: fmt.Sprintf("m/pkg%02d", i), Component: comps[i%len(comps)],
			Imports: imports, Importers: importers,
		})
	}
	return &core.Result{ModulePath: "m"}, pkgs
}

func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

// TestPackagesViewFitsHeight pins the core regression: the packages screen must
// never render taller than the terminal, or the alt-screen header scrolls off.
func TestPackagesViewFitsHeight(t *testing.T) {
	res, pkgs := manyPkgs(30)
	m := update(New(res, pkgs), runes("3"))
	const h = 20
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: h})
	v := m.View()
	if got := lineCount(v); got > h {
		t.Fatalf("packages view is %d lines, want <= %d (must fit the terminal):\n%s", got, h, v)
	}
	if !strings.Contains(v, "Packages") || !strings.Contains(v, "depdog") {
		t.Errorf("the header must survive; it is what scrolls off when the body overflows:\n%s", v)
	}
	if !strings.Contains(v, "▼") {
		t.Errorf("a list taller than the screen should scroll, not spill (expected a ▼ marker):\n%s", v)
	}
}

// TestPackagesHeightStableWhileMoving is the anti-skip guarantee: moving the
// selection down the whole list must keep every frame within the terminal, and
// the detail pane must track the current selection.
func TestPackagesHeightStableWhileMoving(t *testing.T) {
	res, pkgs := manyPkgs(30)
	m := update(New(res, pkgs), runes("3"))
	const h = 22
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: h})
	for i := 0; i < len(pkgs); i++ {
		v := m.View()
		if got := lineCount(v); got > h {
			t.Fatalf("at selection %d the view is %d lines, want <= %d:\n%s", i, got, h, v)
		}
		sel := m.filteredPackages()[clamp(m.selPkg, len(pkgs))]
		if want := "── " + sel.ImportPath + " ──"; !strings.Contains(v, want) {
			t.Errorf("detail should track the selection %q at step %d:\n%s", want, i, v)
		}
		m = update(m, runes("j"))
	}
}

// TestPackageDetailTruncates checks a wide fan-out is capped with a summary line
// rather than spilling every edge onto the screen.
func TestPackageDetailTruncates(t *testing.T) {
	var imports []core.ImportView
	for j := 0; j < 40; j++ {
		imports = append(imports, core.ImportView{Path: fmt.Sprintf("m/dep%02d", j), Class: core.ClassInModule, Component: "d"})
	}
	m := update(New(&core.Result{ModulePath: "m"}, []core.PackageView{{ImportPath: "m/big", Component: "c", Imports: imports}}), runes("3"))
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	v := m.View()
	if got := lineCount(v); got > 20 {
		t.Fatalf("view overflows at %d lines:\n%s", got, v)
	}
	if !strings.Contains(v, "more") {
		t.Errorf("a 40-import detail pane should be truncated with a \"… N more\" line:\n%s", v)
	}
	if strings.Contains(v, "m/dep39") {
		t.Errorf("the last of 40 imports should be truncated away on a 20-row screen:\n%s", v)
	}
}

func TestQuit(t *testing.T) {
	next, cmd := New(fixtureResult(), fixturePkgs()).Update(runes("q"))
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
	m := next.(Model)
	if !m.quitting {
		t.Error("q should set quitting")
	}
	if m.View() != "" {
		t.Errorf("quitting view should be empty, got %q", m.View())
	}
}

func TestWindow(t *testing.T) {
	cases := []struct {
		n, sel, max              int
		start, end, above, below int
	}{
		{3, 1, 0, 0, 3, 0, 0},   // unsized: everything
		{3, 1, 5, 0, 3, 0, 0},   // fits: everything
		{10, 0, 3, 0, 3, 0, 7},  // top
		{10, 5, 3, 4, 7, 4, 3},  // middle, centered on sel
		{10, 9, 3, 7, 10, 7, 0}, // bottom, clamped
	}
	for _, c := range cases {
		s, e, a, b := window(c.n, c.sel, c.max)
		if s != c.start || e != c.end || a != c.above || b != c.below {
			t.Errorf("window(%d,%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				c.n, c.sel, c.max, s, e, a, b, c.start, c.end, c.above, c.below)
		}
	}
}

func TestViolationsScroll(t *testing.T) {
	var vs []core.Violation
	for i := 0; i < 20; i++ {
		vs = append(vs, core.Violation{
			FromPackage: "m/from", FromComponent: "c",
			ImportPath: fmt.Sprintf("m/pkg%02d", i), Rule: "r",
			Positions: []core.Position{{File: "f.go", Line: i}},
		})
	}
	m := New(&core.Result{ModulePath: "m", Violations: vs}, nil)
	m = update(m, runes("2"))                               // Violations screen
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 24}) // height 24 -> ~10 rows

	v := m.View()
	if !strings.Contains(v, "▼ 10 more") {
		t.Errorf("expected a below-marker at the top:\n%s", v)
	}
	if strings.Contains(v, "▲") {
		t.Errorf("no above-marker expected at the top:\n%s", v)
	}
	if strings.Contains(v, "m/pkg15") {
		t.Errorf("item 15 should be scrolled out of view:\n%s", v)
	}

	for i := 0; i < 15; i++ { // scroll the selection down
		m = update(m, runes("j"))
	}
	v = m.View()
	if !strings.Contains(v, "▲") {
		t.Errorf("expected an above-marker after scrolling down:\n%s", v)
	}
	if !strings.Contains(v, "m/pkg15") {
		t.Errorf("the selected item should be visible after scrolling:\n%s", v)
	}
}

func TestViolationFilter(t *testing.T) {
	res := &core.Result{ModulePath: "m", Violations: []core.Violation{
		{FromComponent: "domain", FromPackage: "m/domain", ImportPath: "m/repo", Rule: "domain: allow [std]", Positions: []core.Position{{File: "d.go", Line: 1}}},
		{FromComponent: "handler", FromPackage: "m/handler", ImportPath: "m/service", Rule: "handler: allow [domain]", Positions: []core.Position{{File: "h.go", Line: 2}}},
		{FromComponent: "handler", FromPackage: "m/handler", ImportPath: "m/repo", Rule: "handler: allow [domain]", Positions: []core.Position{{File: "h.go", Line: 3}}},
	}}
	m := update(New(res, nil), runes("2")) // Violations screen

	m = update(m, runes("/"))
	if !m.filtering {
		t.Fatal("/ should enter filter mode on the Violations screen")
	}
	// Keys that are normally commands become filter text while filtering.
	for _, r := range "handler" {
		m = update(m, runes(string(r)))
	}
	if m.quitting {
		t.Fatal("typing while filtering must not trigger commands")
	}
	if m.filter != "handler" {
		t.Fatalf("filter = %q, want handler", m.filter)
	}

	v := m.View()
	if !strings.Contains(v, "filter: handler") {
		t.Errorf("active filter indicator missing:\n%s", v)
	}
	if !strings.Contains(v, "handler → m/service") {
		t.Errorf("matching violation should show:\n%s", v)
	}
	if strings.Contains(v, "domain → m/repo") {
		t.Errorf("non-matching violation should be filtered out:\n%s", v)
	}

	// Enter accepts and keeps the filter; esc (via a fresh entry) clears it.
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.filtering || m.filter != "handler" {
		t.Errorf("enter should accept: filtering=%v filter=%q", m.filtering, m.filter)
	}
	m = update(m, runes("/"))
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.filtering || m.filter != "" {
		t.Errorf("esc should clear: filtering=%v filter=%q", m.filtering, m.filter)
	}
}

func TestPackageFilter(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), runes("3")) // Packages screen
	m = update(m, runes("/"))
	if !m.filtering {
		t.Fatal("/ should enter filter mode on the Packages screen")
	}
	for _, r := range "handler" {
		m = update(m, runes(string(r)))
	}
	v := m.View()
	if !strings.Contains(v, "filter: handler") {
		t.Errorf("active filter indicator missing:\n%s", v)
	}
	if !strings.Contains(v, "internal/handler") {
		t.Errorf("matching package should show:\n%s", v)
	}
	// The filter narrows the list itself to the one matching package (the detail
	// pane may still name domain as an import, so assert on the list, not the view).
	if fp := m.filteredPackages(); len(fp) != 1 || fp[0].ImportPath != "example.test/shop/internal/handler" {
		t.Errorf("filter should narrow the package list to handler, got %v", fp)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.filtering || m.filter != "" {
		t.Errorf("esc should clear the filter: filtering=%v filter=%q", m.filtering, m.filter)
	}
	// After clearing, every package is back.
	if !strings.Contains(m.View(), "internal/domain") {
		t.Errorf("clearing the filter should restore all packages:\n%s", m.View())
	}
}

// TestPackagesViolationsFirst pins the ordering: offending packages are pulled
// into a leading "▸ violations" group, above the component groups of the rest.
func TestPackagesViolationsFirst(t *testing.T) {
	res := &core.Result{
		ModulePath: "m",
		Violations: []core.Violation{{
			FromPackage: "m/a", FromComponent: "aaa",
			ImportPath: "m/x", Rule: "aaa: allow [std]",
			Positions: []core.Position{{File: "a.go", Line: 1}},
		}},
	}
	pkgs := []core.PackageView{
		{ImportPath: "m/a", Component: "aaa", Imports: []core.ImportView{{Path: "m/x", Class: core.ClassInModule, Component: "xxx"}}},
		{ImportPath: "m/b", Component: "bbb"},
		{ImportPath: "m/c", Component: "ccc"},
	}
	m := update(New(res, pkgs), runes("3"))
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()

	vi := strings.Index(v, "▸ violations")
	if vi < 0 {
		t.Fatalf("offending packages should be grouped under a ▸ violations header:\n%s", v)
	}
	// The clean packages keep their component grouping, below the violations group.
	for _, comp := range []string{"▸ bbb", "▸ ccc"} {
		ci := strings.Index(v, comp)
		if ci < 0 {
			t.Errorf("clean component group %q should still render:\n%s", comp, v)
		}
		if ci >= 0 && ci < vi {
			t.Errorf("the violations group should come before %q:\n%s", comp, v)
		}
	}
	// The offending package is listed only under violations, not its own component.
	if strings.Contains(v, "▸ aaa") {
		t.Errorf("an offending package should not also appear under its component header:\n%s", v)
	}
}

func TestSwitchToConfig(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet())), runes("4"))
	if m.active != tabConfig {
		t.Fatalf("4 should select the Config tab, active = %d", m.active)
	}
	v := m.View()
	for _, want := range []string{
		"Config", "depdog.yaml", // the active config path
		"default", "deny", "test_files", "hybrid", "components",
		"internal/domain/**", "whitelist", "allow", "std",
		"skip", "internal/legacy/**",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("config view missing %q\n%s", want, v)
		}
	}
	if strings.Contains(v, "\x1b") {
		t.Errorf("ANSI leaked into forced-plain view:\n%q", v)
	}
}

func TestConfigViewWithoutRuleSet(t *testing.T) {
	// No WithConfig: the tab must still render (a hint), never panic.
	m := update(New(fixtureResult(), fixturePkgs()), runes("4"))
	if m.active != tabConfig {
		t.Fatalf("4 should select the Config tab even without a rule set")
	}
	if v := m.View(); v == "" {
		t.Error("config view should not be empty without a rule set")
	}
}

func TestMatrixView(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet())), runes("5"))
	if m.active != tabMatrix {
		t.Fatalf("5 should select the Matrix tab")
	}
	v := m.View()
	// Grid + legend + the focus pane for the default selection (domain, first row).
	for _, want := range []string{"Rule matrix", "Matrix", "domain", "handler", "std", "self", "focus: domain", "allow →"} {
		if !strings.Contains(v, want) {
			t.Errorf("matrix view missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "\x1b") {
		t.Errorf("ANSI leaked into forced-plain view:\n%q", v)
	}

	// The focus pane follows the selection: down one row selects handler, whose
	// allow list names domain.
	v = update(m, runes("j")).View()
	if !strings.Contains(v, "focus: handler") {
		t.Errorf("after ↓ the focus pane should show handler:\n%s", v)
	}
}

func TestMatrixVerdicts(t *testing.T) {
	rs := fixtureRuleSet() // domain: allow[std]; handler: allow[domain, std]; policy deny
	cases := []struct {
		from, to string
		want     cellKind
	}{
		{"domain", "domain", cellSelf},
		{"domain", "std", cellAllow},           // explicit allow
		{"domain", "handler", cellDefaultDeny}, // whitelist stance, not listed
		{"domain", "external", cellDefaultDeny},
		{"handler", "domain", cellAllow}, // explicit allow of a peer
		{"handler", "std", cellAllow},
		{"handler", "unassigned", cellDefaultDeny},
	}
	for _, c := range cases {
		if got := cellVerdict(rs, c.from, c.to); got != c.want {
			t.Errorf("cellVerdict(%s→%s) = %d, want %d", c.from, c.to, got, c.want)
		}
	}
}

func TestMatrixViewWithoutRuleSet(t *testing.T) {
	// No WithConfig: the Matrix tab must still render a hint, never panic.
	m := update(New(fixtureResult(), fixturePkgs()), runes("5"))
	if m.active != tabMatrix {
		t.Fatalf("5 should select the Matrix tab even without a rule set")
	}
	if !strings.Contains(m.View(), "no compiled rule set") {
		t.Errorf("matrix without a rule set should show a hint:\n%s", m.View())
	}
}

func TestNextVerdict(t *testing.T) {
	cases := map[cellKind]string{
		cellDefaultAllow: "allow",
		cellDefaultDeny:  "allow",
		cellAllow:        "deny",
		cellDeny:         "default",
	}
	for k, want := range cases {
		if got := nextVerdict(k); got != want {
			t.Errorf("nextVerdict(%d) = %q, want %q", k, got, want)
		}
	}
}

func TestMatrixToggleWritesVerdict(t *testing.T) {
	var got [3]string
	mk := func() Model {
		return update(New(fixtureResult(), fixturePkgs(),
			WithConfig("depdog.yaml", fixtureRuleSet()),
			WithEdit(func(from, target, verdict string) error { got = [3]string{from, target, verdict}; return nil })),
			runes("5"))
	}
	// domain row, std column (idx 2): explicit allow -> toggling writes deny.
	m := mk()
	m = update(m, tea.KeyMsg{Type: tea.KeyRight})
	m = update(m, tea.KeyMsg{Type: tea.KeyRight})
	update(m, runes(" "))
	if got != [3]string{"domain", "std", "deny"} {
		t.Errorf("explicit-allow domain→std should toggle to deny, got %v", got)
	}
	// domain row, handler column (idx 1): default-deny -> toggling writes allow.
	got = [3]string{}
	m = mk()
	m = update(m, tea.KeyMsg{Type: tea.KeyRight})
	update(m, runes(" "))
	if got != [3]string{"domain", "handler", "allow"} {
		t.Errorf("default domain→handler should toggle to allow, got %v", got)
	}
}

func TestMatrixToggleReadOnlyAndSelf(t *testing.T) {
	// No WithEdit: space is inert and says so, never panics.
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet())), runes("5"))
	m = update(m, runes(" "))
	if !strings.Contains(m.View(), "read-only") {
		t.Errorf("space without an edit hook should report read-only:\n%s", m.View())
	}

	// With an edit hook but the cursor on the diagonal (domain→domain): no write.
	edited := false
	m = update(New(fixtureResult(), fixturePkgs(),
		WithConfig("depdog.yaml", fixtureRuleSet()),
		WithEdit(func(_, _, _ string) error { edited = true; return nil })), runes("5"))
	m = update(m, runes(" ")) // cursor starts at col 0 == domain (self)
	if edited {
		t.Error("toggling a component against itself must not write")
	}
}

func TestMatrixColumnCursorClamps(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet())), runes("5"))
	if m.matrixCol != 0 {
		t.Fatalf("initial matrixCol = %d, want 0", m.matrixCol)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyLeft}) // clamp at 0
	if m.matrixCol != 0 {
		t.Errorf("left at col 0 should clamp, got %d", m.matrixCol)
	}
	// 2 components + 3 special targets = 5 columns; index maxes at 4.
	for i := 0; i < 20; i++ {
		m = update(m, tea.KeyMsg{Type: tea.KeyRight})
	}
	if m.matrixCol != 4 {
		t.Errorf("right past the end should clamp at 4, got %d", m.matrixCol)
	}
}

func fixtureBoundaryRuleSet() *core.RuleSet {
	rs := fixtureRuleSet()
	rs.Boundaries = []core.Boundary{
		{Name: "adapters", Members: []core.BoundaryMember{{Label: "domain"}, {Label: "handler"}}},
		{Name: "cmd-services", Sealed: true, Members: []core.BoundaryMember{{Label: "cmd/a/**"}, {Label: "cmd/b/**"}}},
	}
	return rs
}

func TestBoundariesOverlayToggle(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureBoundaryRuleSet())), runes("5"))
	if m.matrixBoundaries {
		t.Fatal("the Matrix tab should start on the rules grid")
	}
	if !strings.Contains(m.View(), "Rule matrix") {
		t.Errorf("expected the grid before b:\n%s", m.View())
	}

	m = update(m, runes("b"))
	if !m.matrixBoundaries {
		t.Fatal("b should enter the boundaries overlay")
	}
	v := m.View()
	for _, want := range []string{"Boundaries", "adapters", "cmd-services", "sealed", "members", "no member may import"} {
		if !strings.Contains(v, want) {
			t.Errorf("boundaries overlay missing %q:\n%s", want, v)
		}
	}

	m2 := update(m, runes("j"))
	if m2.matrixBoundSel != 1 {
		t.Errorf("j should select the second boundary, got %d", m2.matrixBoundSel)
	}
	if !strings.Contains(m2.View(), "── cmd-services ──") {
		t.Errorf("the detail pane should follow the selection:\n%s", m2.View())
	}

	m = update(m, runes("b"))
	if m.matrixBoundaries || !strings.Contains(m.View(), "Rule matrix") {
		t.Errorf("b should toggle back to the grid")
	}
}

func TestBoundariesOverlayEmpty(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet())), runes("5"))
	m = update(m, runes("b"))
	if !strings.Contains(m.View(), "no boundaries defined") {
		t.Errorf("expected a hint when no boundaries exist:\n%s", m.View())
	}
}

func TestBoundariesOverlayLiveViolation(t *testing.T) {
	res := &core.Result{
		ModulePath: "m",
		Violations: []core.Violation{
			{FromComponent: "domain", ImportPath: "m/h", Target: "handler", Boundary: "adapters", Reason: core.ReasonBoundary},
		},
	}
	m := update(New(res, nil, WithConfig("depdog.yaml", fixtureBoundaryRuleSet())), runes("5"))
	m = update(m, runes("b"))
	if !strings.Contains(m.View(), "1 live boundary violation") {
		t.Errorf("expected a live boundary-violation count for adapters:\n%s", m.View())
	}
}

func TestMatrixSelectionClamps(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", manyComponentRuleSet(40))), runes("5"))
	m = update(m, tea.WindowSizeMsg{Width: 200, Height: 20})

	if m.matrixSel != 0 {
		t.Fatalf("initial matrixSel = %d, want 0", m.matrixSel)
	}
	m = update(m, runes("k")) // up at the top stays clamped
	if m.matrixSel != 0 {
		t.Errorf("up at top: matrixSel = %d, want 0", m.matrixSel)
	}
	m = update(m, runes("j")) // down moves the selection
	if m.matrixSel != 1 {
		t.Errorf("down: matrixSel = %d, want 1", m.matrixSel)
	}
	for i := 0; i < 100; i++ { // move far past the end
		m = update(m, runes("j"))
	}
	if m.matrixSel != 39 {
		t.Errorf("selection past the end should clamp at 39, got %d", m.matrixSel)
	}
	if !strings.Contains(m.View(), "▲") {
		t.Errorf("a 40-row matrix on a 20-row screen, selection at the end, should show an above-marker:\n%s", m.View())
	}
	// The focus pane tracks the selection (comp39 is the last component).
	if !strings.Contains(m.View(), "focus: comp39") {
		t.Errorf("focus pane should follow the selection to comp39:\n%s", m.View())
	}
}

// configLines builds a rule set whose rendered dump is long enough to force the
// document to scroll on a small terminal.
func manyComponentRuleSet(n int) *core.RuleSet {
	rs := &core.RuleSet{Rules: map[string]core.Rule{}, Policy: core.PolicyDeny, TestFiles: core.TestHybrid}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("comp%02d", i)
		rs.Components = append(rs.Components, core.Component{Name: name, Patterns: []string{fmt.Sprintf("internal/%s/**", name)}})
		rs.Rules[name] = core.Rule{Allow: []core.Ref{{Kind: core.RefStd}}}
	}
	return rs
}

func TestConfigScrollClamps(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", manyComponentRuleSet(30))), runes("4"))
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 20})

	// Up at the top stays clamped at offset 0.
	if m.configScroll != 0 {
		t.Fatalf("initial configScroll = %d, want 0", m.configScroll)
	}
	m = update(m, runes("k")) // up
	if m.configScroll != 0 {
		t.Errorf("up at the top should clamp at 0, got %d", m.configScroll)
	}

	// The document is taller than the screen: a ▼ marker must appear.
	v := m.View()
	if !strings.Contains(v, "▼") {
		t.Errorf("a config dump taller than the screen should show a ▼ marker:\n%s", v)
	}
	if got := lineCount(v); got > 20 {
		t.Fatalf("config view is %d lines, want <= 20 (must fit the terminal):\n%s", got, v)
	}

	// Scroll all the way down; the offset must clamp, never runs off the end.
	for i := 0; i < 100; i++ {
		m = update(m, runes("j"))
	}
	v = m.View()
	if got := lineCount(v); got > 20 {
		t.Fatalf("config view overflows after scrolling: %d lines\n%s", got, v)
	}
	if !strings.Contains(v, "▲") {
		t.Errorf("after scrolling to the bottom an ▲ marker should show:\n%s", v)
	}
	// The last component must be visible at the bottom.
	if !strings.Contains(v, "comp29") {
		t.Errorf("the bottom of the document should be reachable:\n%s", v)
	}
	// Header survives (it is what scrolls off if the body overflows).
	if !strings.Contains(v, "depdog") || !strings.Contains(v, "Config") {
		t.Errorf("header must survive scrolling:\n%s", v)
	}
}

func TestConfigScrollResetsOnRefresh(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", manyComponentRuleSet(30)),
		WithRefresh(func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
			return fixtureResult(), fixturePkgs(), fixtureRuleSet(), nil
		})), runes("4"))
	m = update(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	for i := 0; i < 10; i++ {
		m = update(m, runes("j"))
	}
	if m.configScroll == 0 {
		t.Fatal("precondition: the config should be scrolled before the refresh")
	}
	next, cmd := m.Update(runes("r"))
	m = next.(Model)
	m = update(m, cmd().(refreshMsg))
	if m.configScroll != 0 {
		t.Errorf("a refresh should reset the config scroll offset, got %d", m.configScroll)
	}
}

func TestConfigEditOpensConfigFile(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	m := update(New(fixtureResult(), fixturePkgs(), WithRoot("/proj"),
		WithConfig("depdog.yaml", fixtureRuleSet())), runes("4"))
	next, cmd := m.Update(runes("e"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("e on the Config tab with $EDITOR set should return an exec command")
	}
	if m.status != "" {
		t.Errorf("no status message expected on success, got %q", m.status)
	}
	if !m.editedConfig {
		t.Error("the Config tab e should record that the editor was launched from it")
	}
}

func TestConfigEditArgvOpensYamlAtLineOne(t *testing.T) {
	// The argv the Config tab builds points $EDITOR at depdog.yaml, line 1.
	root := "/proj"
	rel := "depdog.yaml"
	argv := editorArgv("vim", filepath.Join(root, rel), 1)
	want := []string{"vim", "+1", filepath.Join(root, rel)}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("config editor argv = %q, want %q", argv, want)
	}
}

func TestConfigEditMissingEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet())), runes("4"))
	next, cmd := m.Update(runes("e"))
	m = next.(Model)
	if cmd != nil {
		t.Error("e without $EDITOR should not launch a process")
	}
	if !strings.Contains(m.status, "$EDITOR is not set") {
		t.Errorf("status should tell the user to set $EDITOR, got %q", m.status)
	}
	if m.editedConfig {
		t.Error("a failed launch should not mark editedConfig")
	}
}

func TestConfigEditAutoRefreshesOnExit(t *testing.T) {
	calls := 0
	m := New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet()),
		WithRefresh(func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
			calls++
			return fixtureResult(), fixturePkgs(), fixtureRuleSet(), nil
		}))
	m = update(m, runes("4"))
	m.editedConfig = true // simulate the Config-tab e having launched the editor

	next, cmd := m.Update(editorFinishedMsg{})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("editor exit from the Config tab should fire a refresh command")
	}
	if !strings.Contains(m.status, "config edited") {
		t.Errorf("status should announce the config re-run, got %q", m.status)
	}
	if m.editedConfig {
		t.Error("the editedConfig flag should reset after firing the refresh")
	}
	msg := cmd()
	if _, ok := msg.(refreshMsg); !ok {
		t.Fatalf("the auto-refresh command returned %T, want refreshMsg", msg)
	}
	if calls != 1 {
		t.Fatalf("refresh callback called %d times, want 1", calls)
	}
}

func TestEditorFinishedNoAutoRefreshWithoutFlag(t *testing.T) {
	// A normal editor exit (from Violations/Packages) must not auto-refresh.
	m := New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet()),
		WithRefresh(func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
			t.Fatal("a non-config editor exit must not trigger a refresh")
			return nil, nil, nil, nil
		}))
	next, cmd := m.Update(editorFinishedMsg{})
	if cmd != nil {
		t.Errorf("editor exit without the config flag should not return a command, got %v", cmd)
	}
	_ = next
}

func TestRefreshDeliversRuleSet(t *testing.T) {
	fresh := fixtureRuleSet()
	fresh.Skip = []string{"internal/generated/**"}
	m := New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", manyComponentRuleSet(2)),
		WithRefresh(func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
			return fixtureResult(), fixturePkgs(), fresh, nil
		}))
	m = update(m, runes("4"))
	if strings.Contains(m.View(), "internal/generated/**") {
		t.Fatal("precondition: the fresh skip pattern should not be shown yet")
	}
	next, cmd := m.Update(runes("r"))
	m = next.(Model)
	m = update(m, cmd().(refreshMsg))
	if !strings.Contains(m.View(), "internal/generated/**") {
		t.Errorf("the Config tab should re-render with the delivered rule set:\n%s", m.View())
	}
}

func TestConfigErrorTruncatedToOneLine(t *testing.T) {
	m := New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet()),
		WithRefresh(func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
			return nil, nil, nil, fmt.Errorf("depdog.yaml: bad pattern\n  line 4: unclosed brace\n  hint: check the glob")
		}))
	m = update(m, runes("4"))
	next, cmd := m.Update(runes("r"))
	m = next.(Model)
	m = update(m, cmd().(refreshMsg))
	// Old data survives.
	if !strings.Contains(m.View(), "internal/domain/**") {
		t.Errorf("old config should survive a failed re-run:\n%s", m.View())
	}
	// The footer status is a single line — no embedded newline.
	if strings.Contains(m.status, "\n") {
		t.Errorf("a multi-line config error must be truncated to one line for the footer, got %q", m.status)
	}
	if !strings.Contains(m.status, "bad pattern") {
		t.Errorf("the truncated error should keep the first, most actionable line, got %q", m.status)
	}
}

func TestHelpOverlay(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), runes("?"))
	if !m.showHelp {
		t.Fatal("? should open the help overlay")
	}
	v := m.View()
	for _, want := range []string{"Keys", "next / previous screen", "toggle this help", "? or esc to close"} {
		if !strings.Contains(v, want) {
			t.Errorf("help overlay missing %q\n%s", want, v)
		}
	}
	// Navigation is swallowed while the overlay is open.
	before := m.active
	m = update(m, runes("2"))
	if m.active != before {
		t.Errorf("navigation should be ignored while help is open")
	}
	// ? toggles it off.
	m = update(m, runes("?"))
	if m.showHelp {
		t.Error("? should close the help overlay")
	}
	// esc closes help without quitting.
	m = update(m, runes("?"))
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if closed := next.(Model); closed.showHelp || closed.quitting || cmd != nil {
		t.Errorf("esc should close help without quitting: showHelp=%v quitting=%v", closed.showHelp, closed.quitting)
	}
	// q quits even with help open.
	m = update(m, runes("?"))
	if _, cmd := m.Update(runes("q")); cmd == nil {
		t.Error("q should quit even from the help overlay")
	}
}

func TestEditorArgv(t *testing.T) {
	cases := []struct {
		editor string
		want   []string
	}{
		{"vim", []string{"vim", "+4", "internal/domain/x.go"}},
		{"nvim", []string{"nvim", "+4", "internal/domain/x.go"}},
		{"vi", []string{"vi", "+4", "internal/domain/x.go"}},
		{"nano", []string{"nano", "+4", "internal/domain/x.go"}},
		{"emacs", []string{"emacs", "+4", "internal/domain/x.go"}},
		{"code", []string{"code", "--goto", "internal/domain/x.go:4", "--wait"}},
		{"/usr/local/bin/code", []string{"/usr/local/bin/code", "--goto", "internal/domain/x.go:4", "--wait"}},
		{"code --new-window", []string{"code", "--new-window", "--goto", "internal/domain/x.go:4", "--wait"}},
		{"subl", []string{"subl", "internal/domain/x.go:4"}},
		{"someeditor", []string{"someeditor", "internal/domain/x.go"}}, // unknown: file only
	}
	for _, c := range cases {
		got := editorArgv(c.editor, "internal/domain/x.go", 4)
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Errorf("editorArgv(%q) = %q, want %q", c.editor, got, c.want)
		}
	}
}

func TestEditMissingEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	m := update(New(fixtureResult(), fixturePkgs()), runes("2"))
	next, cmd := m.Update(runes("e"))
	m = next.(Model)
	if cmd != nil {
		t.Error("e without $EDITOR should not launch a process")
	}
	if !strings.Contains(m.status, "$EDITOR is not set") || !strings.Contains(m.status, "export EDITOR=") {
		t.Errorf("status should tell the user to set $EDITOR, got %q", m.status)
	}
	if !strings.Contains(m.View(), "$EDITOR is not set") {
		t.Errorf("footer should surface the status message:\n%s", m.View())
	}
	// The message clears on the next keypress.
	m = update(m, runes("j"))
	if m.status != "" {
		t.Errorf("status should clear on the next key, got %q", m.status)
	}
}

func TestEditOpensSelectedViolation(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	m := update(New(fixtureResult(), fixturePkgs()), runes("2"))
	m = update(m, runes("j")) // select the second violation
	next, cmd := m.Update(runes("e"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("e with $EDITOR set should return an exec command")
	}
	if m.status != "" {
		t.Errorf("no status message expected on success, got %q", m.status)
	}
}

func TestEditOnPackagesUsesViolationPosition(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	// The domain package has a violation with a recorded position.
	m := update(New(fixtureResult(), fixturePkgs()), runes("3"))
	if _, cmd := m.Update(runes("e")); cmd == nil {
		t.Error("e on a package with a known violation position should open the editor")
	}

	// A package with no violations has no known file position.
	res := &core.Result{ModulePath: "example.test/shop"}
	pkgs := []core.PackageView{{ImportPath: "example.test/shop/internal/clean", Component: "clean"}}
	m2 := update(New(res, pkgs), runes("3"))
	next, cmd := m2.Update(runes("e"))
	m2 = next.(Model)
	if cmd != nil {
		t.Error("e without a known position should not launch a process")
	}
	if !strings.Contains(m2.status, "no known file position") {
		t.Errorf("status should explain the missing position, got %q", m2.status)
	}
}

func TestEditOnDashboardExplains(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	next, cmd := New(fixtureResult(), fixturePkgs()).Update(runes("e"))
	m := next.(Model)
	if cmd != nil {
		t.Error("e on the dashboard should not launch a process")
	}
	if m.status == "" {
		t.Error("e on the dashboard should explain where it works")
	}
}

func TestEditorFinishedError(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), editorFinishedMsg{err: fmt.Errorf("exit status 1")})
	if !strings.Contains(m.status, "exit status 1") {
		t.Errorf("editor failure should surface in the status, got %q", m.status)
	}
	m = update(New(fixtureResult(), fixturePkgs()), editorFinishedMsg{})
	if m.status != "" {
		t.Errorf("clean editor exit should not set a status, got %q", m.status)
	}
}

func TestRerunRefreshesData(t *testing.T) {
	fresh := &core.Result{
		ModulePath: "example.test/shop",
		Violations: []core.Violation{{
			FromPackage: "example.test/shop/internal/handler", FromComponent: "handler",
			ImportPath: "example.test/shop/internal/repo", Rule: "handler: allow [domain]",
			Positions: []core.Position{{File: "internal/handler/h.go", Line: 9}},
		}},
		Components: []core.ComponentStat{{Name: "handler", Packages: 1, Edges: 1, Violations: 1}},
		Stats:      core.Stats{Packages: 1, Edges: 1},
	}
	freshPkgs := []core.PackageView{{ImportPath: "example.test/shop/internal/handler", Component: "handler"}}
	calls := 0
	m := New(fixtureResult(), fixturePkgs(), WithRefresh(func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
		calls++
		return fresh, freshPkgs, fixtureRuleSet(), nil
	}))
	m = update(m, runes("2"))
	m = update(m, runes("j")) // selection on the second (soon out-of-range) row

	next, cmd := m.Update(runes("r"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("r should return a refresh command")
	}
	if !strings.Contains(m.status, "re-running") {
		t.Errorf("status should show the re-run is in flight, got %q", m.status)
	}

	msg := cmd()
	rm, ok := msg.(refreshMsg)
	if !ok {
		t.Fatalf("refresh command returned %T, want refreshMsg", msg)
	}
	if calls != 1 {
		t.Fatalf("refresh callback called %d times, want 1", calls)
	}
	m = update(m, rm)
	if len(m.res.Violations) != 1 {
		t.Fatalf("violations = %d, want 1 after refresh", len(m.res.Violations))
	}
	if m.selected != 0 {
		t.Errorf("selection should clamp to the shorter list, got %d", m.selected)
	}
	if !strings.Contains(m.status, "1 violation") {
		t.Errorf("status should report the fresh count, got %q", m.status)
	}
	if !strings.Contains(m.View(), "handler → example.test/shop/internal/repo") {
		t.Errorf("violations screen should show the fresh data:\n%s", m.View())
	}
	// The violation-edge index is rebuilt too.
	if !m.violEdges[[2]string{"example.test/shop/internal/handler", "example.test/shop/internal/repo"}] {
		t.Error("violEdges should reflect the fresh result")
	}
	if m.violEdges[[2]string{"example.test/shop/internal/domain", "example.test/shop/internal/repo"}] {
		t.Error("stale violEdges entry should be gone")
	}
}

func TestRerunErrorKeepsOldData(t *testing.T) {
	m := New(fixtureResult(), fixturePkgs(), WithRefresh(func() (*core.Result, []core.PackageView, *core.RuleSet, error) {
		return nil, nil, nil, fmt.Errorf("depdog.yaml: bad pattern")
	}))
	m = update(m, runes("2"))
	next, cmd := m.Update(runes("r"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("r should return a refresh command")
	}
	m = update(m, cmd().(refreshMsg))
	if len(m.res.Violations) != 2 {
		t.Errorf("old data should survive a failed re-run, got %d violations", len(m.res.Violations))
	}
	if !strings.Contains(m.status, "bad pattern") || !strings.Contains(m.status, "press r") {
		t.Errorf("status should carry the error and the fix, got %q", m.status)
	}
}

func TestRerunUnavailable(t *testing.T) {
	next, cmd := New(fixtureResult(), fixturePkgs()).Update(runes("r"))
	m := next.(Model)
	if cmd != nil {
		t.Error("r without a refresh hook should not return a command")
	}
	if m.status == "" {
		t.Error("r without a refresh hook should explain itself")
	}
}

func TestHelpListsEditAndRerun(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), runes("?"))
	v := m.View()
	for _, want := range []string{"$EDITOR", "re-run the check"} {
		if !strings.Contains(v, want) {
			t.Errorf("help overlay missing %q\n%s", want, v)
		}
	}
	// The list-screen footer hints at both keys.
	m = update(m, runes("?"))
	m = update(m, runes("2"))
	f := m.View()
	for _, want := range []string{"e edit", "r re-run"} {
		if !strings.Contains(f, want) {
			t.Errorf("violations footer missing %q\n%s", want, f)
		}
	}
}

// TestConfigGoldenFrame pins the Config screen's exact rendered frame. Golden
// frames are deterministic (forced-plain color, fixed window) — regenerate with
// `go test ./internal/tui -update` after an intended layout change, then re-run
// without -update to confirm determinism.
func TestConfigGoldenFrame(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", fixtureRuleSet())), runes("4"))
	m = update(m, tea.WindowSizeMsg{Width: 72, Height: 30})
	golden.RequireEqual(t, []byte(m.View()))
}

// TestConfigGoldenScrolled pins the windowed frame: a long config on a short
// terminal, scrolled part-way, showing the ▲/▼ markers.
func TestConfigGoldenScrolled(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs(), WithConfig("depdog.yaml", manyComponentRuleSet(30))), runes("4"))
	m = update(m, tea.WindowSizeMsg{Width: 72, Height: 20})
	for i := 0; i < 8; i++ {
		m = update(m, runes("j"))
	}
	golden.RequireEqual(t, []byte(m.View()))
}

func TestProgramLifecycle(t *testing.T) {
	tm := teatest.NewTestModel(t, New(fixtureResult(), fixturePkgs()), teatest.WithInitialTermSize(90, 30))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Dashboard")) && bytes.Contains(b, []byte("domain"))
	}, teatest.WithDuration(3*time.Second))

	tm.Send(runes("2"))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("example.test/shop/internal/repo"))
	}, teatest.WithDuration(3*time.Second))

	tm.Send(runes("q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}
