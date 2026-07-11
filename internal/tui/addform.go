package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The add-component form is the Matrix tab's structural edit (key `a`): a two
// field prompt (name, path pattern) that appends a new component to depdog.yaml
// via the injected addComp hook, then refreshes so it shows up in the grid.
// Validation lives in the hook (the same checks `init` uses), so a bad name or
// glob is reported inline and the form stays open.

// updateAddForm captures keystrokes while the add-component form is open. Runes
// extend the focused field; tab switches fields; enter advances from the name to
// the path and then submits; esc cancels.
func (m Model) updateAddForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.closeAddForm()
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyTab, tea.KeyShiftTab:
		m.addField = 1 - m.addField
	case tea.KeyEnter:
		if m.addField == 0 && strings.TrimSpace(m.addName) != "" {
			m.addField = 1
			return m, nil
		}
		return m.submitAdd()
	case tea.KeyBackspace:
		if m.addField == 0 {
			m.addName = dropLast(m.addName)
		} else {
			m.addPattern = dropLast(m.addPattern)
		}
		m.addErr = ""
	case tea.KeyRunes, tea.KeySpace:
		s := string(msg.Runes)
		if msg.Type == tea.KeySpace {
			s = " "
		}
		if m.addField == 0 {
			m.addName += s
		} else {
			m.addPattern += s
		}
		m.addErr = ""
	}
	return m, nil
}

// submitAdd validates-and-writes via the addComp hook. On success it closes the
// form and re-runs the check; on failure it keeps the form open with the error.
func (m Model) submitAdd() (tea.Model, tea.Cmd) {
	name, pattern := strings.TrimSpace(m.addName), strings.TrimSpace(m.addPattern)
	if name == "" || pattern == "" {
		m.addErr = "both a name and a path pattern are required"
		return m, nil
	}
	if m.addComp == nil {
		m.addErr = "adding is not available in this session"
		return m, nil
	}
	if err := m.addComp(name, pattern); err != nil {
		m.addErr = oneLine(err.Error())
		return m, nil
	}
	m.closeAddForm()
	cmd := m.startRefresh()
	m.status = fmt.Sprintf("added component %q — re-running…", name)
	return m, cmd
}

func (m *Model) closeAddForm() {
	m.matrixAdding = false
	m.addName, m.addPattern, m.addField, m.addErr = "", "", 0, ""
}

// addFormView renders the add-component form as a small labelled panel with a
// caret on the focused field.
func (m Model) addFormView() string {
	field := func(idx int, value string) string {
		caret := ""
		if idx == m.addField {
			caret = "▊"
		}
		box := value + caret
		if idx == m.addField {
			return styleActive.Render(box)
		}
		return box
	}

	lines := []string{
		styleTitle.Render("Add component"),
		"",
		styleDim.Render("  name  ") + field(0, m.addName),
		styleDim.Render("  path  ") + field(1, m.addPattern),
		"",
	}
	if m.addErr != "" {
		lines = append(lines, styleBad.Render("  "+m.addErr), "")
	} else {
		lines = append(lines, styleDim.Render("  e.g. name \"service\", path \"internal/service/**\""), "")
	}
	lines = append(lines, styleDim.Render("  tab: switch field · enter: next / add · esc: cancel"))
	return strings.Join(lines, "\n")
}

// dropLast removes the last rune of s (backspace over multibyte input).
func dropLast(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(r[:len(r)-1])
}
