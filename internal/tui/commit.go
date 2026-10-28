package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SuperQuail/QuailEgg/internal/changes"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/semver"
)

// Draft is the neutral result of the commit wizard.
type Draft struct {
	Message  string
	Type     string
	Scope    string
	Body     string
	Packages map[string]semver.Bump
}

// CommitModel is the five step change record wizard: commit message, change
// record text, packages, bump levels and confirmation.
type CommitModel struct {
	step     int
	message  textinput.Model
	body     textinput.Model
	packages []string
	selected map[string]bool
	bumps    map[string]semver.Bump
	cursor   int
	err      string
	done     bool
	aborted  bool
	draft    Draft
}

// NewCommitModel builds the wizard for the given packages.
func NewCommitModel(packages []string, initial Draft) CommitModel {
	message := textinput.New()
	message.Placeholder = i18n.T("tui.commit.placeholder")
	message.CharLimit = 200
	message.Width = 60
	message.SetValue(initial.Message)
	message.Focus()

	body := textinput.New()
	body.Placeholder = i18n.T("tui.commit.body_placeholder")
	body.CharLimit = 200
	body.Width = 60
	body.SetValue(initial.Body)

	selected := map[string]bool{}
	bumps := map[string]semver.Bump{}
	for _, name := range packages {
		selected[name] = true
		bumps[name] = semver.Patch
	}
	for name, bump := range initial.Packages {
		selected[name] = true
		bumps[name] = bump
	}
	return CommitModel{
		message:  message,
		body:     body,
		packages: packages,
		selected: selected,
		bumps:    bumps,
		draft:    initial,
	}
}

// Init implements tea.Model.
func (m CommitModel) Init() tea.Cmd { return textinput.Blink }

// Update implements tea.Model.
func (m CommitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		switch m.step {
		case 0:
			m.message, cmd = m.message.Update(msg)
		case 1:
			m.body, cmd = m.body.Update(msg)
		}
		return m, cmd
	}

	// While a text field is focused it owns the keyboard. Only the keys that
	// leave the field are intercepted here; space, the arrow keys and letters
	// such as j/k must reach the input instead of the shortcuts below.
	if m.typing() {
		switch key.String() {
		case "ctrl+c":
			m.aborted = true
			return m, tea.Quit
		case "esc":
			if m.step == 0 {
				m.aborted = true
				return m, tea.Quit
			}
			m.body.Blur()
			m.step = 0
			return m, m.message.Focus()
		case "enter":
			return m.advance()
		}
		var cmd tea.Cmd
		switch m.step {
		case 0:
			m.message, cmd = m.message.Update(msg)
		default:
			m.body, cmd = m.body.Update(msg)
		}
		return m, cmd
	}

	switch key.String() {
	case "ctrl+c", "esc":
		m.step--
		switch m.step {
		case 0:
			return m, m.message.Focus()
		case 1:
			return m, m.body.Focus()
		}
		return m, nil
	case "enter":
		return m.advance()
	case "up", "k":
		if m.listVisible() && m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		if m.listVisible() && m.cursor < m.listLen()-1 {
			m.cursor++
		}
		return m, nil
	case "left", "right", "tab":
		if m.step == 3 {
			if name := m.cursorPackage(); name != "" {
				m.bumps[name] = cycleBump(m.bumps[name], key.String() == "left" || key.String() == "tab")
			}
		}
		return m, nil
	case " ":
		if m.step == 2 && m.cursor < len(m.packages) {
			name := m.packages[m.cursor]
			m.selected[name] = !m.selected[name]
		}
		return m, nil
	}
	return m, nil
}

// typing reports whether a text field is focused.
func (m CommitModel) typing() bool { return m.step == 0 || m.step == 1 }

// listVisible reports whether a package list is on screen, meaning the cursor
// shortcuts should move through it.
func (m CommitModel) listVisible() bool { return m.step == 2 || m.step == 3 }

// selectedPackages lists the ticked packages in configuration order.
func (m CommitModel) selectedPackages() []string {
	out := make([]string, 0, len(m.packages))
	for _, name := range m.packages {
		if m.selected[name] {
			out = append(out, name)
		}
	}
	return out
}

// listLen is the number of rows the cursor can move through on this step.
func (m CommitModel) listLen() int {
	if m.step == 3 {
		return len(m.selectedPackages())
	}
	return len(m.packages)
}

// cursorPackage is the package under the cursor on the current list step.
func (m CommitModel) cursorPackage() string {
	items := m.packages
	if m.step == 3 {
		items = m.selectedPackages()
	}
	if m.cursor < 0 || m.cursor >= len(items) {
		return ""
	}
	return items[m.cursor]
}

func (m CommitModel) advance() (tea.Model, tea.Cmd) {
	switch m.step {
	case 0:
		value := strings.TrimSpace(m.message.Value())
		if value == "" {
			m.err = i18n.T("tui.commit.err_body")
			return m, nil
		}
		_, _, description, ok := changes.SplitConventional(value)
		if !ok {
			m.err = i18n.T("commit.err.conventional")
			return m, nil
		}
		m.err = ""
		if strings.TrimSpace(m.body.Value()) == "" {
			m.body.SetValue(description)
		}
		m.message.Blur()
		m.step = 1
		return m, m.body.Focus()
	case 1:
		m.body.Blur()
		m.step = 2
		m.cursor = 0
	case 2:
		if !m.anySelected() {
			m.err = i18n.T("tui.commit.err_packages")
			return m, nil
		}
		m.err = ""
		m.step = 3
		m.cursor = 0
	case 3:
		m.step = 4
	case 4:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func cycleBump(current semver.Bump, backwards bool) semver.Bump {
	order := []semver.Bump{semver.Patch, semver.Minor, semver.Major}
	index := 0
	for i, item := range order {
		if item == current {
			index = i
		}
	}
	if backwards {
		index = (index + len(order) - 1) % len(order)
	} else {
		index = (index + 1) % len(order)
	}
	return order[index]
}

func (m CommitModel) anySelected() bool {
	for _, ok := range m.selected {
		if ok {
			return true
		}
	}
	return false
}

// View implements tea.Model.
func (m CommitModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("tui.commit.title")) + "\n\n")
	switch m.step {
	case 0:
		b.WriteString(i18n.T("tui.commit.step1") + "\n\n")
		b.WriteString(m.message.View() + "\n")
	case 1:
		b.WriteString(i18n.T("tui.commit.step2") + "\n\n")
		b.WriteString(m.body.View() + "\n")
	case 2:
		b.WriteString(i18n.T("tui.commit.step3") + "\n\n")
		for i, name := range m.packages {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}
			mark := "[ ]"
			if m.selected[name] {
				mark = okStyle.Render("[x]")
			}
			b.WriteString(cursor + mark + " " + name + "\n")
		}
	case 3:
		b.WriteString(i18n.T("tui.commit.step4") + "\n\n")
		for i, name := range m.selectedPackages() {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}
			b.WriteString(cursor + fmt.Sprintf("%-24s %s", name, m.bumps[name]) + "\n")
		}
	case 4:
		b.WriteString(i18n.T("tui.commit.step5") + "\n\n")
		b.WriteString(i18n.Tf("tui.commit.message", m.message.Value()) + "\n")
		if strings.TrimSpace(m.body.Value()) == "" {
			b.WriteString(dimStyle.Render(i18n.T("tui.commit.body_same")) + "\n")
		} else {
			b.WriteString(i18n.Tf("tui.commit.body", m.body.Value()) + "\n")
		}
		b.WriteString("\n")
		for _, name := range m.packages {
			if m.selected[name] {
				b.WriteString("- " + name + ": " + string(m.bumps[name]) + "\n")
			}
		}
	}
	if m.err != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.err) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render(i18n.T("tui.commit.hint")))
	return b.String()
}

// RunCommit runs the wizard and returns the filled draft.
func RunCommit(packages []string, initial Draft) (Draft, error) {
	model := NewCommitModel(packages, initial)
	program := tea.NewProgram(model)
	finalModel, err := program.Run()
	if err != nil {
		return Draft{}, err
	}
	final, ok := finalModel.(CommitModel)
	if !ok || final.aborted || !final.done {
		return Draft{}, ErrAborted
	}
	draft := final.draft
	draft.Message = strings.TrimSpace(final.message.Value())
	draft.Body = strings.TrimSpace(final.body.Value())
	draft.Packages = map[string]semver.Bump{}
	for name, ok := range final.selected {
		if ok {
			draft.Packages[name] = final.bumps[name]
		}
	}
	return draft, nil
}
