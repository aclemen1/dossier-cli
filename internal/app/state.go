package app

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aclemen1/dossier-cli/internal/connector"
	"github.com/aclemen1/dossier-cli/internal/dossier"
	"github.com/aclemen1/dossier-cli/internal/spec"
	"github.com/aclemen1/dossier-cli/internal/store"
)

var moves = map[string]struct {
	from []string
	to   string
}{
	"wait":   {[]string{dossier.Open}, dossier.Waiting},
	"resume": {[]string{dossier.Waiting}, dossier.Open},
	"reopen": {[]string{dossier.Done}, dossier.Open},
	"close":  {[]string{dossier.Open, dossier.Waiting}, dossier.Done},
}

var verbHint = map[string]string{
	dossier.Open:    "`dossier wait <id> --on <who>` or `dossier close <id>`",
	dossier.Waiting: "`dossier resume <id>` or `dossier close <id>`",
	dossier.Done:    "`dossier reopen <id>`",
	dossier.Merged:  "the dossier it was merged into",
}

// SetState applies a move, logs it, reflects it in every source and applies
// the lifecycle rules. It returns the number of transitions left pending.
func (a *App) SetState(d *dossier.Dossier, move, note, waitingOn string) (int, error) {
	m := moves[move]
	ok := false
	for _, f := range m.from {
		if d.State == f {
			ok = true
		}
	}
	if !ok {
		return 0, spec.UserError("%s is %s; `%s` applies to %s dossiers. Use %s", d.ID, d.State, move, strings.Join(m.from, " or "), verbHint[d.State])
	}
	from := d.State
	d.State = m.to
	if m.to == dossier.Waiting {
		d.WaitingOn = waitingOn
	} else {
		d.WaitingOn = ""
	}
	line := from + " → " + m.to
	if waitingOn != "" {
		line += " · on " + waitingOn
	}
	if note != "" {
		line += " · " + note
	}
	_ = d.Log("%s", line)
	if m.to == dossier.Done {
		_ = a.Archive(d)
	}
	pending := a.reflect(d, from, m.to, note)
	if m.to == dossier.Done {
		a.notifyDependents(d)
	}
	if a.S.Config.ClosesTabOn(m.to) {
		_ = a.Archive(d)
		if err := a.closeSession(d); err != nil {
			_ = d.Log("tab not closed: %v", err)
		}
	}
	return pending, d.Save()
}

// reflect calls every source's transition. Failures are kept for `retry`.
func (a *App) reflect(d *dossier.Dossier, from, to, note string) int {
	for _, src := range d.Sources {
		name := src.Name()
		if name == "manual" || name == "" {
			continue
		}
		t := dossier.Transition{Source: name, SourceRef: src.ID, ThreadRef: threadFor(d, name), From: from, To: to, Note: note, At: dossier.Now()}
		cfg, ok := a.S.Config.Source(name)
		if !ok {
			t.Error = "no [[source]] named " + name + " in the store config"
			d.Run.PendingTransitions = append(d.Run.PendingTransitions, t)
			continue
		}
		if err := (connector.Runner{Store: a.S, Source: cfg}).Transition(t.SourceRef, t.ThreadRef, from, to, note); err != nil {
			t.Error = err.Error()
			d.Run.PendingTransitions = append(d.Run.PendingTransitions, t)
			_ = d.Log("source %s: %s → %s pending (%s)", name, from, to, firstLine(err.Error()))
		}
	}
	return len(d.Run.PendingTransitions)
}

func threadFor(d *dossier.Dossier, source string) string {
	for _, t := range d.Threads {
		if strings.HasPrefix(t, source+":") {
			return t
		}
	}
	return ""
}

// Retry replays pending transitions, oldest first.
func (a *App) Retry(d *dossier.Dossier) int {
	todo := d.Run.PendingTransitions
	d.Run.PendingTransitions = nil
	for _, t := range todo {
		cfg, ok := a.S.Config.Source(t.Source)
		if ok {
			if err := (connector.Runner{Store: a.S, Source: cfg}).Transition(t.SourceRef, t.ThreadRef, t.From, t.To, t.Note); err == nil {
				_ = d.Log("source %s: %s → %s delivered on retry", t.Source, t.From, t.To)
				continue
			} else {
				t.Error = err.Error()
			}
		}
		d.Run.PendingTransitions = append(d.Run.PendingTransitions, t)
	}
	_ = d.Save()
	return len(d.Run.PendingTransitions)
}

// ---------------------------------------------------------------- search

type Hit struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Updated string `json:"updated"`
	Score   int    `json:"score"`
	Snippet string `json:"snippet"`
	File    string `json:"file"`
}

func (a *App) Search(query string, states []string, withTranscripts bool) ([]Hit, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil, spec.UserError("search needs words. Example: dossier search \"armoire pharmacie\"")
	}
	all, err := a.All()
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, s := range states {
		if s != "all" {
			want[s] = true
		}
	}
	var hits []Hit
	for _, d := range all {
		if len(want) > 0 && !want[d.State] {
			continue
		}
		files := []string{d.Path("dossier.md")}
		for _, sub := range []string{"context", "files"} {
			m, _ := filepath.Glob(d.Path(sub, "*.md"))
			files = append(files, m...)
		}
		if withTranscripts {
			files = append(files, TranscriptPaths(d)...)
		}
		best := Hit{ID: d.ID, Title: d.Title, State: d.State, Updated: d.Updated}
		found := map[string]bool{}
		for _, f := range files {
			score, snippet := scoreFile(f, words, found)
			if score > best.Score {
				best.Score, best.Snippet = score, snippet
				best.File, _ = filepath.Rel(a.S.Root, f)
			}
		}
		if len(found) == len(words) {
			best.Score += 100 * len(words)
			hits = append(hits, best)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Updated > hits[j].Updated
	})
	return hits, nil
}

func scoreFile(path string, words []string, found map[string]bool) (int, string) {
	f, err := os.Open(path)
	if err != nil {
		return 0, ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	score, bestLine, bestLineScore := 0, "", 0
	for sc.Scan() {
		line := strings.ToLower(sc.Text())
		n := 0
		for _, w := range words {
			if strings.Contains(line, w) {
				found[w] = true
				n++
			}
		}
		score += n
		if n > bestLineScore {
			bestLineScore, bestLine = n, strings.TrimSpace(sc.Text())
		}
	}
	if len([]rune(bestLine)) > 200 {
		bestLine = string([]rune(bestLine)[:197]) + "…"
	}
	return score, bestLine
}

// ---------------------------------------------------------------- ingest

type IngestReport struct {
	Source  string       `json:"source"`
	Signals int          `json:"signals"`
	Events  int          `json:"events"`
	Opened  []OpenResult `json:"opened"`
	Skipped []string     `json:"skipped,omitempty"`
	Errors  []string     `json:"errors,omitempty"`
	Cursor  string       `json:"cursor,omitempty"`
}

func (a *App) cursors() map[string]string {
	out := map[string]string{}
	if b, err := os.ReadFile(a.S.Meta("cursors.json")); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (a *App) saveCursor(source, cursor string) error {
	c := a.cursors()
	c[source] = cursor
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := a.S.Meta("cursors.json.tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.S.Meta("cursors.json"))
}

func (a *App) Ingest(names []string, dryRun bool) ([]IngestReport, error) {
	sources := a.S.Config.Sources
	if len(names) > 0 {
		sources = nil
		for _, n := range names {
			cfg, ok := a.S.Config.Source(n)
			if !ok {
				return nil, spec.UserError("no [[source]] named %q in %s. Declared: %s", n, a.S.Meta("config.toml"), sourceNames(a.S.Config.Sources))
			}
			sources = append(sources, cfg)
		}
	}
	if len(sources) == 0 {
		return nil, spec.UserError("no [[source]] declared in %s. Add one, for example name = \"gmail\" with its command", a.S.Meta("config.toml"))
	}
	all, _ := a.All()
	var reports []IngestReport
	for _, src := range sources {
		rep := IngestReport{Source: src.Name}
		var watch []string
		for _, d := range all {
			if d.State == dossier.Merged {
				continue
			}
			for _, t := range d.Threads {
				if strings.HasPrefix(t, src.Name+":") {
					watch = append(watch, t)
				}
			}
		}
		res, err := (connector.Runner{Store: a.S, Source: src}).Poll(a.cursors()[src.Name], watch, dryRun)
		if err != nil {
			rep.Errors = append(rep.Errors, err.Error())
			reports = append(reports, rep)
			continue
		}
		rep.Signals, rep.Events, rep.Cursor = len(res.Signals), len(res.Events), res.Cursor
		if dryRun {
			for _, s := range res.Signals {
				rep.Skipped = append(rep.Skipped, "signal "+s.SourceRef+" · "+s.Title)
			}
			for _, e := range res.Events {
				rep.Skipped = append(rep.Skipped, "event "+e.ThreadRef+" · "+e.Kind)
			}
			reports = append(reports, rep)
			continue
		}
		failed := false
		for _, s := range res.Signals {
			r, err := a.Open(OpenParams{Title: s.Title, SourceRef: s.SourceRef, ThreadRef: s.ThreadRef, URL: s.URL,
				Instruction: s.Instruction, Summary: s.Summary, Files: s.Files})
			if err != nil {
				rep.Errors = append(rep.Errors, s.SourceRef+": "+err.Error())
				if r.ID == "" {
					failed = true
				}
				continue
			}
			if r.Outcome == "existing" {
				rep.Skipped = append(rep.Skipped, s.SourceRef+" already in "+r.ID)
				continue
			}
			rep.Opened = append(rep.Opened, r)
		}
		for _, e := range res.Events {
			d := a.FindByThread(e.ThreadRef)
			if d == nil {
				rep.Skipped = append(rep.Skipped, "event on unknown thread "+e.ThreadRef)
				continue
			}
			if d.State == dossier.Done {
				if _, err := a.SetState(d, "reopen", "event on "+e.ThreadRef, ""); err != nil {
					rep.Errors = append(rep.Errors, err.Error())
					continue
				}
			} else if d.State == dossier.Waiting {
				if _, err := a.SetState(d, "resume", "event on "+e.ThreadRef, ""); err != nil {
					rep.Errors = append(rep.Errors, err.Error())
					continue
				}
			}
			summary := e.Summary
			if summary == nil {
				summary = map[string]any{}
			}
			summary["kind"] = e.Kind
			r, err := a.event(d, "", summary, e.Files, false)
			if err != nil {
				rep.Errors = append(rep.Errors, d.ID+": "+err.Error())
				continue
			}
			r.Outcome = "event"
			rep.Opened = append(rep.Opened, r)
		}
		if !failed && res.Cursor != "" {
			if err := a.saveCursor(src.Name, res.Cursor); err != nil {
				rep.Errors = append(rep.Errors, err.Error())
			}
		}
		reports = append(reports, rep)
	}
	return reports, nil
}

func sourceNames(s []store.SourceConfig) string {
	var n []string
	for _, x := range s {
		n = append(n, x.Name)
	}
	if len(n) == 0 {
		return "none"
	}
	return strings.Join(n, ", ")
}
