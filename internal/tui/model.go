// Package tui is depdog's Bubble Tea interface: an interactive view over a
// core.Result. It is a pure consumer of the engine's types — every number it
// shows is also available from `depdog check --format json` — so it adds
// navigation, not data.
package tui

import (
	"bytes"
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
	// styleCursor marks the Matrix tab's edit cursor — the one cell a toggle acts
	// on — in cyan reverse, distinct from the row bar and the red violation cell.
	styleCursor = lipgloss.NewStyle().Reverse(true).Foreground(lipgloss.Color("6")).Bold(true)
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
	editor    *Editor // visual-editor hooks (staged edits); nil ⇒ read-only
	status    string  // transient message shown in the footer, cleared on any key
	active    tab
	selected  int // highlighted violation on the Violations screen
	selPkg    int // highlighted package on the Packages screen
	// configScroll is the document scroll offset on the Config tab. That tab is a
	// document (a scroll offset), not a list (a selection): up/down move the window
	// over static text, so it has no highlighted row.
	configScroll int
	// matrixSel/matrixCol are the selected row (from-component) and column
	// (import target) on the Matrix tab; up/down and left/right move the edit
	// cursor over the grid.
	matrixSel int
	matrixCol int
	// matrixMode turns the Config tab into the visual rule editor (entered with
	// `m`, left with esc). It is a sub-mode of Config, not a peer tab, so tab and
	// arrow navigation stay consistent across the four real tabs.
	matrixMode bool
	// Staged-editing state: matrixConfig is the in-memory working copy every edit
	// mutates (the grid re-evaluates off it); matrixSaved is the last bytes on disk
	// (updated on Save); matrixEntry is the config from when the editor opened —
	// fixed for the session, the "revert all the way" target. matrixExit shows the
	// save/discard prompt when leaving with unsaved changes.
	matrixConfig []byte
	matrixSaved  []byte
	matrixEntry  []byte
	matrixExit   bool
	// matrixBoundaries toggles the editor to its boundaries overlay (the
	// orthogonal mutual-exclusion axis); matrixBoundSel picks a boundary and
	// matrixMemberSel a member within it (for add/remove).
	matrixBoundaries bool
	matrixBoundSel   int
	matrixMemberSel  int
	// input-form state on the Matrix tab: add a component (`a`), re-path (`p`), or
	// rename (`R`) the selected one. formTarget is the component being acted on
	// (empty for add); formName/formPattern are the editable name/path fields.
	matrixForm  formKind
	formTarget  string
	formName    string
	formPattern string
	formField   int    // add/rename: 0 = name; add/re-path: 1 = pattern
	formErr     string // last submit/validation error, shown in the form
	filter      string
	filtering   bool // capturing keystrokes into filter on the Violations screen
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

// Editor wires the visual rule editor's staged edits. The transformers are pure
// (config bytes → new bytes); the model holds the working copy and re-evaluates
// it with Eval so the grid reflects staged edits, and only Save touches disk.
// Load reads the current on-disk config when the editor opens (the rollback
// snapshot). A nil Editor leaves the editor read-only.
type Editor struct {
	Load func() ([]byte, error)
	Eval func([]byte) (*core.Result, []core.PackageView, *core.RuleSet, error)
	Save func([]byte) error

	SetRule      func(data []byte, from, target, verdict string) ([]byte, error)
	AddComponent func(data []byte, name, pattern string) ([]byte, error)
	Repath       func(data []byte, component string, patterns []string) ([]byte, error)
	Rename       func(data []byte, oldName, newName string) ([]byte, error)
	AddMember    func(data []byte, boundary, member string) ([]byte, error)
	RemoveMember func(data []byte, boundary, member string) ([]byte, error)
}

// WithEditor wires the visual rule editor. Without it the editor is read-only
// (visualize only).
func WithEditor(e Editor) Option {
	return func(m *Model) { m.editor = &e }
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
		m.matrixSel = clamp(m.matrixSel, m.matrixRowCount())
		m.matrixCol = clamp(m.matrixCol, m.matrixColCount())
		m.matrixBoundSel = clamp(m.matrixBoundSel, m.boundaryCount())
		m.matrixMemberSel = clamp(m.matrixMemberSel, m.currentMemberCount())
		m.status = "re-ran: " + plural(len(msg.res.Violations), "violation")
	case tea.KeyMsg:
		m.status = "" // any key dismisses a transient status message
		if m.filtering {
			return m.updateFilter(msg)
		}
		if m.matrixForm != formNone {
			return m.updateForm(msg) // the form swallows every key until esc/submit
		}
		if m.matrixExit {
			return m.updateExitPrompt(msg) // the save/discard prompt swallows keys
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
			if m.matrixMode {
				m.leaveEditorOK() // esc leaves, or raises the save/discard prompt if dirty
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		}
		if m.showHelp {
			return m, nil // the overlay swallows navigation until closed
		}
		switch msg.String() {
		case "tab":
			if m.leaveEditorOK() {
				m.active = (m.active + 1) % numTabs
			}
		case "shift+tab":
			if m.leaveEditorOK() {
				m.active = (m.active + numTabs - 1) % numTabs
			}
		case "right", "l":
			// In the editor left/right move the edit cursor (grid column, or the
			// boundary member); everywhere else they page between tabs.
			switch {
			case m.matrixMode && !m.matrixBoundaries:
				m.matrixCol = clamp(m.matrixCol+1, m.matrixColCount())
			case m.matrixMode:
				m.matrixMemberSel = clamp(m.matrixMemberSel+1, m.currentMemberCount())
			default:
				m.active = (m.active + 1) % numTabs
			}
		case "left", "h":
			switch {
			case m.matrixMode && !m.matrixBoundaries:
				m.matrixCol = clamp(m.matrixCol-1, m.matrixColCount())
			case m.matrixMode:
				m.matrixMemberSel = clamp(m.matrixMemberSel-1, m.currentMemberCount())
			default:
				m.active = (m.active + numTabs - 1) % numTabs
			}
		case "m":
			// Open the visual rule editor (a Config sub-mode); esc leaves it.
			if !m.matrixMode {
				m.openMatrix()
			}
		case "w":
			if m.matrixMode {
				return m.saveEditor()
			}
		case " ", "enter":
			if m.matrixMode && !m.matrixBoundaries {
				return m.toggleCell()
			}
		case "b":
			if m.matrixMode {
				m.matrixBoundaries = !m.matrixBoundaries
			}
		case "a":
			switch {
			case m.matrixMode && m.matrixBoundaries && m.editor != nil && m.currentBoundary() != nil:
				m.matrixForm = formAddMember
				m.formTarget, m.formName, m.formPattern = m.currentBoundary().Name, "", ""
				m.formField, m.formErr = 0, ""
			case m.matrixMode && !m.matrixBoundaries && m.editor != nil:
				m.matrixForm = formAdd
				m.formName, m.formPattern, m.formField, m.formErr = "", "", 0, ""
			}
		case "d":
			if m.matrixMode && m.matrixBoundaries && m.editor != nil {
				return m.removeSelectedMember()
			}
		case "p":
			if m.matrixMode && !m.matrixBoundaries && m.editor != nil && m.selectedComponent() != nil {
				c := m.selectedComponent()
				m.matrixForm = formRepath
				m.formTarget, m.formName, m.formPattern = c.Name, "", strings.Join(c.Patterns, " ")
				m.formField, m.formErr = 1, ""
			}
		case "R":
			if m.matrixMode && !m.matrixBoundaries && m.editor != nil && m.selectedComponent() != nil {
				c := m.selectedComponent()
				m.matrixForm = formRename
				m.formTarget, m.formName, m.formPattern = c.Name, c.Name, ""
				m.formField, m.formErr = 0, ""
			}
		case "1":
			if m.leaveEditorOK() {
				m.active = tabDashboard
			}
		case "2":
			if m.leaveEditorOK() {
				m.active = tabViolations
			}
		case "3":
			if m.leaveEditorOK() {
				m.active = tabPackages
			}
		case "4":
			if m.leaveEditorOK() {
				m.active = tabConfig
			}
		case "/":
			if m.active == tabViolations || m.active == tabPackages {
				m.filtering = true
			}
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "e":
			// Outside the editor `e` opens depdog.yaml in $EDITOR. Inside it, the
			// on-disk file lacks the staged edits, so hand-editing it would fight the
			// in-memory working copy (a later save would clobber it) — save or leave
			// the editor first.
			if m.matrixMode {
				m.status = "save (w) or leave (esc) the editor before hand-editing depdog.yaml"
				return m, nil
			}
			return m, m.openInEditor()
		case "r":
			// The editor re-evaluates every staged edit live, and a disk re-run would
			// replace the staged rule set — so `r` only re-runs outside the editor.
			if m.matrixMode {
				m.status = "the editor already re-evaluates edits live"
				return m, nil
			}
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

// exitMatrix leaves the visual editor, returning the Config tab to its document
// view. A no-op when the editor is not open.
func (m *Model) exitMatrix() {
	m.matrixMode = false
	m.matrixBoundaries = false
	m.matrixExit = false
}

// openMatrix enters the editor and snapshots the on-disk config as the rollback
// point ("before we started").
func (m *Model) openMatrix() {
	m.active = tabConfig
	m.matrixMode, m.matrixBoundaries, m.matrixExit = true, false, false
	if m.editor == nil {
		return
	}
	data, err := m.editor.Load()
	if err != nil {
		m.status = "couldn't read the config: " + oneLine(err.Error())
		return
	}
	m.matrixConfig = data
	m.matrixSaved = append([]byte(nil), data...)
	m.matrixEntry = append([]byte(nil), data...)
}

// matrixDirty reports whether the staged config differs from what's on disk.
func (m Model) matrixDirty() bool {
	return m.editor != nil && !bytes.Equal(m.matrixConfig, m.matrixSaved)
}

// savedSinceOpen reports whether a Save this session changed the file from the
// config the editor opened with — i.e. whether "revert all the way" would do
// anything beyond a plain discard.
func (m Model) savedSinceOpen() bool {
	return m.editor != nil && !bytes.Equal(m.matrixEntry, m.matrixSaved)
}

// stage applies a transformer to the working config and re-evaluates it in
// memory, returning the updated model — nothing touches disk. On a transformer
// (validation/refusal) or eval error the model is returned unchanged with the
// error, so callers can surface it (inline in a form, or on the status line).
func (m Model) stage(transform func([]byte) ([]byte, error)) (Model, error) {
	if m.editor == nil {
		return m, fmt.Errorf("editing not available (read-only session)")
	}
	next, err := transform(m.matrixConfig)
	if err != nil {
		return m, err
	}
	res, pkgs, rules, err := m.editor.Eval(next)
	if err != nil {
		return m, err
	}
	m.matrixConfig = next
	m.setData(res, pkgs)
	m.rules = rules
	m.matrixSel = clamp(m.matrixSel, m.matrixRowCount())
	m.matrixCol = clamp(m.matrixCol, m.matrixColCount())
	m.matrixBoundSel = clamp(m.matrixBoundSel, m.boundaryCount())
	m.matrixMemberSel = clamp(m.matrixMemberSel, m.currentMemberCount())
	return m, nil
}

// applyEdit stages a transformer and reports the result on the status line (used
// by direct actions — the cell toggle and member remove). Forms use stage
// directly so validation errors land in the form.
func (m Model) applyEdit(transform func([]byte) ([]byte, error), desc string) (tea.Model, tea.Cmd) {
	staged, err := m.stage(transform)
	if err != nil {
		m.status = oneLine(err.Error())
		return m, nil
	}
	staged.status = desc + " · unsaved (w save · esc to save/discard)"
	return staged, nil
}

// saveEditor writes the staged config to disk. A no-op with a note when there is
// nothing unsaved.
func (m Model) saveEditor() (tea.Model, tea.Cmd) {
	if !m.matrixDirty() {
		m.status = "no unsaved changes"
		return m, nil
	}
	if err := m.editor.Save(m.matrixConfig); err != nil {
		m.status = "save failed: " + oneLine(err.Error())
		return m, nil
	}
	m.matrixSaved = append([]byte(nil), m.matrixConfig...)
	m.status = "saved depdog.yaml"
	return m, nil
}

// leaveEditorOK prepares to leave the editor. With no unsaved changes it exits
// and returns true; with unsaved changes it raises the save/discard prompt and
// returns false so the caller aborts its navigation.
func (m *Model) leaveEditorOK() bool {
	if !m.matrixMode {
		return true
	}
	if m.matrixDirty() {
		m.matrixExit = true
		return false
	}
	m.exitMatrix()
	return true
}

// exitPromptView renders the save/discard/cancel prompt shown when leaving the
// editor with unsaved changes. It offers the "revert all the way" option only
// when a Save this session moved the file past the config you opened with.
func (m Model) exitPromptView() string {
	discard := "  discard — roll back to the config from when you opened the editor"
	if m.savedSinceOpen() {
		discard = "  discard unsaved edits — roll back to your last save"
	}
	lines := []string{
		styleTitle.Render("Unsaved changes"),
		"",
		styleDim.Render("  You've edited the rules but haven't saved them to depdog.yaml."),
		"",
		"  " + styleGood.Render("s") + styleDim.Render("  save changes and leave"),
		"  " + styleBad.Render("d") + styleDim.Render(discard),
	}
	if m.savedSinceOpen() {
		lines = append(lines, "  "+styleBad.Render("o")+styleDim.Render("  revert all the way — roll back to when you opened the editor (undoes saves too)"))
	}
	return strings.Join(append(lines, "  "+styleDim.Render("c  cancel — keep editing")), "\n")
}

// updateExitPrompt handles the save/discard/cancel prompt shown when leaving the
// editor with unsaved changes. `o` (revert all the way to the config the editor
// opened with) is offered only when a Save this session moved the file past it.
func (m Model) updateExitPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "s": // save and leave
		if err := m.editor.Save(m.matrixConfig); err != nil {
			m.status = "save failed: " + oneLine(err.Error())
			m.matrixExit = false
			return m, nil
		}
		m.matrixSaved = append([]byte(nil), m.matrixConfig...)
		m.exitMatrix()
		m.status = "saved depdog.yaml"
	case "d": // discard unsaved edits — roll back to the last on-disk state
		return m.leaveRolledBackTo(m.matrixSaved, "discarded unsaved changes")
	case "o": // revert all the way to the config the editor opened with
		if m.savedSinceOpen() {
			return m.leaveRolledBackTo(m.matrixEntry, "reverted to the config from when you opened the editor")
		}
		return m.leaveRolledBackTo(m.matrixSaved, "discarded unsaved changes")
	case "c", "esc": // cancel — keep editing
		m.matrixExit = false
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// leaveRolledBackTo resets the working copy to target, writing it to disk when it
// differs from the saved state (so reverting past a mid-session save actually
// restores the file), re-evaluates, and leaves the editor.
func (m Model) leaveRolledBackTo(target []byte, note string) (tea.Model, tea.Cmd) {
	m.matrixConfig = append([]byte(nil), target...)
	if !bytes.Equal(m.matrixSaved, m.matrixConfig) {
		if err := m.editor.Save(m.matrixConfig); err != nil {
			m.status = "revert failed: " + oneLine(err.Error())
			m.matrixExit = false
			return m, nil
		}
	}
	m.matrixSaved = append([]byte(nil), m.matrixConfig...)
	if res, pkgs, rules, err := m.editor.Eval(m.matrixConfig); err == nil {
		m.setData(res, pkgs)
		m.rules = rules
	}
	m.exitMatrix()
	m.status = note
	return m, nil
}

// moveSelection moves the highlighted row on whichever list-bearing screen is
// active, clamped to its bounds. On the Config tab up/down scroll the compiled-
// rules document, unless the visual editor is open — then they move its row (a
// component, or a boundary in the overlay).
func (m *Model) moveSelection(d int) {
	switch m.active {
	case tabViolations:
		m.selected = clamp(m.selected+d, len(m.filteredViolations()))
	case tabPackages:
		m.selPkg = clamp(m.selPkg+d, len(m.filteredPackages()))
	case tabConfig:
		switch {
		case m.matrixMode && m.matrixBoundaries:
			m.matrixBoundSel = clamp(m.matrixBoundSel+d, m.boundaryCount())
			m.matrixMemberSel = 0 // a new boundary resets the member cursor
		case m.matrixMode:
			m.matrixSel = clamp(m.matrixSel+d, m.matrixRowCount())
		default:
			m.configScroll = clampScroll(m.configScroll+d, m.configLineCount(), m.bodyRows())
		}
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
		switch {
		case m.active == tabViolations:
			b.WriteString(m.violationsView())
		case m.active == tabPackages:
			b.WriteString(m.packagesView())
		case m.active == tabConfig && m.matrixMode:
			switch {
			case m.matrixExit:
				b.WriteString(m.exitPromptView())
			case m.matrixForm != formNone:
				b.WriteString(m.formView())
			case m.matrixBoundaries:
				b.WriteString(m.boundariesView())
			default:
				b.WriteString(m.matrixView())
			}
		case m.active == tabConfig:
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
		{"up/down or k/j", "move the selection (or scroll the Config document)"},
		{"m", "Config tab: open the rule matrix editor (esc to leave)"},
		{"─ in the matrix (experimental) ─", ""},
		{"↑↓←→", "move the edit cursor over the grid"},
		{"space", "toggle the cursored edge — allow → deny → default"},
		{"b", "boundaries overlay — ←/→ pick a member, a add, d remove"},
		{"a", "add a new component (name + path) to depdog.yaml"},
		{"p", "re-path the selected component (edit its path glob)"},
		{"R", "rename the selected component (refs follow automatically)"},
		{"w", "save staged edits to depdog.yaml (edits stage in memory until then)"},
		{"esc", "leave the editor — prompts save/discard if there are unsaved edits"},
		{"─────────────────", ""},
		{"/", "filter the list (Violations, Packages)"},
		{"e", "open $EDITOR: the selection (Violations, Packages) or depdog.yaml (Config)"},
		{"r", "re-run the check and refresh every screen"},
		{"esc", "leave the editor, clear a filter, or close this help"},
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
	// The visual rule editor (a Config sub-mode) has its own key hints; esc leaves.
	if m.matrixMode {
		if m.matrixExit {
			h := "unsaved changes — s save · d discard"
			if m.savedSinceOpen() {
				h += " · o revert all"
			}
			return styleWarn.Render(h + " · c cancel")
		}
		switch m.matrixForm {
		case formAddMember:
			return styleDim.Render("type a member (name or glob) · enter add · esc cancel")
		case formRename:
			return styleDim.Render("type the new name · enter save · esc cancel")
		case formRepath:
			return styleDim.Render("type the path glob(s) · enter save · esc cancel")
		case formAdd:
			return styleDim.Render("type to fill fields · tab switch field · enter next/add · esc cancel")
		}
		save := ""
		if m.matrixDirty() {
			save = " · w save"
		}
		if m.matrixBoundaries {
			h := "↑/↓ boundary · ←/→ member · b rules"
			if m.editor != nil {
				h += " · a add · d remove"
			}
			return styleDim.Render(h + save + " · esc exit · ? help")
		}
		hint := "↑↓←→ move cursor · b boundaries"
		if m.editor != nil {
			hint = "↑↓←→ cursor · space toggle · b boundaries · a add · p re-path · R rename"
		}
		return styleDim.Render(hint + save + " · esc exit · ? help")
	}

	if m.active == tabViolations || m.active == tabPackages {
		return styleDim.Render("tab/1-4 switch · ↑/↓ move · / filter · e edit · r re-run · ? help · q quit")
	}
	if m.active == tabConfig {
		return styleDim.Render("tab/1-4 switch · ↑/↓ scroll · m matrix · e edit · r re-run · ? help · q quit")
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
