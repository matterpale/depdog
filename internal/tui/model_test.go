package tui

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	for _, want := range []tab{tabViolations, tabPackages, tabDashboard} {
		m = update(m, tea.KeyMsg{Type: tea.KeyTab})
		if m.active != want {
			t.Fatalf("tab sequence: active = %d, want %d", m.active, want)
		}
	}
}

func TestPackagesView(t *testing.T) {
	m := update(New(fixtureResult(), fixturePkgs()), runes("3"))
	v := m.View()
	for _, want := range []string{
		"Packages", "▸ domain", "▸ handler", "internal/domain",
		"imports:", "[repository]", "✗", "imported by:",
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
