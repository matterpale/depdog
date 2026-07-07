package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/matterpale/depdog/internal/core"
)

// Run opens the interactive UI over a check result and its package views, and
// blocks until the user quits. It requires a terminal; the caller is
// responsible for checking that. Options wire the module root (for `e`) and
// the re-run hook (for `r`).
func Run(res *core.Result, pkgs []core.PackageView, opts ...Option) error {
	_, err := tea.NewProgram(New(res, pkgs, opts...), tea.WithAltScreen()).Run()
	return err
}
