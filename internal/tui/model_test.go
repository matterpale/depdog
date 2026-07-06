package tui

import (
	"bytes"
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

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestDashboardView(t *testing.T) {
	m := update(New(fixtureResult()), tea.WindowSizeMsg{Width: 80, Height: 24})
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
	m := update(New(fixtureResult()), runes("2"))
	v := m.View()
	if !strings.Contains(v, "domain → example.test/shop/internal/repo") {
		t.Errorf("violations list missing entry:\n%s", v)
	}
	if !strings.Contains(v, "rule: domain: allow [std]") {
		t.Errorf("detail missing rule text:\n%s", v)
	}
}

func TestViolationSelectionMoves(t *testing.T) {
	m := update(New(fixtureResult()), runes("2"))
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
	m := New(fixtureResult())
	m = update(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.active != tabViolations {
		t.Fatalf("tab did not advance: active = %d", m.active)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.active != tabDashboard {
		t.Errorf("tab did not wrap back to dashboard: active = %d", m.active)
	}
}

func TestQuit(t *testing.T) {
	next, cmd := New(fixtureResult()).Update(runes("q"))
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

func TestProgramLifecycle(t *testing.T) {
	tm := teatest.NewTestModel(t, New(fixtureResult()), teatest.WithInitialTermSize(90, 30))
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
