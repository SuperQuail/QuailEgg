package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// InitChoices are the toggles offered by the init wizard.
type InitChoices struct {
	Mode          string
	WithHook      bool
	WithWorkflows bool
	WithStatus    bool
	AddedPackages []string
	Packages      []string
}

// InitModel reviews the detected configuration before writing files.
type InitModel struct {
	choices InitChoices
	cursor  int
	done    bool
	aborted bool
}

// NewInitModel builds the init review screen.
func NewInitModel(choices InitChoices) InitModel {
	return InitModel{choices: choices}
}

// Init implements tea.Model.
func (m InitModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m InitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < 3 {
			m.cursor++
		}
	case " ":
		switch m.cursor {
		case 0:
			if m.choices.Mode == "pr" {
				m.choices.Mode = "commit"
			} else {
				m.choices.Mode = "pr"
			}
		case 1:
			m.choices.WithHook = !m.choices.WithHook
		case 2:
			m.choices.WithWorkflows = !m.choices.WithWorkflows
		case 3:
			m.choices.WithStatus = !m.choices.WithStatus
		}
	case "enter":
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model.
func (m InitModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("tui.init.title")) + "\n\n")
	rows := []string{
		i18n.Tf("tui.init.mode", m.choices.Mode),
		i18n.Tf("tui.init.hook", m.choices.WithHook),
		i18n.Tf("tui.init.workflows", m.choices.WithWorkflows),
		i18n.Tf("tui.init.status", m.choices.WithStatus),
	}
	for i, row := range rows {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		b.WriteString(cursor + row + "\n")
	}
	if len(m.choices.AddedPackages) > 0 {
		b.WriteString("\n" + i18n.Tf("tui.init.added", strings.Join(m.choices.AddedPackages, ", ")) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render(i18n.T("tui.init.hint")))
	return b.String()
}

// RunInit runs the init review screen.
func RunInit(choices InitChoices) (InitChoices, error) {
	model := NewInitModel(choices)
	program := tea.NewProgram(model)
	finalModel, err := program.Run()
	if err != nil {
		return InitChoices{}, err
	}
	final, ok := finalModel.(InitModel)
	if !ok || final.aborted || !final.done {
		return InitChoices{}, ErrAborted
	}
	return final.choices, nil
}
