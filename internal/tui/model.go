// Package tui is depdog's Bubble Tea interface: an interactive view over a
// core.Result. It is a pure consumer of the engine's types — every number it
// shows is also available from `depdog check --format json` — so it adds
// navigation, not data.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/matterpale/depdog/internal/core"
)

type tab int

const (
	tabDashboard tab = iota
	tabViolations
	tabPackages
	numTabs
)

func (t tab) title() string {
	switch t {
	case tabViolations:
		return "Violations"
	case tabPackages:
		return "Packages"
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
	res       *core.Result
	pkgs      []core.PackageView // sorted by component, then import path
	violEdges map[[2]string]bool // (from package, import) of every violation
	active    tab
	selected  int // highlighted violation on the Violations screen
	selPkg    int // highlighted package on the Packages screen
	filter    string
	filtering bool // capturing keystrokes into filter on the Violations screen
	width     int
	height    int
	quitting  bool
}

// New builds the model over a check result and its package views.
func New(res *core.Result, pkgs []core.PackageView) Model {
	sorted := append([]core.PackageView(nil), pkgs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Component != sorted[j].Component {
			return sorted[i].Component < sorted[j].Component
		}
		return sorted[i].ImportPath < sorted[j].ImportPath
	})
	edges := make(map[[2]string]bool, len(res.Violations))
	for _, v := range res.Violations {
		edges[[2]string{v.FromPackage, v.ImportPath}] = true
	}
	return Model{res: res, pkgs: sorted, violEdges: edges}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.filtering {
			return m.updateFilter(msg)
		}
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
		case "3":
			m.active = tabPackages
		case "/":
			if m.active == tabViolations || m.active == tabPackages {
				m.filtering = true
			}
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		}
	}
	return m, nil
}

// updateFilter captures keystrokes into the Violations filter. Enter accepts,
// esc clears and exits, backspace edits. Each edit resets the selection.
func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.filtering = false
	case tea.KeyEsc:
		m.filtering = false
		m.filter = ""
		m.resetSelection()
	case tea.KeyBackspace:
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m.resetSelection()
		}
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyRunes:
		m.filter += string(msg.Runes)
		m.resetSelection()
	}
	return m, nil
}

// resetSelection moves both list selections back to the top, so a filter change
// never leaves the highlight past the end of a narrowed list.
func (m *Model) resetSelection() {
	m.selected = 0
	m.selPkg = 0
}

// moveSelection moves the highlighted row on whichever list-bearing screen is
// active, clamped to its bounds.
func (m *Model) moveSelection(d int) {
	switch m.active {
	case tabViolations:
		m.selected = clamp(m.selected+d, len(m.filteredViolations()))
	case tabPackages:
		m.selPkg = clamp(m.selPkg+d, len(m.filteredPackages()))
	}
}

// filteredViolations returns the violations matching the active filter (a
// case-insensitive substring over component, import and rule), or all of them
// when no filter is set.
func (m Model) filteredViolations() []core.Violation {
	if m.filter == "" {
		return m.res.Violations
	}
	f := strings.ToLower(m.filter)
	var out []core.Violation
	for _, v := range m.res.Violations {
		hay := strings.ToLower(v.FromComponent + " " + v.ImportPath + " " + v.Rule)
		if strings.Contains(hay, f) {
			out = append(out, v)
		}
	}
	return out
}

// filteredPackages returns the package views matching the active filter (a
// case-insensitive substring over import path and component), or all of them
// when no filter is set.
func (m Model) filteredPackages() []core.PackageView {
	if m.filter == "" {
		return m.pkgs
	}
	f := strings.ToLower(m.filter)
	var out []core.PackageView
	for _, p := range m.pkgs {
		if strings.Contains(strings.ToLower(p.ImportPath+" "+p.Component), f) {
			out = append(out, p)
		}
	}
	return out
}

func clamp(i, n int) int {
	switch {
	case n == 0 || i < 0:
		return 0
	case i >= n:
		return n - 1
	default:
		return i
	}
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
	case tabPackages:
		b.WriteString(m.packagesView())
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
	if m.filtering {
		return styleDim.Render("type to filter · enter accept · esc clear")
	}
	if m.active == tabViolations || m.active == tabPackages {
		return styleDim.Render("tab/1-3 switch · ↑/↓ move · / filter · q quit")
	}
	return styleDim.Render("tab/1-3 switch · ↑/↓ move · q quit")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
