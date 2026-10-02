package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aclemen1/dossier-cli/internal/app"
	"github.com/aclemen1/dossier-cli/internal/dossier"
	"github.com/aclemen1/dossier-cli/internal/store"
)

func TestTreeNestsIncludedDossiersAndSurvivesCycles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	s, err := store.Init(filepath.Join(root, "pro"), "pro", false)
	if err != nil {
		t.Fatal(err)
	}
	a := &app.App{S: s}
	for _, title := range []string{"Séance", "Point A", "Point B", "Seul"} {
		if _, err := a.Open(app.OpenParams{Title: title, NoStart: true}); err != nil {
			t.Fatal(err)
		}
	}
	a.Link("1", "2", dossier.RelIncludes)
	a.Link("1", "3", dossier.RelIncludes)
	seance, _ := a.Load("1")
	seance.Parent = "D-0003"
	if err := seance.Save(); err != nil {
		t.Fatal(err)
	}
	if got := Stores(root); len(got) != 1 {
		t.Fatalf("stores %v", got)
	}
	rows, errs := load(Stores(root), false, "")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	var got []string
	for _, r := range rows {
		if r.d == nil {
			got = append(got, "#"+r.header)
			continue
		}
		line := strings.Repeat(">", r.depth) + r.d.Title
		if r.cycle {
			line += "↻"
		}
		got = append(got, line)
	}
	want := "#pro,Seul,Séance,>Point A,>Point B,>>Séance↻"
	if strings.Join(got, ",") != want {
		t.Fatalf("rows\n got %s\nwant %s", strings.Join(got, ","), want)
	}
	if rows, _ := load(Stores(root), false, "seul"); len(rows) != 2 || rows[1].d.Title != "Seul" {
		t.Fatalf("filter: %+v", rows)
	}
}
