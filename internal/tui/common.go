package tui

import (
	"errors"

	"github.com/charmbracelet/lipgloss"
)

// ErrAborted is returned when the user cancels a wizard.
var ErrAborted = errors.New("cancelled")

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
