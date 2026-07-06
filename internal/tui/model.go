// Package tui is depdog's Bubble Tea interface: an interactive view over a
// core.Result. It is a pure consumer of the engine's types — every number it
// shows is also available from `depdog check --format json` — so it adds
// navigation, not data.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/matterpale/depdog/internal/core"
)

type tab int

const (
	tabDashboard tab = iota
	tabViolations
	numTabs
)

func (t tab) title() string {
	switch t {
	case tabViolations:
		return "Violations"
	default:
		return "Dashboard"
	}
}

var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Underline(true)
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleBad      = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	styleGood     = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleSelected = lipgloss.NewStyle().Reverse(true)
)

// Model is depdog's root Bubble Tea model.
type Model struct {
	res      *core.Result
	active   tab
	selected int // highlighted violation on the Violations screen
	width    int
	quitting bool
}

// New builds the model over a check result.
func New(res *core.Result) Model { return Model{res: res} }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "tab", "right", "l":
			m.active = (m.active + 1) % numTabs
		case "shift+tab", "left", "h":
			m.active = (m.active + numTabs - 1) % numTabs
		case "1":
			m.active = tabDashboard
		case "2":
			m.active = tabViolations
		case "up", "k":
			if m.active == tabViolations && m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.active == tabViolations && m.selected < len(m.res.Violations)-1 {
				m.selected++
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	switch m.active {
	case tabViolations:
		b.WriteString(m.violationsView())
	default:
		b.WriteString(m.dashboardView())
	}
	b.WriteString("\n\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	title := styleTitle.Render("depdog") + styleDim.Render(" · "+m.res.ModulePath)

	tabs := make([]string, 0, numTabs)
	for t := tab(0); t < numTabs; t++ {
		label := " " + t.title() + " "
		if t == m.active {
			label = styleActive.Render(label)
		} else {
			label = styleDim.Render(label)
		}
		tabs = append(tabs, label)
	}
	bar := strings.Join(tabs, styleDim.Render("|"))

	rule := ""
	if m.width > 0 {
		rule = "\n" + styleDim.Render(strings.Repeat("─", m.width))
	}
	return title + "\n" + bar + rule
}

func (m Model) footer() string {
	return styleDim.Render("tab/1-2 switch · ↑/↓ move · q quit")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
