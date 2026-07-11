package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The Matrix tab has one text-input form with three modes: add a component
// (`a` — name + path), re-path the selected component (`p` — a new path glob),
// and rename it (`R` — a new name). Validation lives in the injected hooks (the
// same checks `init` uses), so a bad name or glob is reported inline and the
// form stays open.

type formKind int

const (
	formNone formKind = iota
	formAdd
	formRepath
	formRename
)

// updateForm captures keystrokes while a form is open. Runes extend the focused
// field; tab switches fields in add mode; enter advances name → path then
// submits (add) or submits (re-path); esc cancels.
func (m Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.closeForm()
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyTab, tea.KeyShiftTab:
		if m.matrixForm == formAdd {
			m.formField = 1 - m.formField
		}
	case tea.KeyEnter:
		if m.matrixForm == formAdd && m.formField == 0 && strings.TrimSpace(m.formName) != "" {
			m.formField = 1
			return m, nil
		}
		return m.submitForm()
	case tea.KeyBackspace:
		if m.formField == 0 {
			m.formName = dropLast(m.formName)
		} else {
			m.formPattern = dropLast(m.formPattern)
		}
		m.formErr = ""
	case tea.KeyRunes, tea.KeySpace:
		s := string(msg.Runes)
		if msg.Type == tea.KeySpace {
			s = " "
		}
		if m.formField == 0 {
			m.formName += s
		} else {
			m.formPattern += s
		}
		m.formErr = ""
	}
	return m, nil
}

// submitForm validates-and-writes via the mode's hook. On success it closes the
// form and re-runs the check; on failure it keeps the form open with the error.
func (m Model) submitForm() (tea.Model, tea.Cmd) {
	switch m.matrixForm {
	case formAdd:
		name, pattern := strings.TrimSpace(m.formName), strings.TrimSpace(m.formPattern)
		if name == "" || pattern == "" {
			m.formErr = "both a name and a path pattern are required"
			return m, nil
		}
		if m.addComp == nil {
			m.formErr = "adding is not available in this session"
			return m, nil
		}
		if err := m.addComp(name, pattern); err != nil {
			m.formErr = oneLine(err.Error())
			return m, nil
		}
		m.closeForm()
		m.status = fmt.Sprintf("added component %q — re-running…", name)
		return m, m.startRefresh()

	case formRepath:
		patterns := strings.Fields(m.formPattern)
		if len(patterns) == 0 {
			m.formErr = "a component needs at least one path pattern"
			return m, nil
		}
		if m.repath == nil {
			m.formErr = "re-pathing is not available in this session"
			return m, nil
		}
		if err := m.repath(m.formTarget, patterns); err != nil {
			m.formErr = oneLine(err.Error())
			return m, nil
		}
		target := m.formTarget
		m.closeForm()
		m.status = fmt.Sprintf("re-pathed %q — re-running…", target)
		return m, m.startRefresh()

	case formRename:
		newName := strings.TrimSpace(m.formName)
		if newName == "" {
			m.formErr = "type a new name"
			return m, nil
		}
		if m.rename == nil {
			m.formErr = "renaming is not available in this session"
			return m, nil
		}
		if newName == m.formTarget {
			m.closeForm() // no-op rename
			return m, nil
		}
		if err := m.rename(m.formTarget, newName); err != nil {
			m.formErr = oneLine(err.Error())
			return m, nil
		}
		old := m.formTarget
		m.closeForm()
		m.status = fmt.Sprintf("renamed %q → %q — re-running…", old, newName)
		return m, m.startRefresh()
	}
	return m, nil
}

func (m *Model) closeForm() {
	m.matrixForm = formNone
	m.formTarget, m.formName, m.formPattern, m.formField, m.formErr = "", "", "", 0, ""
}

// formView renders the active form as a small labelled panel with a caret on the
// focused field. In re-path mode the component name is a fixed label, not a
// field.
func (m Model) formView() string {
	field := func(idx int, value string) string {
		box := value
		if idx == m.formField {
			box += "▊"
			return styleActive.Render(box)
		}
		return box
	}

	var lines []string
	switch m.matrixForm {
	case formAdd:
		lines = []string{
			styleTitle.Render("Add component"),
			"",
			styleDim.Render("  name  ") + field(0, m.formName),
			styleDim.Render("  path  ") + field(1, m.formPattern),
			"",
		}
	case formRepath:
		lines = []string{
			styleTitle.Render("Re-path ") + styleTitle.Render(m.formTarget),
			"",
			styleDim.Render("  path  ") + field(1, m.formPattern),
			"",
		}
	case formRename:
		lines = []string{
			styleTitle.Render("Rename ") + styleTitle.Render(m.formTarget),
			"",
			styleDim.Render("  name  ") + field(0, m.formName),
			"",
		}
	}
	switch {
	case m.formErr != "":
		lines = append(lines, styleBad.Render("  "+m.formErr), "")
	case m.matrixForm == formAdd:
		lines = append(lines, styleDim.Render("  e.g. name \"service\", path \"internal/service/**\""), "")
	case m.matrixForm == formRepath:
		lines = append(lines, styleDim.Render("  space-separate multiple globs, e.g. \"internal/api/** internal/rpc/**\""), "")
	case m.matrixForm == formRename:
		lines = append(lines, styleDim.Render("  every allow/deny ref, boundary member and group entry follows the rename"), "")
	}

	hint := "  tab: switch field · enter: next / add · esc: cancel"
	if m.matrixForm == formRepath || m.matrixForm == formRename {
		hint = "  enter: save · esc: cancel"
	}
	return strings.Join(append(lines, styleDim.Render(hint)), "\n")
}

// dropLast removes the last rune of s (backspace over multibyte input).
func dropLast(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(r[:len(r)-1])
}
