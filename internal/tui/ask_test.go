package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aclemen1/dossier-cli/internal/dossier"
)

func typeIn(a *ask, s string) {
	for _, r := range s {
		a.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestWaitFormAsksWhomAndWhenThenRuns(t *testing.T) {
	m := &model{}
	r := &row{store: &storeView{root: "/s"}, d: &dossier.Dossier{ID: "D-0017", State: dossier.Waiting, WaitingOn: "Alain", WaitUntil: "2027-01-04T23:59:59+01:00"}}
	m.stateKey("W", r)
	if m.ask == nil || m.ask.fields[0].value != "Alain" {
		t.Fatalf("form %+v", m.ask)
	}
	m.ask.key(tea.KeyMsg{Type: tea.KeyCtrlU})
	typeIn(m.ask, "Patricia")
	if done, _ := m.ask.key(tea.KeyMsg{Type: tea.KeyEnter}); done {
		t.Fatal("the form ended after the first field")
	}
	if done, cmd := m.ask.key(tea.KeyMsg{Type: tea.KeyEnter}); !done || cmd == nil {
		t.Fatal("the form did not run the wait")
	}
	m.stateKey("W", r)
	m.ask.key(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.ask.key(tea.KeyMsg{Type: tea.KeyEnter})
	if _, cmd := m.ask.key(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || !m.statusErr {
		t.Fatal("a wait on nobody was accepted")
	}
	if done, cmd := (&ask{fields: []askField{{}}, done: func([]string) tea.Cmd { return tea.Quit }}).key(tea.KeyMsg{Type: tea.KeyEsc}); !done || cmd != nil {
		t.Fatal("esc should cancel")
	}
}
