package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/banjo/nota/internal/protocol"
)

// bearer adds a device token to every request.
type bearer struct {
	tok  string
	base http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return b.base.RoundTrip(r)
}

// mcpSession connects an MCP client to the server's /mcp endpoint.
func (e *env) mcpSession(tok, query string) (*sdk.ClientSession, error) {
	e.t.Helper()
	tr := &sdk.StreamableClientTransport{Endpoint: e.ts.URL + MCPPath + query, DisableStandaloneSSE: true, MaxRetries: -1,
		HTTPClient: &http.Client{Transport: bearer{tok: tok, base: http.DefaultTransport}}}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil).Connect(context.Background(), tr, nil)
	if err == nil {
		e.t.Cleanup(func() { cs.Close() })
	}
	return cs, err
}

func tool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		var msg strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*sdk.TextContent); ok {
				msg.WriteString(tc.Text)
			}
		}
		t.Fatalf("%s: tool error: %s", name, msg.String())
	}
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("%s: %v in %s", name, err, b)
	}
}

func TestHTTPMCP(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("anton@example.com")
	if st, _, _ := e.put(tok, "Projekt/Plan.md", 0, "---\ntags: [work]\n---\n# Plan\n\nShip it.\n"); st != 200 {
		t.Fatal(st)
	}
	cs, err := e.mcpSession(tok, "")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Notes []struct{ Path, Title string }
		Count int
	}
	tool(t, cs, "list_notes", map[string]any{}, &list)
	if list.Count != 1 || list.Notes[0].Path != "Projekt/Plan.md" {
		t.Fatalf("list_notes: %+v", list)
	}
	var found struct{ Count int }
	tool(t, cs, "search_notes", map[string]any{"query": "ship"}, &found)
	if found.Count != 1 {
		t.Fatalf("search_notes: %+v", found)
	}
	var created struct{ Path, Hash string }
	tool(t, cs, "create_note", map[string]any{"title": "From an agent", "body": "Hello", "tags": []string{"ai"}}, &created)
	if created.Path != "Inbox/From an agent.md" {
		t.Fatalf("create_note: %+v", created)
	}
	var appended struct{ Content string }
	tool(t, cs, "append_to_note", map[string]any{"note": "Plan", "text": "- step two"}, &appended)
	if !strings.HasSuffix(appended.Content, "- step two\n") {
		t.Fatalf("append: %q", appended.Content)
	}
	var moved struct{ Path string }
	tool(t, cs, "move_note", map[string]any{"note": "From an agent", "folder": "Projekt"}, &moved)
	if moved.Path != "Projekt/From an agent.md" {
		t.Fatalf("move: %+v", moved)
	}
	// Every write is a version of the token's device, so sync pulls it.
	c := e.changes(tok, 1, 100)
	var got []string
	for _, en := range c.Entries {
		got = append(got, en.Path+" "+en.DeviceID)
	}
	if strings.Join(got, "|") != "Projekt/Plan.md dev-anton|Inbox/From an agent.md dev-anton|Projekt/From an agent.md dev-anton" {
		t.Fatalf("versions: %v", got)
	}
	res, body := e.do(tok, "GET", protocol.FilePath("main", "Projekt/Plan.md"), nil, "")
	if res.StatusCode != 200 || !strings.HasSuffix(string(body), "- step two\n") {
		t.Fatalf("sync sees the MCP edit: %d %q", res.StatusCode, body)
	}

	// Another user's token sees only their own vault; picking a vault
	// by name works, an unknown one is 404.
	eva := e.user("eva@example.com")
	ecs, err := e.mcpSession(eva, "?vault=main")
	if err != nil {
		t.Fatal(err)
	}
	tool(t, ecs, "list_notes", map[string]any{}, &list)
	if list.Count != 0 {
		t.Fatalf("eva sees %+v", list)
	}
	if _, err := e.mcpSession(eva, "?vault=nope"); err == nil {
		t.Fatal("unknown vault connected")
	}
	if _, err := e.mcpSession("nota_wrong", ""); err == nil {
		t.Fatal("bad token connected")
	}
	req, _ := http.NewRequest("POST", e.ts.URL+MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 401 || !strings.Contains(r.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("no token: %d %v", r.StatusCode, r.Header)
	}
}
