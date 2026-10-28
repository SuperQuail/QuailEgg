package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// ConfirmModel is a yes/no confirmation screen.
type ConfirmModel struct {
	title   string
	body    string
	answer  bool
	decided bool
}

// Init implements tea.Model.
func (m ConfirmModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m ConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch strings.ToLower(key.String()) {
	case "y":
		m.answer = true
		m.decided = true
		return m, tea.Quit
	case "n", "q", "esc", "ctrl+c":
		m.decided = true
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model.
func (m ConfirmModel) View() string {
	return titleStyle.Render(m.title) + "\n\n" + m.body + "\n\n" + dimStyle.Render(i18n.T("tui.confirm.hint"))
}

// Confirm asks a yes/no question.
func Confirm(title, body string) (bool, error) {
	program := tea.NewProgram(ConfirmModel{title: title, body: body})
	finalModel, err := program.Run()
	if err != nil {
		return false, err
	}
	final, ok := finalModel.(ConfirmModel)
	if !ok || !final.answered() {
		return false, nil
	}
	return final.answer, nil
}

func (m ConfirmModel) answered() bool { return m.decided }
