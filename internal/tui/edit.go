package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/matterpale/depdog/internal/core"
)

// editorFinishedMsg reports the $EDITOR process exiting; err is non-nil when it
// failed to start or exited non-zero.
type editorFinishedMsg struct{ err error }

// refreshMsg carries a fresh load+check run back into the model: either new
// data for every screen (including the recompiled rule set the Config tab
// renders), or the error that kept the old data in place.
type refreshMsg struct {
	res   *core.Result
	pkgs  []core.PackageView
	rules *core.RuleSet
	err   error
}

// openInEditor builds the tea.ExecProcess command that suspends the UI and
// opens $EDITOR. On the Config tab it opens depdog.yaml itself at line 1 and
// arms the auto-refresh flag; elsewhere it opens the selected file:line. It
// returns nil — with an actionable status message set — when $EDITOR is unset or
// the selection has no position.
func (m *Model) openInEditor() tea.Cmd {
	m.editedConfig = false
	var (
		file string
		line int
	)
	if m.active == tabConfig {
		file, line = filepath.FromSlash(m.configRel), 1
		if file == "" {
			m.status = "no config path known to edit — restart with `depdog tui`"
			return nil
		}
	} else {
		pos, ok := m.selectedPosition()
		if !ok {
			return nil // selectedPosition set the status
		}
		file, line = filepath.FromSlash(pos.File), pos.Line
	}

	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		m.status = "$EDITOR is not set — export EDITOR=vim (or your editor) and press e again"
		return nil
	}
	if m.root != "" {
		file = filepath.Join(m.root, file)
	}
	argv := editorArgv(editor, file, line)
	c := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- $EDITOR is the user's own choice
	if m.active == tabConfig {
		m.editedConfig = true // its exit auto-fires the refresh pipeline
	}
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorFinishedMsg{err: err} })
}

// selectedPosition resolves the active screen's selection to a source position:
// the first position of the selected violation, or — on the Packages screen —
// the first recorded violation position inside the selected package. When
// nothing can be opened it sets an explanatory status and reports false.
func (m *Model) selectedPosition() (core.Position, bool) {
	switch m.active {
	case tabViolations:
		vs := m.filteredViolations()
		if len(vs) == 0 {
			m.status = "nothing selected — no violation to open"
			return core.Position{}, false
		}
		v := vs[clamp(m.selected, len(vs))]
		if len(v.Positions) == 0 {
			m.status = "no file position recorded for this violation"
			return core.Position{}, false
		}
		return v.Positions[0], true
	case tabPackages:
		pkgs := m.filteredPackages()
		if len(pkgs) == 0 {
			m.status = "nothing selected — no package to open"
			return core.Position{}, false
		}
		p := pkgs[clamp(m.selPkg, len(pkgs))]
		for _, v := range m.res.Violations {
			if v.FromPackage == p.ImportPath && len(v.Positions) > 0 {
				return v.Positions[0], true
			}
		}
		m.status = "no known file position for this package — press e on a violation instead"
		return core.Position{}, false
	default:
		m.status = "e opens the selection in $EDITOR on the Violations or Packages screen"
		return core.Position{}, false
	}
}

// editorArgv builds the argv that opens file at line in the given editor.
// $EDITOR may carry flags ("code --new-window"); the line-number syntax is
// keyed off the command's base name, falling back to just the file for
// editors whose syntax we don't know.
func editorArgv(editor, file string, line int) []string {
	argv := strings.Fields(editor)
	base := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	switch base {
	case "vim", "nvim", "vi", "gvim", "nano", "emacs", "micro":
		return append(argv, fmt.Sprintf("+%d", line), file)
	case "code", "code-insiders", "codium", "cursor":
		return append(argv, "--goto", fmt.Sprintf("%s:%d", file, line), "--wait")
	case "subl", "sublime_text", "hx":
		return append(argv, fmt.Sprintf("%s:%d", file, line))
	default:
		return append(argv, file)
	}
}

// startRefresh kicks off an asynchronous re-run of the load+check pipeline via
// the injected refresh hook. The result arrives later as a refreshMsg.
func (m *Model) startRefresh() tea.Cmd {
	if m.refresh == nil {
		m.status = "re-run is not available here — restart with `depdog tui`"
		return nil
	}
	m.status = "re-running check…"
	refresh := m.refresh
	return func() tea.Msg {
		res, pkgs, rules, err := refresh()
		return refreshMsg{res: res, pkgs: pkgs, rules: rules, err: err}
	}
}
