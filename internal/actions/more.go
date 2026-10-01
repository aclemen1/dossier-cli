package actions

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aclemen1/dossier-cli/internal/app"
	"github.com/aclemen1/dossier-cli/internal/dossier"
	"github.com/aclemen1/dossier-cli/internal/spec"
	"github.com/aclemen1/dossier-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "dossier", Name: "grep", Summary: "Search a dossier's full conversation, compactions included.",
		Params: []spec.Param{
			idParam("Dossier id. Defaults to DOSSIER_ID."),
			{Name: "pattern", Kind: spec.String, Positional: true, Required: true, Help: "Regular expression, case-insensitive."},
			{Name: "limit", Kind: spec.String, Default: "50", Help: "Maximum number of hits."},
		},
		Examples: []string{`dossier grep D-0042 "date de passage"`, `dossier grep D-0042 "devis|offre" --limit 10`},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, false, func(a *app.App) (any, error) {
				d, err := a.Load(ctx.Str("id"))
				if err != nil {
					return nil, err
				}
				limit, _ := strconv.Atoi(ctx.Str("limit"))
				return a.Grep(d, ctx.Str("pattern"), limit)
			})
		},
		Text: func(w io.Writer, r any) {
			for _, h := range r.([]app.GrepHit) {
				fmt.Fprintf(w, "%s %s:%d %s: %s\n", h.Dossier, h.Source, h.Line, h.Role, h.Text)
			}
		},
	})

	spec.Register(&spec.Action{
		Category: "graph", Name: "merge", Summary: "Merge one dossier into another: files, sources, threads and links move over.",
		Discussion: "<from> becomes merged and points at <into>; its session tab closes. A done <into> is reopened. " +
			"The session of <into> hears about the merge; the conversation of <from> stays searchable with grep.",
		Params: []spec.Param{
			{Name: "from", Kind: spec.String, Positional: true, Required: true, Help: "Dossier that disappears into the other."},
			{Name: "into", Kind: spec.String, Required: true, Help: "Dossier that receives everything."},
		},
		Effects: []string{
			"Copies <from>/context and <from>/files into <into>; moves sources, threads and links.",
			"Sets <from> to merged with merged_into; closes its tab.",
			"Reopens <into> when it was done (source transitions follow) and prompts its session.",
		},
		Destructive: true,
		Examples:    []string{"dossier merge D-0051 --into D-0042"},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) { return a.Merge(ctx.Str("from"), ctx.Str("into")) })
		},
		Text: func(w io.Writer, r any) {
			m := r.(app.MergeResult)
			fmt.Fprintf(w, "%s merged into %s · %d file(s)\n", m.From, m.Into, len(m.Files))
		},
	})

	spec.Register(&spec.Action{
		Category: "graph", Name: "notify", Summary: "Send an event to a dossier that <from> includes (agenda follow-up).",
		Discussion: "Only along an includes link: a meeting dossier tells each of its points what was decided.",
		Params: []spec.Param{
			{Name: "from", Kind: spec.String, Positional: true, Required: true, Help: "Dossier that includes <to>."},
			{Name: "to", Kind: spec.String, Positional: true, Required: true, Help: "Included dossier."},
			{Name: "text", Kind: spec.String, Required: true, Help: "What <to> must know."},
		},
		Effects:  []string{"Logs the event in <to> and prompts its session; a dossier without session only gets the log line."},
		Examples: []string{`dossier notify D-0007 D-0042 --text "Décidé en séance du 08.10 : on attend l'offre."`},
		Run: func(ctx *spec.Context) (any, error) {
			return withApp(ctx, true, func(a *app.App) (any, error) {
				from, err := a.Load(ctx.Str("from"))
				if err != nil {
					return nil, err
				}
				to, err := a.Load(ctx.Str("to"))
				if err != nil {
					return nil, err
				}
				if !from.HasLink(dossier.RelIncludes, to.ID) {
					return nil, spec.UserError("%s does not include %s: notify only follows includes links. Add one with `dossier link %s %s --rel includes`", from.ID, to.ID, from.ID, to.ID)
				}
				text := fmt.Sprintf("From dossier %s (%s): %s", from.ID, from.Title, ctx.Str("text"))
				_ = to.Log("from %s: %s", from.ID, ctx.Str("text"))
				if to.Run.Session == "" {
					return map[string]any{"to": to.ID, "prompted": false}, to.Save()
				}
				return map[string]any{"to": to.ID, "prompted": true}, a.Prompt(to, text)
			})
		},
	})

	spec.Register(&spec.Action{
		Category: "internal", Name: "closetab", Summary: "Wait, then close a dossier's tab; the session stays resumable.",
		Params: []spec.Param{
			idParam("Dossier id."),
			{Name: "delay", Kind: spec.String, Default: "0s", Help: "Wait before closing, e.g. 8s."},
		},
		Examples: []string{"dossier closetab D-0042 --delay 8s"},
		Run: func(ctx *spec.Context) (any, error) {
			if d, err := time.ParseDuration(ctx.Str("delay")); err == nil {
				time.Sleep(d)
			}
			return withApp(ctx, true, func(a *app.App) (any, error) {
				d, err := a.Load(ctx.Str("id"))
				if err != nil {
					return nil, err
				}
				return nil, a.CloseTab(d)
			})
		},
	})
}

// installSkill writes the embedded skill where an agent harness finds it.
func installSkill(target, dir string) (string, error) {
	if dir == "" {
		switch strings.ToLower(target) {
		case "claude", "claude-code", "claudecode":
			dir = store.ExpandHome("~/.claude/skills/dossier")
		default:
			return "", spec.UserError("--for %q is not supported yet; use --for claude, or --dir <skills directory>/dossier", target)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "SKILL.md")
	return p, os.WriteFile(p, []byte(skillText), 0o644)
}
