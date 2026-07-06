package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/matterpale/depdog/internal/core"
)

// Run opens the interactive UI over a check result and blocks until the user
// quits. It requires a terminal; the caller is responsible for checking that.
func Run(res *core.Result) error {
	_, err := tea.NewProgram(New(res), tea.WithAltScreen()).Run()
	return err
}
