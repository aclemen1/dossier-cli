package app

import (
	"strings"
	"testing"
)

func TestTrackAttachesAThreadThatTransitionsFollow(t *testing.T) {
	f := newFixture(t)
	f.a.Open(OpenParams{Title: "PostgreSQL", SourceRef: "fake:thread/a", ThreadRef: "fake:thread/a", NoStart: true})
	f.a.Open(OpenParams{Title: "Autre", SourceRef: "fake:thread/b", NoStart: true})
	if _, err := f.a.Track("1", "fake:thread/draft"); err != nil {
		t.Fatal(err)
	}
	d, _ := f.a.Load("1")
	if !d.HasSource("fake:thread/draft") || !d.HasThread("fake:thread/draft") {
		t.Fatalf("not tracked %+v", d)
	}
	f.a.SetState(d, "wait", "", "Camptocamp")
	if got := strings.Join(f.transitions(), ","); got != "open→waiting,open→waiting" {
		t.Fatalf("both threads should get the waiting transition: %s", got)
	}
	if _, err := f.a.Track("1", "fake:thread/b"); err == nil || !strings.Contains(err.Error(), "already belongs to D-0002") {
		t.Fatalf("thread of another dossier: %v", err)
	}
	if _, err := f.a.Track("1", "gmail:thread/x"); err == nil || !strings.Contains(err.Error(), "no [[source]] named") {
		t.Fatalf("unknown source: %v", err)
	}
	if _, err := f.a.Track("1", "nonsense"); err == nil {
		t.Fatal("bad reference accepted")
	}
}

func TestRestartClosesAndResumesTheSameSession(t *testing.T) {
	f := newFixture(t)
	f.a.Open(OpenParams{Title: "X"})
	d, _ := f.a.Load("1")
	panes = func() map[string]string { return map[string]string{"fake:p1": "idle"} }
	defer func() { panes = func() map[string]string { return map[string]string{} } }()
	if err := f.a.Restart(d); err != nil {
		t.Fatal(err)
	}
	if len(f.calls("session/close")) != 1 {
		t.Fatal("restart did not close the session")
	}
	loads := f.calls("session/load")
	if len(loads) < 2 || loads[len(loads)-1]["params"].(map[string]any)["sessionId"] != "sess-1" {
		t.Fatalf("restart did not resume the same session: %v", loads)
	}
	if d.Run.TabID != "fake:t1" {
		t.Fatalf("tab %q", d.Run.TabID)
	}
	light, _ := f.a.Open(OpenParams{Title: "Léger", NoStart: true})
	ld, _ := f.a.Load(light.ID)
	if err := f.a.Restart(ld); err == nil || !strings.Contains(err.Error(), "dossier attach") {
		t.Fatalf("restart without session: %v", err)
	}
}
