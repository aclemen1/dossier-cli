package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aclemen1/dossier-cli/internal/dossier"
)

// ask is a short form on the bottom line: one field after the other, enter to
// go on, esc to cancel.
type ask struct {
	title  string
	fields []askField
	i      int
	done   func(values []string) tea.Cmd
}

type askField struct {
	label, value, hint string
}

func (a *ask) key(k tea.KeyMsg) (finished bool, cmd tea.Cmd) {
	f := &a.fields[a.i]
	switch k.Type {
	case tea.KeyEsc:
		return true, nil
	case tea.KeyEnter:
		if a.i < len(a.fields)-1 {
			a.i++
			return false, nil
		}
		values := make([]string, len(a.fields))
		for i, f := range a.fields {
			values[i] = strings.TrimSpace(f.value)
		}
		return true, a.done(values)
	case tea.KeyBackspace:
		if r := []rune(f.value); len(r) > 0 {
			f.value = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		f.value = ""
	case tea.KeyRunes, tea.KeySpace:
		f.value += string(k.Runes)
	case tea.KeyCtrlC:
		return true, tea.Quit
	}
	return false, nil
}

func (a *ask) view() string {
	f := a.fields[a.i]
	head := lipgloss.NewStyle().Bold(true).Foreground(cAccent).Render(a.title)
	line := head + sMuted.Render("  ·  "+f.label+"  ") + sBold.Render(f.value+"▏")
	if f.hint != "" {
		line += sFaint.Render("   " + f.hint)
	}
	return line + "\n" + sMuted.Render("enter ") + sFaint.Render("confirm") + sMuted.Render("  ·  esc ") + sFaint.Render("cancel") + sMuted.Render("  ·  ctrl+u ") + sFaint.Render("clear")
}

// stateKey opens the form or runs the command of a state key: W wait, u
// resume or reopen, x close.
func (m *model) stateKey(k string, r *row) tea.Cmd {
	d, root := r.d, r.store.root
	switch k {
	case "W":
		if d.State != dossier.Open && d.State != dossier.Waiting {
			m.status, m.statusErr = d.Label()+" is "+d.State+": only an open or waiting dossier can wait", true
			return nil
		}
		title, hint := "Wait "+d.Label(), "a date (2026-10-15), a delay (7d, 48h) or none; empty: the store's default"
		if d.State == dossier.Waiting {
			title, hint = "Correct the wait of "+d.Label(), "a date, a delay or none; empty: keep "+dayMonthYear(d.WaitUntil)
		}
		m.ask = &ask{title: title, fields: []askField{
			{label: "waiting on", value: d.WaitingOn, hint: "who must answer"},
			{label: "chase", hint: hint},
		}, done: func(v []string) tea.Cmd {
			if v[0] == "" {
				m.status, m.statusErr = d.Label()+": a wait needs someone to wait on", true
				return nil
			}
			args := []string{"--on", v[0]}
			if v[1] != "" {
				args = append(args, "--until", v[1])
			}
			return run(root, d.ID, "wait", args...)
		}}
	case "u":
		switch d.State {
		case dossier.Waiting:
			return run(root, d.ID, "resume")
		case dossier.Done:
			return run(root, d.ID, "reopen")
		}
		m.status, m.statusErr = d.Label()+" is "+d.State+": u resumes a waiting dossier or reopens a closed one", true
	case "x":
		if d.State == dossier.Done || d.State == dossier.Merged {
			m.status, m.statusErr = d.Label()+" is already "+d.State, true
			return nil
		}
		m.ask = &ask{title: "Close " + d.Label() + " · " + truncate(d.Title, 40), fields: []askField{
			{label: "outcome", hint: "optional, kept in the history; its sources are closed too"},
		}, done: func(v []string) tea.Cmd {
			var args []string
			if v[0] != "" {
				args = []string{"--note", v[0]}
			}
			return run(root, d.ID, "close", args...)
		}}
	}
	return nil
}
