// Package actions declares every action as a spec.Action.
package actions

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/aclemen1/dossier-cli/internal/app"
	"github.com/aclemen1/dossier-cli/internal/connector"
	"github.com/aclemen1/dossier-cli/internal/dossier"
	"github.com/aclemen1/dossier-cli/internal/spec"
	"github.com/aclemen1/dossier-cli/internal/store"
)

var Version = "0.1.0-dev"

//go:embed skill.md
var skillText string

func idParam(help string) spec.Param {
	return spec.Param{Name: "id", Kind: spec.String, Positional: true, Help: help, Aliases: []string{"dossier"}}
}

// withApp opens the store, takes its lock for mutating actions and runs fn.
func withApp(ctx *spec.Context, lock bool, fn func(*app.App) (any, error)) (any, error) {
	a, err := app.New(ctx.Store)
	if err != nil {
		return nil, err
	}
	if lock {
		if err := a.S.Lock(); err != nil {
			return nil, err
		}
		defer a.S.Unlock()
	}
	return fn(a)
}

type stateResult struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Pending int    `json:"pending_transitions"`
}

func (r stateResult) PendingTransitions() int { return r.Pending }

func moveAction(name, summary string, effects, examples []string, extra ...spec.Param) *spec.Action {
	params := append([]spec.Param{idParam("Dossier id (D-0042, 42 or 0042-slug). Defaults to DOSSIER_ID.")}, extra...)
	return &spec.Action{
		Category: "state", Name: name, Summary: summary, Params: params, Effects: effects, Examples: examples,
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) {
				d, err := a.Load(ctx.Str("id"))
				if err != nil {
					return nil, err
				}
				pending, err := a.SetState(d, name, ctx.Str("note"), ctx.Str("on"))
				if err != nil {
					return nil, err
				}
				if name == "resume" && ctx.Str("prompt") != "" {
					if err := a.Prompt(d, ctx.Str("prompt")); err != nil {
						return nil, err
					}
				}
				return stateResult{d.ID, d.State, pending}, nil
			})
		},
		Text: func(w io.Writer, r any) {
			s := r.(stateResult)
			fmt.Fprintf(w, "%s is %s", s.ID, s.State)
			if s.Pending > 0 {
				fmt.Fprintf(w, " · %d source transition(s) pending, see `dossier retry %s`", s.Pending, s.ID)
			}
			fmt.Fprintln(w)
		},
	}
}

func init() {
	// ---------------------------------------------------------------- meta
	spec.Register(&spec.Action{
		Category: "meta", Name: "version", Summary: "Print the dossier version.", Meta: true,
		Examples: []string{"dossier version"},
		Run:      func(*spec.Context) (any, error) { return Version, nil },
	})
	spec.Register(&spec.Action{
		Category: "meta", Name: "schema", Summary: "Browse actions: catalog, category, or one action's full spec.", Meta: true,
		Params: []spec.Param{
			{Name: "category", Kind: spec.String, Positional: true, Help: "Category to list."},
			{Name: "action", Kind: spec.String, Positional: true, Help: "Action to describe."},
			{Name: "search", Kind: spec.String, Help: "Match actions across categories."},
		},
		Examples: []string{"dossier schema", "dossier schema state", "dossier schema state close", "dossier schema --search merge"},
		Run: func(ctx *spec.Context) (any, error) {
			if q := ctx.Str("search"); q != "" {
				return spec.Search(q), nil
			}
			cat, act := ctx.Str("category"), ctx.Str("action")
			switch {
			case cat == "":
				return spec.Catalog(), nil
			case act == "":
				l := spec.ActionsIn(cat)
				if len(l) == 0 {
					return nil, spec.NotFound("no category %q. Categories: %s", cat, strings.Join(spec.Categories(), ", "))
				}
				return l, nil
			}
			a := spec.Find(cat, act)
			if a == nil {
				return nil, spec.NotFound("no action %q in %q. Try `dossier schema %s`", act, cat, cat)
			}
			return spec.Leaf{Action: a, Usage: spec.Usage(a)}, nil
		},
		Text: spec.TextSchema,
	})
	spec.Register(&spec.Action{
		Category: "meta", Name: "skill", Summary: "Print the embedded agent skill.", Meta: true,
		Params:   []spec.Param{{Name: "verb", Kind: spec.String, Positional: true, Default: "show", Enum: []string{"show"}, Help: "show"}},
		Examples: []string{"dossier skill show"},
		Run:      func(*spec.Context) (any, error) { return strings.TrimSpace(skillText), nil },
	})

	// ---------------------------------------------------------------- store
	spec.Register(&spec.Action{
		Category: "store", Name: "init", Summary: "Create a store: one per sphere.",
		Params: []spec.Param{
			{Name: "path", Kind: spec.String, Positional: true, Required: true, Help: "Directory of the new store."},
			{Name: "sphere", Kind: spec.String, Required: true, Help: "Sphere name, e.g. perso or pro."},
			{Name: "default", Kind: spec.Bool, Help: "Record this store as the default one in ~/.config/dossier/config.toml."},
		},
		Effects: []string{
			"Creates <path>/.dossier/config.toml, the prompt templates, index.md (OKF bundle root) and .gitignore.",
			"With --default, writes default_store to ~/.config/dossier/config.toml.",
		},
		Examples: []string{"dossier init ~/dossiers/perso --sphere perso --default", "dossier init ~/dossiers/pro --sphere pro"},
		Run: func(ctx *spec.Context) (any, error) {
			s, err := store.Init(ctx.Str("path"), ctx.Str("sphere"), ctx.Bool("default"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"store": s.Root, "sphere": s.Config.Store.Sphere, "config": s.Meta("config.toml")}, nil
		},
	})
	spec.Register(&spec.Action{
		Category: "store", Name: "setup", Summary: "Sync the store's skills directory with [agent] skills and skill_commands.",
		Effects: []string{
			"Symlinks each [agent] skills directory into <store>/.claude/skills/.",
			"Writes <store>/.claude/skills/<name>/SKILL.md from each skill_commands command.",
			"Removes symlinks that the config no longer lists.",
			"Writes <store>/.dossier/run/agent-settings.json (hooks, deny rules). Both run on their own before every session start.",
		},
		Examples: []string{"dossier setup"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) {
				settings, err := a.AgentSettings()
				if err != nil {
					return nil, err
				}
				return map[string]any{"skills": a.SyncSkills(), "agent_settings": settings}, nil
			})
		},
	})
	spec.Register(&spec.Action{
		Category: "store", Name: "doctor", Summary: "Check the ACP server, herdr, every connector and pending transitions.",
		Examples: []string{"dossier doctor"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, false, func(a *app.App) (any, error) { return a.Doctor(), nil })
		},
		Text: func(w io.Writer, r any) {
			for _, c := range r.(app.DoctorReport).Checks {
				mark := "ok  "
				if !c.OK {
					mark = "FAIL"
				}
				fmt.Fprintf(w, "%s %-28s %s\n", mark, c.Name, c.Detail)
			}
		},
	})

	// ---------------------------------------------------------------- dossier
	spec.Register(&spec.Action{
		Category: "dossier", Name: "open", Summary: "Open a dossier and start its agent session in the background.",
		Discussion: "Idempotent on --source: a known source returns its dossier, and reopens it when it is done. " +
			"An instruction that starts with a routing prefix and a number (\"D-42: …\") goes to that dossier as an event.",
		Params: []spec.Param{
			{Name: "title", Kind: spec.String, Required: true, Help: "Short title of the affair."},
			{Name: "instruction", Kind: spec.String, Help: "First instruction for the agent."},
			{Name: "instruction-file", Kind: spec.String, Help: "Read the instruction from a file."},
			{Name: "source", Kind: spec.String, Help: "Source reference <name>:<ref>, e.g. gmail:task/abc."},
			{Name: "thread", Kind: spec.String, Help: "Thread reference <name>:<ref>, used to attach later events."},
			{Name: "url", Kind: spec.String, Help: "Link to the original item."},
			{Name: "file", Kind: spec.StringList, Help: "File copied into context/ (repeatable)."},
			{Name: "parent", Kind: spec.String, Help: "Parent dossier id."},
			{Name: "no-start", Kind: spec.Bool, Help: "Create the dossier without starting its session."},
		},
		Effects: []string{
			"Creates <store>/NNNN-<slug>/ with dossier.md, log.md, .state.json and prompts/0001-open.md.",
			"Starts an agent session in a new herdr tab through the ACP server and sends the open prompt; returns before the turn ends.",
		},
		Examples: []string{
			`dossier open --title "Armoire de pharmacie" --instruction "Demander une date de passage"`,
			`dossier open --title "Devis toiture" --file ~/Downloads/devis.pdf --no-start`,
			`dossier open --title "Relance gérance" --parent D-0042`,
		},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) {
				instr := ctx.Str("instruction")
				if f := ctx.Str("instruction-file"); f != "" {
					b, err := os.ReadFile(store.ExpandHome(f))
					if err != nil {
						return nil, spec.UserError("cannot read --instruction-file %s: %v", f, err)
					}
					instr = string(b)
				}
				var files []connector.File
				for _, f := range ctx.List("file") {
					p := store.ExpandHome(f)
					if _, err := os.Stat(p); err != nil {
						return nil, spec.UserError("--file %s: %v", f, err)
					}
					files = append(files, connector.File{Name: p, Path: p})
				}
				return a.Open(app.OpenParams{Title: ctx.Str("title"), Instruction: instr, SourceRef: ctx.Str("source"),
					ThreadRef: ctx.Str("thread"), URL: ctx.Str("url"), Files: files, Parent: ctx.Str("parent"), NoStart: ctx.Bool("no-start")})
			})
		},
		Text: func(w io.Writer, r any) {
			o := r.(app.OpenResult)
			fmt.Fprintf(w, "%s %s · %s", o.ID, o.Outcome, o.Dir)
			if o.TabID != "" {
				fmt.Fprintf(w, " · tab %s", o.TabID)
			}
			fmt.Fprintln(w)
		},
	})

	spec.Register(&spec.Action{
		Category: "dossier", Name: "ls", Summary: "List dossiers with their state and what their agent is doing.",
		Params: []spec.Param{
			{Name: "status", Kind: spec.String, Default: "active", Enum: []string{"active", "open", "waiting", "done", "merged", "all"}, Help: "active = open and waiting."},
			{Name: "parent", Kind: spec.String, Help: "Only children of this dossier."},
		},
		Examples: []string{"dossier ls", "dossier ls --status waiting", "dossier ls --status all --format text"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, false, func(a *app.App) (any, error) { return a.List(ctx.Str("status"), ctx.Str("parent")) })
		},
		Text: func(w io.Writer, r any) {
			rows := r.([]app.Row)
			for _, x := range rows {
				extra := ""
				if x.WaitingOn != "" {
					extra = " · on " + x.WaitingOn
				}
				if x.Pending > 0 {
					extra += fmt.Sprintf(" · %d pending", x.Pending)
				}
				if len(x.BlockedBy) > 0 {
					extra += " · blocked by " + strings.Join(x.BlockedBy, ", ")
				}
				fmt.Fprintf(w, "%s  %-8s %-8s %s%s\n", x.ID, x.State, x.Activity, x.Title, extra)
			}
			if len(rows) == 0 {
				fmt.Fprintln(w, "no dossier")
			}
		},
	})

	spec.Register(&spec.Action{
		Category: "dossier", Name: "show", Summary: "Show one dossier: sources, state, session, files, history.",
		Params:   []spec.Param{idParam("Dossier id. Defaults to DOSSIER_ID.")},
		Examples: []string{"dossier show D-0042", "dossier show 42 --format text"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, false, func(a *app.App) (any, error) {
				d, err := a.Load(ctx.Str("id"))
				if err != nil {
					return nil, err
				}
				return a.Show(d), nil
			})
		},
		Text: func(w io.Writer, r any) {
			s := r.(app.ShowResult)
			fmt.Fprintf(w, "%s · %s · %s · %s\n", s.ID, s.State, s.Activity, s.Title)
			if s.WaitingOn != "" {
				fmt.Fprintf(w, "waiting on %s\n", s.WaitingOn)
			}
			for _, src := range s.Sources {
				fmt.Fprintf(w, "source  %s %s\n", src.ID, src.Resource)
			}
			for _, f := range s.Files {
				fmt.Fprintf(w, "file    %s\n", f)
			}
			for _, e := range s.Outgoing {
				fmt.Fprintf(w, "link    %s → %s · %s · %s\n", e.Rel, e.ID, e.Title, e.State)
			}
			for _, e := range s.Incoming {
				fmt.Fprintf(w, "linked  %s ← %s · %s · %s\n", e.Rel, e.ID, e.Title, e.State)
			}
			if s.Session != "" {
				fmt.Fprintf(w, "session %s · tab %s\n", s.Session, s.TabID)
			}
			fmt.Fprintf(w, "\n%s", s.Log)
		},
	})

	spec.Register(&spec.Action{
		Category: "dossier", Name: "search", Summary: "Search every dossier of the store, open or closed.",
		Params: []spec.Param{
			{Name: "query", Kind: spec.String, Positional: true, Required: true, Help: "Words that must all appear."},
			{Name: "status", Kind: spec.String, Default: "all", Enum: []string{"all", "open", "waiting", "done", "merged"}, Help: "Restrict to one state."},
			{Name: "with-transcripts", Kind: spec.Bool, Help: "Also search session transcripts (slower)."},
		},
		Examples: []string{`dossier search "armoire pharmacie"`, `dossier search "Baer" --status done`},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, false, func(a *app.App) (any, error) {
				return a.Search(ctx.Str("query"), []string{ctx.Str("status")}, ctx.Bool("with-transcripts"))
			})
		},
		Text: func(w io.Writer, r any) {
			for _, h := range r.([]app.Hit) {
				fmt.Fprintf(w, "%s  %-7s %s\n         %s · %s\n", h.ID, h.State, h.Title, h.File, h.Snippet)
			}
		},
	})

	spec.Register(&spec.Action{
		Category: "dossier", Name: "attach", Summary: "Focus the dossier's tab; relaunch its session first when the tab is gone.",
		Params:   []spec.Param{idParam("Dossier id.")},
		Effects:  []string{"May open a new herdr tab running the resumed session."},
		Examples: []string{"dossier attach D-0042"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) {
				d, err := a.Load(ctx.Str("id"))
				if err != nil {
					return nil, err
				}
				if err := a.Attach(d); err != nil {
					return nil, err
				}
				return map[string]any{"id": d.ID, "session": d.Run.Session, "tab_id": d.Run.TabID}, nil
			})
		},
	})

	spec.Register(&spec.Action{
		Category: "dossier", Name: "prompt", Summary: "Send a prompt to the dossier's session without waiting for the turn.",
		Params: []spec.Param{
			idParam("Dossier id. Defaults to DOSSIER_ID."),
			{Name: "text", Kind: spec.String, Help: "Prompt text."},
			{Name: "file", Kind: spec.String, Help: "Read the prompt from a file."},
		},
		Effects:  []string{"Types the prompt into the agent session; relaunches the session if its tab is gone."},
		Examples: []string{`dossier prompt D-0042 --text "La gérance a rappelé, rédige la réponse."`},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) {
				d, err := a.Load(ctx.Str("id"))
				if err != nil {
					return nil, err
				}
				text := ctx.Str("text")
				if f := ctx.Str("file"); f != "" {
					b, err := os.ReadFile(store.ExpandHome(f))
					if err != nil {
						return nil, spec.UserError("cannot read --file %s: %v", f, err)
					}
					text = string(b)
				}
				if strings.TrimSpace(text) == "" {
					return nil, spec.UserError("nothing to send. Example: dossier prompt %s --text \"…\"", d.ID)
				}
				return nil, a.Prompt(d, text)
			})
		},
	})

	// ---------------------------------------------------------------- state
	spec.Register(moveAction("wait", "Mark the dossier as waiting on a third party.",
		[]string{"Sets state to waiting and waiting_on.", "Calls transition open → waiting on every source (Gmail: purple star).", "Closes the tab when lifecycle.close_tab_on includes waiting."},
		[]string{`dossier wait D-0042 --on "Baer SA"`, `dossier wait --on "la gérance"`},
		spec.Param{Name: "on", Kind: spec.String, Required: true, Help: "Who the dossier waits for."},
		spec.Param{Name: "note", Kind: spec.String, Help: "Free note for the history."}))
	spec.Register(moveAction("resume", "Bring a waiting dossier back to open.",
		[]string{"Sets state to open.", "Calls transition waiting → open on every source (Gmail: purple star removed)."},
		[]string{"dossier resume D-0042", `dossier resume D-0042 --prompt "Baer SA a répondu, lis le dernier message."`},
		spec.Param{Name: "note", Kind: spec.String, Help: "Free note for the history."},
		spec.Param{Name: "prompt", Kind: spec.String, Help: "Prompt sent to the session after resuming."}))
	spec.Register(moveAction("reopen", "Reopen a closed dossier.",
		[]string{"Sets state to open.", "Calls transition done → open on every source (Gmail: task unchecked)."},
		[]string{"dossier reopen D-0042"},
		spec.Param{Name: "note", Kind: spec.String, Help: "Free note for the history."}))
	close := moveAction("close", "Close the dossier.",
		[]string{"Sets state to done and archives the transcript.", "Calls transition → done on every source (Gmail: task checked, purple star removed, label reviewed).", "Closes the tab when lifecycle.close_tab_on includes done."},
		[]string{`dossier close D-0042 --note "Passage fixé au 12.10"`, "dossier close"},
		spec.Param{Name: "note", Kind: spec.String, Help: "Outcome, kept in the history."})
	close.Destructive = true
	spec.Register(close)

	spec.Register(&spec.Action{
		Category: "state", Name: "retry", Summary: "Replay source transitions left pending.",
		Params:   []spec.Param{idParam("Dossier id; all dossiers when omitted outside a session.")},
		Examples: []string{"dossier retry", "dossier retry D-0042"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) {
				var targets []*dossier.Dossier
				if ctx.Str("id") != "" || os.Getenv("DOSSIER_ID") != "" {
					d, err := a.Load(ctx.Str("id"))
					if err != nil {
						return nil, err
					}
					targets = []*dossier.Dossier{d}
				} else {
					targets, _ = a.All()
				}
				left := map[string]int{}
				for _, d := range targets {
					if len(d.Run.PendingTransitions) > 0 {
						left[d.ID] = a.Retry(d)
					}
				}
				return left, nil
			})
		},
	})

	// ---------------------------------------------------------------- graph
	relParam := spec.Param{Name: "rel", Kind: spec.String, Required: true, Enum: []string{"includes", "depends_on"},
		Help: "includes: <to> is a point of <from> (agenda item, sub-affair). depends_on: <from> waits for <to>."}
	spec.Register(&spec.Action{
		Category: "graph", Name: "link", Summary: "Add a typed link from one dossier to another.",
		Discussion: "Links are stored on <from>. includes builds agendas and sub-affairs; depends_on marks <from> as blocked " +
			"until <to> is done, and <from> hears when it closes. Cycles are refused.",
		Params: []spec.Param{
			{Name: "from", Kind: spec.String, Positional: true, Required: true, Help: "Dossier that holds the link."},
			{Name: "to", Kind: spec.String, Positional: true, Required: true, Help: "Dossier it points at."},
			relParam,
		},
		Effects:  []string{"Adds {rel, to} to links in <from>/dossier.md and rewrites its managed links block.", "Appends a line to <from>/log.md."},
		Examples: []string{"dossier link D-0007 D-0042 --rel includes", "dossier link D-0042 D-0051 --rel depends_on"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) { return a.Link(ctx.Str("from"), ctx.Str("to"), ctx.Str("rel")) })
		},
		Text: func(w io.Writer, r any) { l := r.(app.LinkResult); fmt.Fprintf(w, "%s %s %s\n", l.From, l.Rel, l.To) },
	})
	unlinkRel := relParam
	unlinkRel.Required = false
	unlinkRel.Help = "Only remove links of this type; all links to <to> when omitted."
	spec.Register(&spec.Action{
		Category: "graph", Name: "unlink", Summary: "Remove the links from one dossier to another.",
		Params: []spec.Param{
			{Name: "from", Kind: spec.String, Positional: true, Required: true, Help: "Dossier that holds the link."},
			{Name: "to", Kind: spec.String, Positional: true, Required: true, Help: "Dossier it points at."},
			unlinkRel,
		},
		Effects:  []string{"Removes the matching links from <from>/dossier.md and rewrites its managed links block."},
		Examples: []string{"dossier unlink D-0007 D-0042", "dossier unlink D-0042 D-0051 --rel depends_on"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) { return a.Unlink(ctx.Str("from"), ctx.Str("to"), ctx.Str("rel")) })
		},
		Text: func(w io.Writer, r any) {
			l := r.(app.LinkResult)
			fmt.Fprintf(w, "%s no longer links to %s\n", l.From, l.To)
		},
	})
	spec.Register(&spec.Action{
		Category: "graph", Name: "tree", Summary: "Walk the links of a dossier: an agenda and what blocks its points.",
		Params: []spec.Param{
			idParam("Root dossier. Defaults to DOSSIER_ID."),
			{Name: "rel", Kind: spec.String, Default: "all", Enum: []string{"all", "includes", "depends_on"}, Help: "Link types to follow."},
			{Name: "depth", Kind: spec.String, Default: "2", Help: "Levels to walk."},
		},
		Examples: []string{"dossier tree D-0007", "dossier tree D-0007 --rel includes --depth 1 --format text"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, false, func(a *app.App) (any, error) {
				d, err := a.Load(ctx.Str("id"))
				if err != nil {
					return nil, err
				}
				depth, err := strconv.Atoi(ctx.Str("depth"))
				if err != nil || depth < 1 {
					return nil, spec.UserError("--depth takes a positive integer, got %q. Example: dossier tree %s --depth 2", ctx.Str("depth"), d.ID)
				}
				return a.Tree(d, ctx.Str("rel"), depth), nil
			})
		},
		Text: func(w io.Writer, r any) { printTree(w, r.(app.TreeNode), "") },
	})

	// ---------------------------------------------------------------- ingest
	spec.Register(&spec.Action{
		Category: "ingest", Name: "ingest", Summary: "Poll source connectors: signals open dossiers, events reach existing ones.",
		Params: []spec.Param{
			{Name: "source", Kind: spec.StringList, Positional: true, Help: "Source names; all declared sources when omitted."},
			{Name: "dry-run", Kind: spec.Bool, Help: "Show what would happen; write and start nothing."},
		},
		Effects: []string{
			"Opens one dossier per new signal and starts its session in the background.",
			"Sends an event prompt to the dossier of each event's thread, reopening or resuming it first.",
			"Advances the source cursor only after every item was handled.",
		},
		Examples: []string{"dossier ingest", "dossier ingest gmail", "dossier ingest gmail --dry-run"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) { return a.Ingest(ctx.List("source"), ctx.Bool("dry-run")) })
		},
		Text: func(w io.Writer, r any) {
			for _, rep := range r.([]app.IngestReport) {
				fmt.Fprintf(w, "%s: %d signal(s), %d event(s)\n", rep.Source, rep.Signals, rep.Events)
				for _, o := range rep.Opened {
					fmt.Fprintf(w, "  %s %s\n", o.ID, o.Outcome)
				}
				for _, s := range rep.Skipped {
					fmt.Fprintf(w, "  skip  %s\n", s)
				}
				for _, e := range rep.Errors {
					fmt.Fprintf(w, "  error %s\n", e)
				}
			}
		},
	})

	// ---------------------------------------------------------------- internal
	spec.Register(&spec.Action{
		Category: "internal", Name: "archive", Summary: "Copy the live session transcript into the dossier.",
		Params:   []spec.Param{idParam("Dossier id. Defaults to DOSSIER_ID.")},
		Examples: []string{"dossier archive D-0042"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, false, func(a *app.App) (any, error) {
				d, err := a.Load(ctx.Str("id"))
				if err != nil {
					return nil, err
				}
				return nil, a.Archive(d)
			})
		},
	})
	spec.Register(&spec.Action{
		Category: "internal", Name: "hook", Summary: "Agent hook entry point; reads the hook JSON on stdin.",
		Params: []spec.Param{{Name: "event", Kind: spec.String, Positional: true, Required: true, Enum: []string{"session-end", "guard"}, Help: "Hook event."}},
		Effects: []string{
			"session-end: copies the session transcript into the dossier whose directory is the session cwd.",
			"guard: answers a PreToolUse Bash hook with a deny verdict when a mutating command names an agent.protect path.",
			"Never fails the agent.",
		},
		Examples: []string{"dossier hook session-end < hook.json", "dossier hook guard < pretooluse.json"},
		Run: func(ctx *spec.Context) (any, error) {
			var in struct {
				SessionID      string `json:"session_id"`
				TranscriptPath string `json:"transcript_path"`
				Cwd            string `json:"cwd"`
				Reason         string `json:"reason"`
				ToolInput      struct {
					Command string `json:"command"`
				} `json:"tool_input"`
			}
			b, _ := io.ReadAll(io.LimitReader(ctx.Stdin, 1<<20))
			_ = json.Unmarshal(bytes.TrimSpace(b), &in)
			s, err := store.Resolve(ctx.Store)
			if err != nil && in.Cwd != "" {
				_ = os.Chdir(in.Cwd)
				s, err = store.Resolve("")
			}
			if err != nil {
				return nil, nil
			}
			a := &app.App{S: s}
			if ctx.Str("event") == "guard" {
				if verdict := a.Guard(in.ToolInput.Command); verdict != nil {
					_ = json.NewEncoder(os.Stdout).Encode(verdict)
				}
				return nil, nil
			}
			d, err := a.LoadDir(in.Cwd)
			if err != nil || in.TranscriptPath == "" {
				return nil, nil
			}
			_ = app.ArchiveFrom(in.TranscriptPath, d)
			_ = d.Log("session ended (%s), transcript archived", in.Reason)
			return nil, nil
		},
	})
}

func printTree(w io.Writer, n app.TreeNode, indent string) {
	label := n.ID + " · " + n.Title + " · " + n.State
	if n.Rel != "" {
		label = n.Rel + " → " + label
	}
	if len(n.BlockedBy) > 0 {
		label += " · blocked by " + strings.Join(n.BlockedBy, ", ")
	}
	if n.Seen {
		label += " · (cycle)"
	}
	fmt.Fprintln(w, indent+label)
	for _, c := range n.Children {
		printTree(w, c, indent+"  ")
	}
}
