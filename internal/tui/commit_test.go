package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SuperQuail/QuailEgg/internal/semver"
)

func typeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func spaceKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
}

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

func send(m CommitModel, msg tea.Msg) CommitModel {
	updated, _ := m.Update(msg)
	next, ok := updated.(CommitModel)
	if !ok {
		return m
	}
	return next
}

func typeString(m CommitModel, s string) CommitModel {
	for _, r := range s {
		if r == ' ' {
			m = send(m, spaceKey())
			continue
		}
		m = send(m, typeKey(r))
	}
	return m
}

// advanceMessage goes from the message step through the record text step.
func advanceMessage(m CommitModel, message, body string) CommitModel {
	m = typeString(m, message)
	m = send(m, enterKey())
	if body != "" {
		m.body.SetValue("")
		m = typeString(m, body)
	}
	return m
}

func TestCommitWizardAcceptsSpacesAndLetters(t *testing.T) {
	m := NewCommitModel([]string{"core"}, Draft{})
	m = typeString(m, "feat(j): add jk cache")
	if got, want := m.message.Value(), "feat(j): add jk cache"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestCommitWizardMovesInputCursor(t *testing.T) {
	m := NewCommitModel([]string{"core"}, Draft{})
	m = send(m, typeKey('a'))
	m = send(m, typeKey('b'))
	m = send(m, tea.KeyMsg{Type: tea.KeyLeft})
	m = send(m, typeKey('X'))
	if got, want := m.message.Value(), "aXb"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyRight})
	m = send(m, typeKey('Y'))
	if got, want := m.message.Value(), "aXbY"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestCommitWizardDerivesRecordTextFromMessage(t *testing.T) {
	m := NewCommitModel([]string{"core"}, Draft{})
	m = advanceMessage(m, "feat(core): tune cache ttl", "")
	if m.step != 1 {
		t.Fatalf("step = %d, want 1", m.step)
	}
	if got, want := m.body.Value(), "tune cache ttl"; got != want {
		t.Fatalf("record text = %q, want %q", got, want)
	}
}

func TestCommitWizardKeepsRecordTextSeparate(t *testing.T) {
	m := NewCommitModel([]string{"core"}, Draft{})
	m = advanceMessage(m, "feat(core): tune ttl", "")
	for i := 0; i < len("tune ttl"); i++ {
		m = send(m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = typeString(m, "raise the default cache ttl to 120s")
	m = send(m, enterKey())
	m = send(m, enterKey())
	m = send(m, enterKey())
	if m.step != 4 {
		t.Fatalf("step = %d, want 4", m.step)
	}
	if got, want := m.message.Value(), "feat(core): tune ttl"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if got, want := m.body.Value(), "raise the default cache ttl to 120s"; got != want {
		t.Fatalf("record text = %q, want %q", got, want)
	}
}

func TestCommitWizardEscReturnsToMessageStep(t *testing.T) {
	m := NewCommitModel([]string{"core"}, Draft{})
	m = typeString(m, "feat(core): tune ttl")
	m = send(m, enterKey())
	m = send(m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.step != 0 {
		t.Fatalf("step = %d, want 0", m.step)
	}
	m = typeString(m, " now")
	if got, want := m.message.Value(), "feat(core): tune ttl now"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestCommitWizardSpaceTogglesSelection(t *testing.T) {
	m := NewCommitModel([]string{"core", "cli"}, Draft{})
	m = advanceMessage(m, "feat: change", "")
	m = send(m, enterKey())
	if m.step != 2 {
		t.Fatalf("step = %d, want 2", m.step)
	}
	m = send(m, spaceKey())
	if m.selected["core"] {
		t.Fatal("space did not toggle the package selection")
	}
}

func TestCommitWizardNavigatesPackagesAtSelectionStep(t *testing.T) {
	m := NewCommitModel([]string{"core", "cli"}, Draft{})
	m = advanceMessage(m, "feat: change", "")
	m = send(m, enterKey())
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", m.cursor)
	}
	m = send(m, spaceKey())
	if m.selected["cli"] {
		t.Fatal("space should toggle the highlighted package")
	}
	if !m.selected["core"] {
		t.Fatal("space toggled the wrong package")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", m.cursor)
	}
	m = send(m, typeKey('j'))
	if m.cursor != 1 {
		t.Fatalf("cursor after j = %d, want 1", m.cursor)
	}
}

func TestCommitWizardBumpStepOnlyShowsSelectedPackages(t *testing.T) {
	m := NewCommitModel([]string{"core", "cli"}, Draft{})
	m = advanceMessage(m, "feat: change", "")
	m = send(m, enterKey())
	if m.step != 2 {
		t.Fatalf("step = %d, want 2", m.step)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	m = send(m, spaceKey())
	m = send(m, enterKey())
	if m.step != 3 {
		t.Fatalf("step = %d, want 3", m.step)
	}
	view := m.View()
	if !strings.Contains(view, "core") {
		t.Fatalf("selected package is missing: %s", view)
	}
	if strings.Contains(view, "cli") {
		t.Fatalf("unselected package is rendered: %s", view)
	}
	if got := m.cursorPackage(); got != "core" {
		t.Fatalf("cursor package = %q, want core", got)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 0 {
		t.Fatalf("cursor moved past the only selected package: %d", m.cursor)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.bumps["core"] != semver.Minor {
		t.Fatalf("bump after right = %s, want minor", m.bumps["core"])
	}
	if m.bumps["cli"] != semver.Patch {
		t.Fatalf("unselected package bump changed to %s", m.bumps["cli"])
	}
}

func TestCommitWizardCyclesBumps(t *testing.T) {
	m := NewCommitModel([]string{"core"}, Draft{})
	m = advanceMessage(m, "feat: change", "")
	m = send(m, enterKey())
	m = send(m, enterKey())
	if m.step != 3 {
		t.Fatalf("step = %d, want 3", m.step)
	}
	if m.bumps["core"] != semver.Patch {
		t.Fatalf("initial bump = %s", m.bumps["core"])
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.bumps["core"] != semver.Minor {
		t.Fatalf("bump after right = %s", m.bumps["core"])
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.bumps["core"] != semver.Patch {
		t.Fatalf("bump after left = %s", m.bumps["core"])
	}
}
