package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// InitPackage is one package offered by the init wizard.
type InitPackage struct {
	ID       string
	Key      string
	Name     string
	Resolver string
	Path     string
	Version  string
}

// InitChoices are the toggles offered by the init wizard.
type InitChoices struct {
	Mode          string
	WithHook      bool
	WithWorkflows bool
	WithStatus    bool
	// Packages lists the new selectable candidates. It is empty when there is
	// nothing to choose, in which case the wizard starts at the options step.
	Packages []InitPackage
	// Selected maps candidate IDs to their checkbox state. A nil map selects
	// every candidate.
	Selected map[string]bool
	// Registered counts packages that are already configured.
	Registered int
	// Unselectable counts candidates qegg cannot register.
	Unselectable int
}

// Init wizard steps.
const (
	initStepPackages = iota
	initStepOptions
	initListHeight = 10
)

// InitModel reviews the detected configuration before writing files.
type InitModel struct {
	choices InitChoices
	step    int
	cursor  int
	offset  int
	done    bool
	aborted bool
}

// NewInitModel builds the init review screens.
func NewInitModel(choices InitChoices) InitModel {
	if choices.Selected == nil {
		choices.Selected = map[string]bool{}
		for _, pkg := range choices.Packages {
			choices.Selected[pkg.ID] = true
		}
	}
	m := InitModel{choices: choices, step: initStepOptions}
	// A single candidate is already selected, so showing a one-row checkbox
	// list would only make the user press enter twice.
	if len(choices.Packages) > 1 {
		m.step = initStepPackages
	}
	return m
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
	case "esc":
		if m.step == initStepOptions && len(m.choices.Packages) > 1 {
			m.step = initStepPackages
			m.cursor = 0
			m.offset = 0
			return m, nil
		}
		m.aborted = true
		return m, tea.Quit
	}
	if m.step == initStepPackages {
		return m.updatePackages(key)
	}
	return m.updateOptions(key)
}

// updatePackages handles the package selection screen.
func (m InitModel) updatePackages(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.clampOffset()
	case "down", "j":
		if m.cursor < len(m.choices.Packages)-1 {
			m.cursor++
		}
		m.clampOffset()
	case " ":
		if m.cursor < len(m.choices.Packages) {
			id := m.choices.Packages[m.cursor].ID
			m.choices.Selected[id] = !m.choices.Selected[id]
		}
	case "enter":
		m.step = initStepOptions
		m.cursor = 0
	}
	return m, nil
}

// updateOptions handles the four configuration toggles.
func (m InitModel) updateOptions(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
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

// clampOffset keeps the cursor inside the visible window.
func (m *InitModel) clampOffset() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+initListHeight {
		m.offset = m.cursor - initListHeight + 1
	}
	maxOffset := len(m.choices.Packages) - initListHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// selectedCount counts the ticked candidates.
func (m InitModel) selectedCount() int {
	count := 0
	for _, pkg := range m.choices.Packages {
		if m.choices.Selected[pkg.ID] {
			count++
		}
	}
	return count
}

// selectedNames lists the ticked candidates in query order.
func (m InitModel) selectedNames() []string {
	out := make([]string, 0, len(m.choices.Packages))
	for _, pkg := range m.choices.Packages {
		if m.choices.Selected[pkg.ID] {
			out = append(out, pkg.Name)
		}
	}
	return out
}

// View implements tea.Model.
func (m InitModel) View() string {
	if m.step == initStepPackages {
		return m.viewPackages()
	}
	return m.viewOptions()
}

// viewPackages renders the package selection screen.
func (m InitModel) viewPackages() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("tui.init.packages_title")) + "\n\n")
	b.WriteString(i18n.Tf("tui.init.selected_count", m.selectedCount(), len(m.choices.Packages)) + "\n\n")
	end := m.offset + initListHeight
	if end > len(m.choices.Packages) {
		end = len(m.choices.Packages)
	}
	for i := m.offset; i < end; i++ {
		pkg := m.choices.Packages[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		mark := "[ ]"
		if m.choices.Selected[pkg.ID] {
			mark = okStyle.Render("[x]")
		}
		b.WriteString(cursor + mark + " " + i18n.Tf("tui.init.package_line", packageKey(pkg), pkg.Resolver, pkg.Path, packageVersion(pkg.Version)) + "\n")
	}
	if m.choices.Registered > 0 {
		b.WriteString("\n" + dimStyle.Render(i18n.Tf("tui.init.registered_count", m.choices.Registered)) + "\n")
	}
	if m.choices.Unselectable > 0 {
		b.WriteString(dimStyle.Render(i18n.Tf("tui.init.skipped_count", m.choices.Unselectable)) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render(i18n.T("tui.init.packages_hint")))
	return b.String()
}

// viewOptions renders the existing configuration toggles.
func (m InitModel) viewOptions() string {
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
	if names := m.selectedNames(); len(names) > 0 {
		b.WriteString("\n" + i18n.Tf("tui.init.added", strings.Join(names, ", ")) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render(i18n.T("tui.init.hint")))
	return b.String()
}

// packageKey renders the identity key, falling back to the manifest name.
func packageKey(pkg InitPackage) string {
	if pkg.Key != "" {
		return pkg.Key
	}
	return pkg.Name
}

// packageVersion renders an empty version placeholder.
func packageVersion(version string) string {
	if version == "" {
		return "-"
	}
	return version
}

// RunInit runs the init review screens.
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
