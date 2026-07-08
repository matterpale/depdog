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
	tabConfig
	numTabs
)

func (t tab) title() string {
	switch t {
	case tabViolations:
		return "Violations"
	case tabPackages:
		return "Packages"
	case tabConfig:
		return "Config"
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
	// styleSelectedBad highlights the selected row when it is an offending package:
	// the selection bar, tinted red so its violation status survives the highlight.
	styleSelectedBad = lipgloss.NewStyle().Reverse(true).Foreground(lipgloss.Color("1")).Bold(true)
)

// Model is depdog's root Bubble Tea model.
type Model struct {
	res       *core.Result
	pkgs      []core.PackageView // violators first, then by component and import path
	rules     *core.RuleSet      // compiled config rendered on the Config tab
	violEdges map[[2]string]bool // (from package, import) of every violation
	violPkgs  map[string]bool    // import paths that are the source of a violation
	root      string             // module root; positions are relative to it
	configRel string             // module-relative config path (stable across refreshes)
	refresh   func() (*core.Result, []core.PackageView, *core.RuleSet, error)
	status    string // transient message shown in the footer, cleared on any key
	active    tab
	selected  int // highlighted violation on the Violations screen
	selPkg    int // highlighted package on the Packages screen
	// configScroll is the document scroll offset on the Config tab. That tab is a
	// document (a scroll offset), not a list (a selection): up/down move the window
	// over static text, so it has no highlighted row.
	configScroll int
	filter       string
	filtering    bool // capturing keystrokes into filter on the Violations screen
	// editedConfig records that the last $EDITOR launch came from the Config tab,
	// so its exit auto-fires the refresh pipeline.
	editedConfig bool
	showHelp     bool
	width        int
	height       int
	quitting     bool
}

// Option configures optional model capabilities.
type Option func(*Model)

// WithRoot sets the module root directory, so `e` can resolve the
// module-relative file positions the engine reports.
func WithRoot(dir string) Option {
	return func(m *Model) { m.root = dir }
}

// WithRefresh wires the `r` key: the hook re-runs the load+check pipeline and
// returns fresh, sorted engine output for every screen — including the compiled
// rule set the Config tab renders (core.Result does not carry it, so the hook
// hands it back alongside).
func WithRefresh(f func() (*core.Result, []core.PackageView, *core.RuleSet, error)) Option {
	return func(m *Model) { m.refresh = f }
}

// WithConfig wires the Config tab: the module-relative config path (stable
// across refreshes) and the compiled rule set to render via report.RuleSet. The
// rule set is later replaced in place by a refresh; the path is not.
func WithConfig(rel string, rs *core.RuleSet) Option {
	return func(m *Model) {
		m.configRel = rel
		m.rules = rs
	}
}

// New builds the model over a check result and its package views.
func New(res *core.Result, pkgs []core.PackageView, opts ...Option) Model {
	var m Model
	m.setData(res, pkgs)
	for _, o := range opts {
		o(&m)
	}
	return m
}

// setData installs a check run's output: package views sorted for display and
// the violation-edge index the Packages screen marks ✗ with. Used both at
// construction and when `r` delivers fresh results.
func (m *Model) setData(res *core.Result, pkgs []core.PackageView) {
	edges := make(map[[2]string]bool, len(res.Violations))
	vpkgs := make(map[string]bool)
	for _, v := range res.Violations {
		edges[[2]string{v.FromPackage, v.ImportPath}] = true
		vpkgs[v.FromPackage] = true
	}
	sorted := append([]core.PackageView(nil), pkgs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		// Offending packages float to the top so the eye lands on them first; the
		// rest keep the component-then-path grouping the list has always used.
		if vi, vj := vpkgs[sorted[i].ImportPath], vpkgs[sorted[j].ImportPath]; vi != vj {
			return vi
		}
		if sorted[i].Component != sorted[j].Component {
			return sorted[i].Component < sorted[j].Component
		}
		return sorted[i].ImportPath < sorted[j].ImportPath
	})
	m.res, m.pkgs, m.violEdges, m.violPkgs = res, sorted, edges, vpkgs
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case editorFinishedMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("editor exited with an error: %v", msg.err)
			m.editedConfig = false
			return m, nil
		}
		// A $EDITOR launched from the Config tab edited depdog.yaml itself: fire
		// the existing refresh so the edited rules take effect on every screen.
		if m.editedConfig {
			m.editedConfig = false
			cmd := m.startRefresh()
			if cmd != nil {
				m.status = "config edited — re-running…"
			}
			return m, cmd
		}
	case refreshMsg:
		if msg.err != nil {
			m.status = "re-run failed: " + oneLine(msg.err.Error()) + " — fix it and press r again"
			return m, nil
		}
		m.setData(msg.res, msg.pkgs)
		if msg.rules != nil {
			m.rules = msg.rules
		}
		m.selected = clamp(m.selected, len(m.filteredViolations()))
		m.selPkg = clamp(m.selPkg, len(m.filteredPackages()))
		m.configScroll = 0 // the fresh document may be shorter; start at the top
		m.status = "re-ran: " + plural(len(msg.res.Violations), "violation")
	case tea.KeyMsg:
		m.status = "" // any key dismisses a transient status message
		if m.filtering {
			return m.updateFilter(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "esc":
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		}
		if m.showHelp {
			return m, nil // the overlay swallows navigation until closed
		}
		switch msg.String() {
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
		case "4":
			m.active = tabConfig
		case "/":
			if m.active == tabViolations || m.active == tabPackages {
				m.filtering = true
			}
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "e":
			return m, m.openInEditor()
		case "r":
			return m, m.startRefresh()
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
// active, clamped to its bounds. The Config tab is a document, not a list: the
// same keys scroll its window, clamped to the last renderable offset.
func (m *Model) moveSelection(d int) {
	switch m.active {
	case tabViolations:
		m.selected = clamp(m.selected+d, len(m.filteredViolations()))
	case tabPackages:
		m.selPkg = clamp(m.selPkg+d, len(m.filteredPackages()))
	case tabConfig:
		m.configScroll = clampScroll(m.configScroll+d, m.configLineCount(), m.bodyRows())
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
	if m.showHelp {
		b.WriteString(helpView())
	} else {
		switch m.active {
		case tabViolations:
			b.WriteString(m.violationsView())
		case tabPackages:
			b.WriteString(m.packagesView())
		case tabConfig:
			b.WriteString(m.configView())
		default:
			b.WriteString(m.dashboardView())
		}
	}
	b.WriteString("\n\n")
	b.WriteString(m.footer())
	return b.String()
}

// helpView renders the full key legend shown by the `?` overlay.
func helpView() string {
	rows := [][2]string{
		{"tab / shift+tab", "next / previous screen"},
		{"1 / 2 / 3 / 4", "Dashboard / Violations / Packages / Config"},
		{"up/down or k/j", "move the selection, or scroll Config"},
		{"/", "filter the list (Violations, Packages)"},
		{"e", "open $EDITOR: the selection (Violations, Packages) or depdog.yaml (Config)"},
		{"r", "re-run the check and refresh every screen"},
		{"esc", "clear filter, or close this help"},
		{"?", "toggle this help"},
		{"q or ctrl+c", "quit"},
	}
	w := 0
	for _, r := range rows {
		if len(r[0]) > w {
			w = len(r[0])
		}
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("Keys") + "\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s   %s\n", w, r[0], styleDim.Render(r[1]))
	}
	return strings.TrimRight(b.String(), "\n")
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
	if m.showHelp {
		return styleDim.Render("? or esc to close")
	}
	if m.status != "" {
		return styleWarn.Render(m.status)
	}
	if m.active == tabViolations || m.active == tabPackages {
		return styleDim.Render("tab/1-4 switch · ↑/↓ move · / filter · e edit · r re-run · ? help · q quit")
	}
	if m.active == tabConfig {
		return styleDim.Render("tab/1-4 switch · ↑/↓ scroll · e edit depdog.yaml · r re-run · ? help · q quit")
	}
	return styleDim.Render("tab/1-4 switch · ↑/↓ move · r re-run · ? help · q quit")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// oneLine collapses a possibly multi-line message to its first line, so a
// multi-line config error still fits the single-line footer.
func oneLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
