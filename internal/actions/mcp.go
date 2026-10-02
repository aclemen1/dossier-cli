package actions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/aclemen1/dossier-cli/internal/app"
	"github.com/aclemen1/dossier-cli/internal/spec"
)

// tool projects an action onto an MCP tool scoped to the current dossier.
type tool struct {
	name, action, description string
	self                      []string          // params set to DOSSIER_ID
	rename                    map[string]string // action param → tool param
	hide                      []string
	check                     func(a *app.App, self string, args map[string]any) error
}

var tools = []tool{
	{name: "show", action: "show", self: []string{"id"}, description: "Show this dossier: state, sources, files, links, history, and the body of its fiche (instruction, notes, sections such as « À ne pas oublier »)."},
	{name: "peek", action: "show", description: "Read another dossier of the store, read-only."},
	{name: "search", action: "search", description: "Search every dossier of the store, open or closed."},
	{name: "grep", action: "grep", rename: map[string]string{"id": "dossier"},
		description: "Search the full conversation of this dossier, or of a dossier merged into it.",
		check:       grepScope},
	{name: "tree", action: "tree", self: []string{"id"}, description: "Walk this dossier's links: points it includes, what blocks them."},
	{name: "wait", action: "wait", self: []string{"id"}, description: "Mark this dossier as waiting on someone outside."},
	{name: "resume", action: "resume", self: []string{"id"}, hide: []string{"prompt"}, description: "Bring this waiting dossier back to open."},
	{name: "close", action: "close", self: []string{"id"}, description: "Close this dossier once the user says it is settled."},
	{name: "open", action: "open", self: []string{"parent"}, hide: []string{"source", "thread", "url"},
		description: "Open a new dossier that grows out of this one (it records this dossier as parent)."},
	{name: "link", action: "link", description: "Link this dossier, or one it opened, to another: includes (agenda item) or depends_on (waits for). from defaults to this dossier.",
		check: fromSelfOrChild},
	{name: "unlink", action: "unlink", description: "Remove links from this dossier, or one it opened, to another. from defaults to this dossier.",
		check: fromSelfOrChild},
	{name: "track", action: "track", self: []string{"id"},
		description: "Attach a thread to this dossier as a source, e.g. the thread of a draft you just wrote: its replies come back here, and its star follows the dossier's state."},
	{name: "merge", action: "merge", self: []string{"from"}, description: "Merge this dossier into another one, after the user agreed."},
	{name: "notify", action: "notify", self: []string{"from"}, description: "Tell a dossier that this one includes what was decided."},
}

// fromSelfOrChild lets a session link from its own dossier or from a dossier it
// opened (parent = this dossier).
func fromSelfOrChild(a *app.App, self string, args map[string]any) error {
	from, _ := args["from"].(string)
	if from == "" {
		args["from"] = self
		return nil
	}
	d, err := a.Load(from)
	if err != nil {
		return err
	}
	if d.ID == self || d.Parent == self {
		args["from"] = d.ID
		return nil
	}
	return spec.UserError("links start from this dossier (%s) or from a dossier it opened; %s is neither", self, d.ID)
}

func grepScope(a *app.App, self string, args map[string]any) error {
	target, _ := args["id"].(string)
	if target == "" {
		args["id"] = self
		return nil
	}
	d, err := a.Load(target)
	if err != nil {
		return err
	}
	if d.ID == self || d.MergedInto == self {
		return nil
	}
	return spec.UserError("grep reads this dossier (%s) or a dossier merged into it; %s is neither. Use search to look across dossiers", self, d.ID)
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func (t tool) schema() map[string]any {
	a := spec.FindVerb(t.action)
	props := map[string]any{}
	var required []string
	for _, p := range a.Params {
		if contains(t.self, p.Name) || contains(t.hide, p.Name) {
			continue
		}
		name := p.Name
		if r, ok := t.rename[p.Name]; ok {
			name = r
		}
		prop := map[string]any{"description": p.Help}
		switch p.Kind {
		case spec.Bool:
			prop["type"] = "boolean"
		case spec.StringList:
			prop["type"] = "array"
			prop["items"] = map[string]any{"type": "string"}
		default:
			prop["type"] = "string"
		}
		if len(p.Enum) > 0 {
			prop["enum"] = p.Enum
		}
		if p.Default != "" {
			prop["default"] = p.Default
		}
		props[name] = prop
		if p.Required && !(t.check != nil && (p.Name == "id" || p.Name == "from")) {
			required = append(required, name)
		}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// call maps tool arguments onto the action's argv, so that parsing, defaults
// and error messages are the CLI's own.
func (t tool) call(self string, in map[string]any) (any, error) {
	a := spec.FindVerb(t.action)
	args := map[string]any{}
	for k, v := range in {
		for from, to := range t.rename {
			if k == to {
				k = from
			}
		}
		if contains(t.self, k) {
			continue
		}
		args[k] = v
	}
	for _, p := range t.self {
		args[p] = self
	}
	if t.check != nil {
		ap, err := app.New(os.Getenv("DOSSIER_STORE"))
		if err != nil {
			return nil, err
		}
		if err := t.check(ap, self, args); err != nil {
			return nil, err
		}
	}
	var argv []string
	for _, p := range a.Params {
		v, ok := args[p.Name]
		if !ok || contains(t.hide, p.Name) {
			continue
		}
		switch x := v.(type) {
		case bool:
			if x {
				argv = append(argv, "--"+p.Name)
			}
		case []any:
			for _, e := range x {
				argv = append(argv, "--"+p.Name, fmt.Sprint(e))
			}
		default:
			if p.Positional {
				argv = append(argv, fmt.Sprint(x))
			} else {
				argv = append(argv, "--"+p.Name, fmt.Sprint(x))
			}
		}
	}
	if _, err := spec.Parse(a, argv); err != nil {
		return nil, err
	}
	return runAction(t.action, argv)
}

// runAction executes a tool call. Tests run it in process.
var runAction = runInstalled

func runInProcess(action string, argv []string) (any, error) {
	a := spec.FindVerb(action)
	parsed, err := spec.Parse(a, argv)
	if err != nil {
		return nil, err
	}
	return a.Run(&spec.Context{Args: parsed, Store: os.Getenv("DOSSIER_STORE"), Format: "json", Stdin: strings.NewReader("")})
}

// runInstalled runs the action with the dossier binary installed now, not the
// one this long-lived server was started from: an update reaches running
// sessions without a restart. Only the tool list itself stays as it was.
func runInstalled(action string, argv []string) (any, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, append([]string{action}, append(argv, "--format", "json")...)...)
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader("")
	out, runErr := cmd.Output()
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *spec.Error     `json:"error"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("dossier %s did not answer with an envelope (%v): %s", action, runErr, strings.TrimSpace(string(out)))
	}
	if !env.OK {
		if env.Error != nil {
			return nil, env.Error
		}
		return nil, fmt.Errorf("dossier %s failed", action)
	}
	var result any
	if len(env.Result) > 0 {
		_ = json.Unmarshal(env.Result, &result)
	}
	return result, nil
}

const mcpInstructions = "Tools of the dossier this session works on. They act on this dossier only, " +
	"except search and peek, which read the whole store. Close or merge only after the user said so."

// serveMCP speaks MCP over stdio, one JSON-RPC message per line.
func serveMCP(in io.Reader, out io.Writer) error {
	self := os.Getenv("DOSSIER_ID")
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var req struct {
			ID     *json.RawMessage `json:"id"`
			Method string           `json:"method"`
			Params json.RawMessage  `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.ID == nil {
			continue // notifications need no answer
		}
		reply := func(result any) { _ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) }
		fail := func(code int, msg string) {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": msg}})
		}
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.ProtocolVersion == "" {
				p.ProtocolVersion = "2025-06-18"
			}
			reply(map[string]any{
				"protocolVersion": p.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "dossier", "version": Version},
				"instructions":    mcpInstructions,
			})
		case "ping":
			reply(map[string]any{})
		case "tools/list":
			var list []map[string]any
			for _, t := range tools {
				list = append(list, map[string]any{"name": t.name, "description": t.description, "inputSchema": t.schema()})
			}
			reply(map[string]any{"tools": list})
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			var t *tool
			for i := range tools {
				if tools[i].name == p.Name {
					t = &tools[i]
				}
			}
			if t == nil {
				fail(-32602, "unknown tool "+p.Name)
				continue
			}
			if self == "" {
				reply(textResult(map[string]any{"ok": false, "error": "DOSSIER_ID is not set: this server serves one dossier session"}, true))
				continue
			}
			if p.Arguments == nil {
				p.Arguments = map[string]any{}
			}
			result, err := t.call(self, p.Arguments)
			if err != nil {
				reply(textResult(map[string]any{"ok": false, "error": spec.Internal(err)}, true))
				continue
			}
			reply(textResult(map[string]any{"ok": true, "result": result}, false))
		default:
			fail(-32601, "method not found: "+req.Method)
		}
	}
	return sc.Err()
}

func textResult(v any, isError bool) map[string]any {
	b, _ := json.Marshal(v)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}, "isError": isError}
}

func init() {
	spec.Register(&spec.Action{
		Category: "internal", Name: "mcp", Summary: "Serve this dossier's tools over MCP (stdio), scoped to DOSSIER_ID.",
		Discussion: "dossier passes this server to every session it starts. Tools: " + toolNames() + ".",
		Examples:   []string{"DOSSIER_ID=D-0042 dossier mcp"},
		Run: func(ctx *spec.Context) (any, error) {
			return nil, serveMCP(os.Stdin, os.Stdout)
		},
	})
}

func toolNames() string {
	var n []string
	for _, t := range tools {
		n = append(n, t.name)
	}
	return strings.Join(n, ", ")
}
