package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newInitTestModel(count int) InitModel {
	packages := make([]InitPackage, 0, count)
	for i := 0; i < count; i++ {
		packages = append(packages, InitPackage{
			ID:       "npm:pkg" + string(rune('a'+i)),
			Name:     "pkg" + string(rune('a'+i)),
			Resolver: "npm",
			Path:     "pkg" + string(rune('a'+i)),
			Version:  "1.0.0",
		})
	}
	return NewInitModel(InitChoices{Mode: "pr", Packages: packages})
}

func sendInit(m InitModel, msg tea.Msg) InitModel {
	updated, _ := m.Update(msg)
	next, ok := updated.(InitModel)
	if !ok {
		return m
	}
	return next
}

func TestInitWizardSelectsAllPackagesByDefault(t *testing.T) {
	m := newInitTestModel(3)
	if m.step != initStepPackages {
		t.Fatalf("step = %d, want the package step", m.step)
	}
	if got := m.selectedCount(); got != 3 {
		t.Fatalf("selected = %d, want 3", got)
	}
	view := m.View()
	for _, name := range []string{"pkga", "pkgb", "pkgc"} {
		if !strings.Contains(view, name) {
			t.Fatalf("view is missing %q:\n%s", name, view)
		}
	}
	m = sendInit(m, spaceKey())
	if got := m.selectedCount(); got != 2 {
		t.Fatalf("selected after toggle = %d, want 2", got)
	}
	if m.choices.Selected["npm:pkga"] {
		t.Fatal("space did not clear the first package")
	}
}

func TestInitWizardPreselectsGivenPackages(t *testing.T) {
	m := NewInitModel(InitChoices{
		Mode: "pr",
		Packages: []InitPackage{
			{ID: "npm:a", Name: "a"},
			{ID: "npm:b", Name: "b"},
		},
		Selected: map[string]bool{"npm:b": true},
	})
	if got := m.selectedCount(); got != 1 {
		t.Fatalf("selected = %d, want 1", got)
	}
	if !m.choices.Selected["npm:b"] || m.choices.Selected["npm:a"] {
		t.Fatalf("preselection = %v", m.choices.Selected)
	}
}

func TestInitWizardSkipsPackageStepWithoutCandidates(t *testing.T) {
	if m := NewInitModel(InitChoices{Mode: "pr"}); m.step != initStepOptions {
		t.Fatalf("empty wizard step = %d, want options", m.step)
	}
	if m := newInitTestModel(1); m.step != initStepOptions {
		t.Fatalf("single candidate step = %d, want options", m.step)
	}
}

func TestInitWizardMovesAndScrolls(t *testing.T) {
	m := newInitTestModel(15)
	for i := 0; i < 12; i++ {
		m = sendInit(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != 12 {
		t.Fatalf("cursor = %d, want 12", m.cursor)
	}
	if m.offset != 3 {
		t.Fatalf("offset = %d, want 3", m.offset)
	}
	view := m.View()
	if !strings.Contains(view, "pkgm") {
		t.Fatalf("view is missing the cursor row:\n%s", view)
	}
	if strings.Contains(view, "pkga ") {
		t.Fatalf("view still renders the first row:\n%s", view)
	}
	for i := 0; i < 12; i++ {
		m = sendInit(m, tea.KeyMsg{Type: tea.KeyUp})
	}
	if m.cursor != 0 || m.offset != 0 {
		t.Fatalf("cursor/offset = %d/%d, want 0/0", m.cursor, m.offset)
	}
}

func TestInitWizardEnterAndBack(t *testing.T) {
	m := newInitTestModel(2)
	m = sendInit(m, enterKey())
	if m.step != initStepOptions {
		t.Fatalf("step = %d, want options", m.step)
	}
	m = sendInit(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.step != initStepPackages || m.aborted {
		t.Fatalf("step/aborted = %d/%v, want the package step", m.step, m.aborted)
	}
	m = sendInit(m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.aborted {
		t.Fatal("esc on the first step must cancel the wizard")
	}
}

func TestInitWizardTogglesOptions(t *testing.T) {
	m := NewInitModel(InitChoices{Mode: "pr", WithHook: true})
	if m.step != initStepOptions {
		t.Fatalf("step = %d, want options", m.step)
	}
	m = sendInit(m, spaceKey())
	if m.choices.Mode != "commit" {
		t.Fatalf("mode = %q, want commit", m.choices.Mode)
	}
	m = sendInit(m, tea.KeyMsg{Type: tea.KeyDown})
	m = sendInit(m, spaceKey())
	if m.choices.WithHook {
		t.Fatal("space did not toggle the hook option")
	}
	m = sendInit(m, enterKey())
	if !m.done {
		t.Fatal("enter on the options step must finish the wizard")
	}
}

func TestInitWizardQuits(t *testing.T) {
	for _, key := range []tea.Msg{typeKey('q'), tea.KeyMsg{Type: tea.KeyCtrlC}} {
		m := sendInit(newInitTestModel(2), key)
		if !m.aborted {
			t.Fatalf("%v did not cancel the wizard", key)
		}
	}
}
