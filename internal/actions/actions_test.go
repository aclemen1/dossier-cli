package actions

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aclemen1/dossier-cli/internal/app"
	"github.com/aclemen1/dossier-cli/internal/spec"
	"github.com/aclemen1/dossier-cli/internal/store"
	"github.com/aclemen1/dossier-cli/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.Dispatch()
	runAction = runInProcess
	os.Exit(m.Run())
}

// storeWith creates a store with light dossiers (no session) and points the
// environment at it, as a dossier session would.
func storeWith(t *testing.T, titles ...string) *app.App {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := store.Init(filepath.Join(t.TempDir(), "s"), "test", false)
	if err != nil {
		t.Fatal(err)
	}
	a := &app.App{S: s}
	for _, title := range titles {
		if _, err := a.Open(app.OpenParams{Title: title, NoStart: true}); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DOSSIER_STORE", s.Root)
	t.Setenv("DOSSIER_ID", "D-0001")
	return a
}

type mcpClient struct {
	in  io.WriteCloser
	out *bufio.Scanner
	id  int
}

func startMCP(t *testing.T) *mcpClient {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { serveMCP(inR, outW); outW.Close() }()
	c := &mcpClient{in: inW, out: bufio.NewScanner(outR)}
	c.out.Buffer(make([]byte, 1<<20), 16<<20)
	t.Cleanup(func() { inW.Close() })
	return c
}

func (c *mcpClient) rpc(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	c.id++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	c.in.Write(append(b, '\n'))
	if !c.out.Scan() {
		t.Fatalf("%s: no answer", method)
	}
	var m map[string]any
	json.Unmarshal(c.out.Bytes(), &m)
	return m
}

// call runs a tool and returns the decoded envelope and the isError flag.
func (c *mcpClient) call(t *testing.T, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	r := c.rpc(t, "tools/call", map[string]any{"name": name, "arguments": args})["result"].(map[string]any)
	text := r["content"].([]any)[0].(map[string]any)["text"].(string)
	var env map[string]any
	json.Unmarshal([]byte(text), &env)
	return env, r["isError"].(bool)
}

func TestMCPListsScopedTools(t *testing.T) {
	storeWith(t, "Séance")
	c := startMCP(t)
	init := c.rpc(t, "initialize", map[string]any{"protocolVersion": "2025-06-18"})["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "dossier" {
		t.Fatalf("initialize %v", init)
	}
	list := c.rpc(t, "tools/list", map[string]any{})["result"].(map[string]any)["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, x := range list {
		m := x.(map[string]any)
		byName[m["name"].(string)] = m
	}
	for _, n := range []string{"show", "peek", "search", "grep", "wait", "close", "open", "link", "merge", "notify"} {
		if byName[n] == nil {
			t.Errorf("tool %s missing", n)
		}
	}
	props := func(n string) map[string]any {
		return byName[n]["inputSchema"].(map[string]any)["properties"].(map[string]any)
	}
	if _, ok := props("close")["id"]; ok {
		t.Fatal("close must not take an id: it acts on this dossier")
	}
	if _, ok := props("peek")["id"]; !ok {
		t.Fatal("peek must take an id")
	}
	if _, ok := props("open")["parent"]; ok {
		t.Fatal("open must not expose parent")
	}
	if _, ok := props("grep")["dossier"]; !ok {
		t.Fatal("grep must expose dossier")
	}
}

func TestMCPToolsActOnTheCurrentDossierOnly(t *testing.T) {
	a := storeWith(t, "Séance", "Armoire", "Autre")
	c := startMCP(t)
	c.rpc(t, "initialize", map[string]any{})

	if env, isErr := c.call(t, "wait", map[string]any{"on": "Baer SA"}); isErr || env["ok"] != true {
		t.Fatalf("wait %v", env)
	}
	if d, _ := a.Load("1"); d.State != "waiting" {
		t.Fatalf("D-0001 state %s", d.State)
	}
	if d, _ := a.Load("2"); d.State != "open" {
		t.Fatal("another dossier changed")
	}
	if env, isErr := c.call(t, "link", map[string]any{"to": "D-0002", "rel": "includes"}); isErr {
		t.Fatalf("link %v", env)
	}
	if env, isErr := c.call(t, "notify", map[string]any{"to": "D-0003", "text": "x"}); !isErr || !strings.Contains(env["error"].(map[string]any)["message"].(string), "does not include") {
		t.Fatalf("notify outside includes: %v", env)
	}
	if env, isErr := c.call(t, "notify", map[string]any{"to": "D-0002", "text": "Décidé : on attend."}); isErr {
		t.Fatalf("notify %v", env)
	}
	log, _ := os.ReadFile(mustLoad(t, a, "2").Path("log.md"))
	if !strings.Contains(string(log), "from D-0001: Décidé : on attend.") {
		t.Fatalf("notify not logged:\n%s", log)
	}
	if env, isErr := c.call(t, "grep", map[string]any{"pattern": "x", "dossier": "D-0002"}); !isErr || !strings.Contains(env["error"].(map[string]any)["message"].(string), "neither") {
		t.Fatalf("grep outside scope: %v", env)
	}
	if _, isErr := c.call(t, "grep", map[string]any{"pattern": "x"}); isErr {
		t.Fatal("grep on this dossier refused")
	}
	if env, isErr := c.call(t, "peek", map[string]any{"id": "D-0003"}); isErr || env["result"].(map[string]any)["title"] != "Autre" {
		t.Fatalf("peek %v", env)
	}
	if env, isErr := c.call(t, "open", map[string]any{"title": "Sous-affaire", "no-start": true}); isErr {
		t.Fatalf("open %v", env)
	} else if child := mustLoad(t, a, "4"); child.Parent != "D-0001" {
		t.Fatalf("child parent %q", child.Parent)
	}
	if env, isErr := c.call(t, "wait", map[string]any{}); !isErr || !strings.Contains(env["error"].(map[string]any)["message"].(string), "--on") {
		t.Fatalf("missing argument: %v", env)
	}
}

func mustLoad(t *testing.T, a *app.App, id string) *dossierView {
	t.Helper()
	d, err := a.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return &dossierView{d.Parent, d.Path}
}

type dossierView struct {
	Parent string
	Path   func(...string) string
}

func TestSkillInstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dossier")
	p, err := installSkill("claude", dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "---\nname: dossier") {
		t.Fatalf("skill %q", b)
	}
	if _, err := installSkill("cursor", ""); err == nil {
		t.Fatal("unsupported harness accepted")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if p, _ := installSkill("claude-code", ""); p != filepath.Join(home, ".claude", "skills", "dossier", "SKILL.md") {
		t.Fatalf("default path %s", p)
	}
}

func TestEveryActionHasExamplesAndSummary(t *testing.T) {
	for _, a := range spec.All() {
		if a.Summary == "" || len(a.Examples) == 0 {
			t.Errorf("%s %s needs a summary and an example", a.Category, a.Name)
		}
	}
}

func TestMCPLinksFromThisDossierOrOneItOpened(t *testing.T) {
	a := storeWith(t, "Limite de connexions", "Autre", "Accord")
	c := startMCP(t)
	c.rpc(t, "initialize", map[string]any{})
	if env, isErr := c.call(t, "open", map[string]any{"title": "Touch Base CI", "no-start": true}); isErr {
		t.Fatalf("open %v", env)
	}
	if env, isErr := c.call(t, "link", map[string]any{"from": "D-0004", "to": "D-0001", "rel": "includes"}); isErr {
		t.Fatalf("link from child: %v", env)
	}
	child, _ := a.Load("4")
	if !child.HasLink("includes", "D-0001") {
		t.Fatalf("child links %v", child.Links)
	}
	if env, isErr := c.call(t, "link", map[string]any{"to": "D-0003", "rel": "depends_on"}); isErr {
		t.Fatalf("link from self: %v", env)
	}
	env, isErr := c.call(t, "link", map[string]any{"from": "D-0002", "to": "D-0003", "rel": "includes"})
	if !isErr || !strings.Contains(env["error"].(map[string]any)["message"].(string), "none of these") {
		t.Fatalf("link from a foreign dossier: %v", env)
	}
	a.Link("1", "2", "includes")
	if env, isErr := c.call(t, "link", map[string]any{"from": "D-0002", "to": "D-0003", "rel": "depends_on"}); isErr {
		t.Fatalf("link from a point this dossier includes: %v", env)
	}
}

func TestMCPWaitUsesTheDefaultDelayAndNotifyWakes(t *testing.T) {
	a := storeWith(t, "Séance", "Point")
	c := startMCP(t)
	c.rpc(t, "initialize", map[string]any{})
	if env, isErr := c.call(t, "wait", map[string]any{"on": "JMR"}); isErr {
		t.Fatalf("wait %v", env)
	}
	d, _ := a.Load("1")
	until, err := time.Parse(time.RFC3339, d.WaitUntil)
	if err != nil || until.Sub(time.Now()) < 6*24*time.Hour || until.Sub(time.Now()) > 8*24*time.Hour {
		t.Fatalf("wait_until %q", d.WaitUntil)
	}
	if env, isErr := c.call(t, "wait", map[string]any{"on": "x", "until": "bientôt"}); !isErr {
		t.Fatalf("bad until accepted: %v", env)
	}
	pt, _ := a.Load("2")
	a.SetState(pt, "wait", "", "Baer SA")
	c.call(t, "resume", map[string]any{})
	c.call(t, "link", map[string]any{"to": "D-0002", "rel": "includes"})
	if env, isErr := c.call(t, "notify", map[string]any{"to": "D-0002", "text": "Décidé en séance."}); isErr {
		t.Fatalf("notify %v", env)
	}
	if pt, _ := a.Load("2"); pt.State != "open" {
		t.Fatalf("notify left the point %s", pt.State)
	}
}
