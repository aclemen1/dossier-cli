// Package tui is an interactive overview of every store under a root: the
// dossiers, their links, what their agents do, and a jump to their pane.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aclemen1/dossier-cli/internal/dossier"
)

const refreshEvery = 5 * time.Second

// Run starts the TUI on the stores found under root.
func Run(root string) error {
	roots := Stores(root)
	if len(roots) == 0 {
		return fmt.Errorf("no dossier store under %s: a store is a directory holding .dossier/config.toml", root)
	}
	m := &model{roots: roots, root: root}
	m.reload()
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type model struct {
	root      string
	roots     []string
	rows      []row
	errs      []string
	cursor    int
	offset    int
	width     int
	height    int
	all       bool
	filter    string
	typing    bool
	detail    bool
	status    string
	statusErr bool
}

type tickMsg time.Time
type jumpMsg struct {
	id  string
	out string
	err error
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Init() tea.Cmd { return tick() }

func (m *model) selected() *row {
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].d != nil {
		return &m.rows[m.cursor]
	}
	return nil
}

func (m *model) reload() {
	key := ""
	if r := m.selected(); r != nil {
		key = r.store.root + "|" + r.d.ID
	}
	m.rows, m.errs = load(m.roots, m.all, m.filter)
	m.cursor = -1
	for i, r := range m.rows {
		if r.d != nil && r.store.root+"|"+r.d.ID == key {
			m.cursor = i
			break
		}
	}
	if m.cursor < 0 {
		m.cursor = 0
		m.move(1)
		m.move(-1)
	}
}

// move steps the cursor over dossier rows, skipping store headers.
func (m *model) move(step int) {
	for i := m.cursor + step; i >= 0 && i < len(m.rows); i += step {
		if m.rows[i].d != nil {
			m.cursor = i
			return
		}
	}
}

func jump(root, id string) tea.Cmd {
	return func() tea.Msg {
		exe, err := os.Executable()
		if err != nil {
			return jumpMsg{id: id, err: err}
		}
		out, err := exec.Command(exe, "attach", id, "--store", root, "--format", "text").CombinedOutput()
		return jumpMsg{id: id, out: strings.TrimSpace(string(out)), err: err}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.reload()
		return m, tick()
	case jumpMsg:
		if msg.err != nil {
			m.status, m.statusErr = msg.id+": "+firstLine(msg.out, msg.err.Error()), true
		} else {
			m.status, m.statusErr = msg.id+": pane focused", false
		}
		m.reload()
	case tea.KeyMsg:
		if m.typing {
			return m, m.typeFilter(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgup":
			for i := 0; i < m.listHeight()/2; i++ {
				m.move(-1)
			}
		case "pgdown":
			for i := 0; i < m.listHeight()/2; i++ {
				m.move(1)
			}
		case "g", "home":
			m.cursor = 0
			m.move(1)
		case "G", "end":
			m.cursor = len(m.rows)
			m.move(-1)
		case "a":
			m.all = !m.all
			m.reload()
		case "/":
			m.typing = true
		case "esc":
			m.filter = ""
			m.reload()
		case "r":
			m.reload()
		case "tab":
			m.detail = !m.detail
		case "enter", "S":
			r := m.selected()
			if r == nil {
				break
			}
			if r.activity == "none" && msg.String() != "S" {
				m.status, m.statusErr = r.d.ID+" has no session yet: press S to start one with its open prompt", true
				break
			}
			m.status, m.statusErr = r.d.ID+": opening its pane…", false
			return m, jump(r.store.root, r.d.ID)
		}
	}
	return m, nil
}

func (m *model) typeFilter(k tea.KeyMsg) tea.Cmd {
	switch k.Type {
	case tea.KeyEnter:
		m.typing = false
	case tea.KeyEsc:
		m.typing, m.filter = false, ""
	case tea.KeyBackspace:
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.filter += string(k.Runes)
		if k.Type == tea.KeySpace {
			m.filter += " "
		}
	case tea.KeyCtrlC:
		return tea.Quit
	}
	m.reload()
	return nil
}

func firstLine(s, fallback string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return strings.TrimPrefix(l, "error: ")
		}
	}
	return fallback
}

// ---------------------------------------------------------------- view

var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#5A4FCF", Dark: "#A99CFF"}
	cMuted   = lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#6E6E6E"}
	cText    = lipgloss.AdaptiveColor{Light: "#1F1F1F", Dark: "#E6E6E6"}
	cOpen    = lipgloss.AdaptiveColor{Light: "#1F6FD1", Dark: "#6CB6FF"}
	cWaiting = lipgloss.AdaptiveColor{Light: "#A04BC2", Dark: "#D59BF0"}
	cDone    = lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#6E6E6E"}
	cWorking = lipgloss.AdaptiveColor{Light: "#B7791F", Dark: "#F2C14E"}
	cReady   = lipgloss.AdaptiveColor{Light: "#2F855A", Dark: "#68D391"}
	cStopped = lipgloss.AdaptiveColor{Light: "#C53030", Dark: "#FC8181"}
	cSelBg   = lipgloss.AdaptiveColor{Light: "#ECE9FF", Dark: "#2D2A4A"}

	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sMuted  = lipgloss.NewStyle().Foreground(cMuted)
	sText   = lipgloss.NewStyle().Foreground(cText)
	sBold   = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sHeader = lipgloss.NewStyle().Bold(true).Foreground(cAccent).MarginTop(1)
	sPanel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cMuted).Padding(0, 1)
)

func stateStyle(s string) lipgloss.Style {
	switch s {
	case dossier.Open:
		return lipgloss.NewStyle().Foreground(cOpen)
	case dossier.Waiting:
		return lipgloss.NewStyle().Foreground(cWaiting)
	case dossier.Merged:
		return lipgloss.NewStyle().Foreground(cDone).Strikethrough(true)
	}
	return lipgloss.NewStyle().Foreground(cDone)
}

// activityMark is a one-cell glyph for what the dossier's agent does.
func activityMark(a string) string {
	switch a {
	case "working":
		return lipgloss.NewStyle().Foreground(cWorking).Render("◉")
	case "ready", "idle":
		return lipgloss.NewStyle().Foreground(cReady).Render("●")
	case "stopped":
		return lipgloss.NewStyle().Foreground(cStopped).Render("○")
	case "none":
		return sMuted.Render("·")
	}
	return lipgloss.NewStyle().Foreground(cWorking).Render("◌")
}

func (m *model) wide() bool { return m.width >= 110 }

func (m *model) listHeight() int {
	h := m.height - 4
	if h < 3 {
		h = 3
	}
	return h
}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	top := m.topBar()
	bottom := m.bottomBar()
	h := m.listHeight()
	var body string
	switch {
	case m.wide():
		lw := m.width * 55 / 100
		list := m.listView(lw, h)
		det := sPanel.Width(m.width - lw - 4).Height(h - 2).Render(m.detailView(m.width-lw-6, h-2))
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, " ", det)
	case m.detail:
		body = sPanel.Width(m.width - 2).Height(h - 2).Render(m.detailView(m.width-4, h-2))
	default:
		body = m.listView(m.width, h)
	}
	return lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
}

func (m *model) topBar() string {
	var parts []string
	for _, r := range m.rows {
		if r.header == "" {
			continue
		}
		c := r.store.count
		parts = append(parts, fmt.Sprintf("%s %s %s",
			sBold.Render(r.header),
			stateStyle(dossier.Open).Render(fmt.Sprintf("%d open", c[dossier.Open])),
			stateStyle(dossier.Waiting).Render(fmt.Sprintf("%d waiting", c[dossier.Waiting]))))
	}
	scope := "active"
	if m.all {
		scope = "all states"
	}
	left := sTitle.Render("dossier") + sMuted.Render(" · "+scope+" · ") + strings.Join(parts, sMuted.Render("  │  "))
	if m.filter != "" || m.typing {
		cur := ""
		if m.typing {
			cur = "▏"
		}
		left += sMuted.Render("  /") + sText.Render(m.filter+cur)
	}
	return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(left)
}

func (m *model) bottomBar() string {
	keys := "↑↓ move · enter jump to pane · a all states · / filter · tab detail · r refresh · q quit"
	if m.wide() {
		keys = strings.Replace(keys, " · tab detail", "", 1)
	}
	line := sMuted.Render(keys)
	if len(m.errs) > 0 {
		line = lipgloss.NewStyle().Foreground(cStopped).Render(m.errs[0])
	}
	if m.status != "" {
		st := lipgloss.NewStyle().Foreground(cReady)
		if m.statusErr {
			st = lipgloss.NewStyle().Foreground(cStopped)
		}
		line = st.Render(m.status) + sMuted.Render("  ·  ") + line
	}
	return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(line)
}

func (m *model) listView(w, h int) string {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset > 0 && m.cursor > 0 && m.rows[m.cursor-1].header != "" && m.cursor-1 < m.offset {
		m.offset = m.cursor - 1
	}
	var lines []string
	for i := m.offset; i < len(m.rows) && len(lines) < h; i++ {
		lines = append(lines, m.rowView(m.rows[i], i == m.cursor, w))
	}
	if len(m.rows) == 0 || (len(m.rows) > 0 && m.selected() == nil && len(lines) <= 2) {
		lines = append(lines, sMuted.Render("  nothing to show — press a for all states, esc to clear the filter"))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
}

func (m *model) rowView(r row, sel bool, w int) string {
	if r.header != "" {
		return sHeader.UnsetMarginTop().Render("▍"+strings.ToUpper(r.header)) + sMuted.Render("  "+r.store.root)
	}
	d := r.d
	var tree strings.Builder
	for lvl := 0; lvl < r.depth; lvl++ {
		last := r.last[lvl]
		switch {
		case lvl == r.depth-1 && last:
			tree.WriteString("└─ ")
		case lvl == r.depth-1:
			tree.WriteString("├─ ")
		case last:
			tree.WriteString("   ")
		default:
			tree.WriteString("│  ")
		}
	}
	id := d.Label()
	state := stateStyle(d.State).Render(fmt.Sprintf("%-7s", d.State))
	prefix := " " + activityMark(r.activity) + " " + sMuted.Render(tree.String()) + sBold.Render(fmt.Sprintf("%-7s", id)) + " " + state + " "
	var tail []string
	if d.State == dossier.Waiting && d.WaitingOn != "" {
		on := d.WaitingOn
		if i := strings.IndexAny(on, "(,"); i > 0 {
			on = strings.TrimSpace(on[:i])
		}
		tail = append(tail, "⏳ "+on+short(d.WaitUntil))
	}
	if r.cycle {
		tail = append(tail, "↻ cycle")
	}
	if len(r.blocked) > 0 {
		tail = append(tail, "⛓ "+strings.Join(r.blocked, " "))
	}
	suffix := ""
	if len(tail) > 0 {
		suffix = "  " + strings.Join(tail, "  ")
	}
	room := w - lipgloss.Width(prefix) - lipgloss.Width(suffix) - 1
	title := truncate(d.Title, room)
	line := prefix + sText.Render(title) + sMuted.Render(suffix)
	st := lipgloss.NewStyle().Width(w).MaxWidth(w)
	if sel {
		st = st.Background(cSelBg)
	}
	return st.Render(line)
}

func short(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return " · " + t.Format("02.01")
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n <= 1 {
		return ""
	}
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func (m *model) detailView(w, h int) string {
	r := m.selected()
	if r == nil {
		return sMuted.Render("No dossier selected.")
	}
	d := r.d
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	wrap := lipgloss.NewStyle().Width(w)
	label := lipgloss.NewStyle().Foreground(cMuted).Width(10)

	line(sTitle.Render(d.Label()) + sMuted.Render(" · "+r.store.name) + aliasNote(d))
	line(wrap.Inherit(sBold).Render(d.Title))
	line("")
	line(label.Render("state") + stateStyle(d.State).Render(d.State) + sMuted.Render(" · agent ") + activityMark(r.activity) + " " + sText.Render(r.activity))
	if d.State == dossier.Waiting {
		line(label.Render("waiting") + sText.Render(truncate(d.WaitingOn, w-12)) + sMuted.Render(short(d.WaitUntil)))
	}
	if d.Run.TabID != "" {
		line(label.Render("tab") + sText.Render(d.Run.TabID))
	}
	for _, s := range d.Sources {
		line(label.Render("source") + sText.Render(truncate(s.ID, w-12)))
	}

	out, in := r.store.a.Outgoing(d), r.store.a.Incoming(d)
	if len(out)+len(in) > 0 {
		line("")
		line(sBold.Render("Links"))
		for _, e := range out {
			line(edge("→", e.Rel, labelOf(r.store, e.ID), e.Title, e.State, w))
		}
		for _, e := range in {
			line(edge("←", e.Rel, labelOf(r.store, e.ID), e.Title, e.State, w))
		}
	}
	if d.Description != "" {
		line("")
		line(sBold.Render("Instruction"))
		line(wrap.Inherit(sText).Render(d.Description))
	}
	if tail := logTail(d, 6); len(tail) > 0 {
		line("")
		line(sBold.Render("History"))
		for _, l := range tail {
			line(sMuted.Render(truncate(l, w)))
		}
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func aliasNote(d *dossier.Dossier) string {
	if d.Label() == d.ID {
		return ""
	}
	return sMuted.Render(" · " + d.ID)
}

func edge(arrow, rel, id, title, state string, w int) string {
	head := sMuted.Render(fmt.Sprintf(" %s %-11s ", arrow, rel)) + sBold.Render(id) + " "
	st := stateStyle(state).Render(state)
	room := w - lipgloss.Width(head) - lipgloss.Width(st) - 2
	return head + sText.Render(truncate(title, room)) + " " + st
}

func labelOf(s *storeView, id string) string {
	if d := s.byID[id]; d != nil {
		return d.Label()
	}
	return id
}
