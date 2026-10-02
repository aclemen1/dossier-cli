package app

import (
	"strings"
	"testing"
	"time"
)

func TestStamp(t *testing.T) {
	at := time.Date(2026, 10, 2, 2, 16, 0, 0, time.FixedZone("CEST", 2*3600))
	if got := Stamp(at, "fr_CH"); got != "[dossier · vendredi 2 octobre 2026, 02:16 (+02:00)]" {
		t.Fatalf("fr %q", got)
	}
	if got := Stamp(at, ""); got != "[dossier · Friday 2 October 2026, 02:16 (+02:00)]" {
		t.Fatalf("en %q", got)
	}
}

func TestEveryPromptIsStamped(t *testing.T) {
	f := newFixture(t)
	f.a.S.Config.Prompt.Locale = "fr"
	saved := now
	now = func() time.Time { return time.Date(2026, 10, 5, 9, 30, 0, 0, time.FixedZone("CEST", 2*3600)) }
	defer func() { now = saved }()
	f.a.Open(OpenParams{Title: "Armoire"})
	d, _ := f.a.Load("1")
	f.a.Prompt(d, "Baer SA a rappelé.")
	for _, c := range f.calls("session/prompt") {
		if text := promptText(c); !strings.HasPrefix(text, "[dossier · lundi 5 octobre 2026, 09:30 (+02:00)]\n\n") {
			t.Fatalf("unstamped prompt %q", text)
		}
	}
}
