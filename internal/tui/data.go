package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aclemen1/dossier-cli/internal/app"
	"github.com/aclemen1/dossier-cli/internal/dossier"
	"github.com/aclemen1/dossier-cli/internal/store"
)

// row is one line of the list: a store header or a dossier, indented under the
// dossier that includes it or is its parent.
type row struct {
	header   string
	store    *storeView
	d        *dossier.Dossier
	activity string
	depth    int
	last     []bool // per ancestor level: was that ancestor the last child
	rel      string
	blocked  []string
	cycle    bool // already shown above on this branch
}

type storeView struct {
	name  string
	root  string
	a     *app.App
	byID  map[string]*dossier.Dossier
	count map[string]int
}

// Stores lists the stores under root: root itself when it is one, otherwise
// every direct subdirectory that holds a .dossier directory.
func Stores(root string) []string {
	if isStore(root) {
		return []string{root}
	}
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() && isStore(p) {
			out = append(out, p)
		}
	}
	return out
}

func isStore(p string) bool {
	fi, err := os.Stat(filepath.Join(p, ".dossier", "config.toml"))
	return err == nil && !fi.IsDir()
}

func shown(d *dossier.Dossier, all bool, filter string) bool {
	if !all && d.State != dossier.Open && d.State != dossier.Waiting {
		return false
	}
	if filter == "" {
		return true
	}
	hay := strings.ToLower(d.ID + " " + d.Alias + " " + d.Title + " " + d.WaitingOn)
	return strings.Contains(hay, strings.ToLower(filter))
}

func load(roots []string, all bool, filter string) ([]row, []string) {
	var rows []row
	var errs []string
	live := app.Panes()
	for _, root := range roots {
		s, err := store.Open(root)
		if err != nil {
			errs = append(errs, root+": "+err.Error())
			continue
		}
		a := &app.App{S: s}
		ds, err := a.All()
		if err != nil {
			errs = append(errs, root+": "+err.Error())
			continue
		}
		name := s.Config.Store.Sphere
		if name == "" {
			name = filepath.Base(root)
		}
		sv := &storeView{name: name, root: root, a: a, byID: map[string]*dossier.Dossier{}, count: map[string]int{}}
		for _, d := range ds {
			sv.byID[d.ID] = d
			sv.count[d.State]++
		}
		rows = append(rows, row{header: name, store: sv})
		rows = append(rows, treeRows(sv, ds, live, all, filter)...)
	}
	return rows, errs
}

// treeRows lays the shown dossiers out as a forest: a dossier appears under
// each shown dossier that includes it or is its parent, and at the top level
// only when no shown dossier holds it.
func treeRows(sv *storeView, ds []*dossier.Dossier, live map[string]string, all bool, filter string) []row {
	visible := map[string]bool{}
	for _, d := range ds {
		if shown(d, all, filter) {
			visible[d.ID] = true
		}
	}
	children := map[string][]string{}
	rels := map[[2]string]string{}
	held := map[string]bool{}
	addChild := func(parent, child, rel string) {
		if !visible[parent] || !visible[child] || parent == child {
			return
		}
		if _, dup := rels[[2]string{parent, child}]; dup {
			return
		}
		children[parent] = append(children[parent], child)
		rels[[2]string{parent, child}] = rel
		held[child] = true
	}
	for _, d := range ds {
		for _, id := range d.Targets(dossier.RelIncludes) {
			addChild(d.ID, id, "includes")
		}
		if d.Parent != "" {
			addChild(d.Parent, d.ID, "child")
		}
	}
	order := func(ids []string) {
		sort.SliceStable(ids, func(i, j int) bool { return less(sv.byID[ids[i]], sv.byID[ids[j]]) })
	}
	var roots []string
	for _, d := range ds {
		if visible[d.ID] && !held[d.ID] {
			roots = append(roots, d.ID)
		}
	}
	order(roots)
	idx := sv.byID
	var out []row
	var walk func(id string, depth int, last []bool, rel string, path map[string]bool)
	walk = func(id string, depth int, last []bool, rel string, path map[string]bool) {
		d := idx[id]
		out = append(out, row{store: sv, d: d, activity: app.Activity(d, live), depth: depth,
			last: append([]bool{}, last...), rel: rel, blocked: app.BlockedBy(d, idx), cycle: path[id]})
		if path[id] {
			return
		}
		path[id] = true
		kids := append([]string{}, children[id]...)
		order(kids)
		for i, k := range kids {
			walk(k, depth+1, append(last, i == len(kids)-1), rels[[2]string{id, k}], path)
		}
		delete(path, id)
	}
	for _, id := range roots {
		walk(id, 0, nil, "", map[string]bool{})
	}
	// A cycle of links leaves dossiers that no root reaches: start from them too.
	for {
		seen := map[string]bool{}
		for _, r := range out {
			seen[r.d.ID] = true
		}
		var rest []string
		for _, d := range ds {
			if visible[d.ID] && !seen[d.ID] {
				rest = append(rest, d.ID)
			}
		}
		if len(rest) == 0 {
			break
		}
		order(rest)
		walk(rest[0], 0, nil, "", map[string]bool{})
	}
	return out
}

// less puts lasting dossiers (with an alias) first, then open before waiting
// before closed, then by number.
func less(a, b *dossier.Dossier) bool {
	if (a.Alias != "") != (b.Alias != "") {
		return a.Alias != ""
	}
	if ra, rb := stateRank(a.State), stateRank(b.State); ra != rb {
		return ra < rb
	}
	return a.Num() < b.Num()
}

func stateRank(s string) int {
	switch s {
	case dossier.Open:
		return 0
	case dossier.Waiting:
		return 1
	case dossier.Done:
		return 2
	}
	return 3
}

func logTail(d *dossier.Dossier, n int) []string {
	b, err := os.ReadFile(d.Path("log.md"))
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "- ") {
			lines = append(lines, strings.TrimPrefix(l, "- "))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// linked is one dossier linked to the selected one, with every relation
// between them, named from the selected dossier's side.
type linked struct {
	id, title, state string
	rels             []string
}

var (
	outName = map[string]string{"includes": "includes", "depends_on": "depends on", "parent": "child of", "merged_into": "merged into"}
	inName  = map[string]string{"includes": "included by", "depends_on": "needed by", "parent": "parent of", "merged_into": "merged from"}
)

func links(sv *storeView, d *dossier.Dossier) []linked {
	var out []linked
	pos := map[string]int{}
	add := func(e app.Edge, names map[string]string) {
		rel := names[e.Rel]
		if rel == "" {
			rel = e.Rel
		}
		i, ok := pos[e.ID]
		if !ok {
			i = len(out)
			pos[e.ID] = i
			id := e.ID
			if t := sv.byID[e.ID]; t != nil {
				id = t.Label()
			}
			out = append(out, linked{id: id, title: e.Title, state: e.State})
		}
		for _, r := range out[i].rels {
			if r == rel {
				return
			}
		}
		out[i].rels = append(out[i].rels, rel)
	}
	for _, e := range sv.a.Outgoing(d) {
		add(e, outName)
	}
	for _, e := range sv.a.Incoming(d) {
		add(e, inName)
	}
	return out
}
