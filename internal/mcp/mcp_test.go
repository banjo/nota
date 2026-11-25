package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/hooks"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
)

var now = time.Date(2026, 11, 22, 10, 42, 0, 0, time.FixedZone("CET", 3600))

// connect serves a MemFS vault over the SDK's in-memory transport and
// returns a connected client session.
func connect(t *testing.T, files map[string]string) (*sdk.ClientSession, *vault.MemFS) {
	return connectWith(t, files, nil)
}

// connectWith is connect with a setup func for the vault (to attach hooks).
func connectWith(t *testing.T, files map[string]string, setup func(*vault.Vault, *events.Bus)) (*sdk.ClientSession, *vault.MemFS) {
	t.Helper()
	m := vault.NewMemFS(files)
	bus := events.New()
	v, err := vault.OpenFS(m, "mem", vault.Options{Bus: bus, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(v, bus)
	}
	ix, err := index.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	srv := NewServer(NewService(v, ix), "test", nil)
	st, ct := sdk.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, m
}

// call runs a tool and decodes its structured result into out. It fails
// the test on a tool error.
func call(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	res := callRaw(t, cs, name, args)
	if res.IsError {
		t.Fatalf("%s: tool error: %s", name, text(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("%s: %v in %s", name, err, b)
	}
}

func callRaw(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// toolError calls a tool that must fail and returns its message.
func toolError(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res := callRaw(t, cs, name, args)
	if !res.IsError {
		t.Fatalf("%s(%v) succeeded, want a tool error", name, args)
	}
	return text(res)
}

func text(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestTools(t *testing.T) {
	cs, m := connect(t, map[string]string{
		"Projekt/Plan.md":         "---\nstatus: draft\ntags: [projekt]\n---\n# Plan\n\nSe [[Möte med Åsa]].\n",
		"Projekt/Planering.md":    "Planering\n",
		".nota/views/drafts.yaml": "name: Drafts\nwhere: { status: draft }\n",
	})
	ctx := context.Background()

	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range lt.Tools {
		names = append(names, tl.Name)
		if tl.Description == "" || tl.InputSchema == nil || tl.OutputSchema == nil {
			t.Errorf("%s: missing description or schema", tl.Name)
		}
	}
	slices.Sort(names)
	want := []string{"append_to_note", "create_folder", "create_note", "get_backlinks", "list_folders", "list_notes", "list_tags", "list_views",
		"move_note", "read_note", "search_notes", "update_frontmatter", "update_note"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools %q, want %q", names, want)
	}

	// create_note goes to the inbox with tags and created in the frontmatter.
	var n Note
	call(t, cs, "create_note", map[string]any{"title": "Möte med Åsa", "body": "Agenda #möte\n", "tags": []string{"projekt"}}, &n)
	if n.Path != "Inbox/Möte med Åsa.md" || n.Title != "Möte med Åsa" {
		t.Fatalf("created %+v", n.NoteInfo)
	}
	wantContent := "---\ntags: [projekt]\ncreated: 2026-11-22T10:42:00+01:00\n---\n# Möte med Åsa\n\nAgenda #möte\n"
	if n.Content != wantContent {
		t.Fatalf("content %q, want %q", n.Content, wantContent)
	}
	if !slices.Equal(n.Tags, []string{"projekt", "möte"}) || n.Created == "" {
		t.Fatalf("tags %q created %q", n.Tags, n.Created)
	}
	if n.URI != "nota://note/Inbox/M%C3%B6te%20med%20%C3%85sa.md" {
		t.Fatalf("uri %q", n.URI)
	}

	var found NotesOutput
	call(t, cs, "search_notes", map[string]any{"query": "agenda"}, &found)
	if found.Count != 1 || found.Notes[0].Path != n.Path || found.Notes[0].Snippet == "" {
		t.Fatalf("search %+v", found)
	}

	var r Note
	call(t, cs, "read_note", map[string]any{"note": "Möte med Åsa"}, &r)
	if r.Hash != n.Hash || r.Body != "# Möte med Åsa\n\nAgenda #möte\n" {
		t.Fatalf("read %+v", r)
	}

	// update_note refuses a stale hash and keeps the note.
	msg := toolError(t, cs, "update_note", map[string]any{"note": n.Path, "body": "x\n", "expected_hash": "sha256:stale"})
	if !strings.Contains(msg, "changed since it was read") {
		t.Fatalf("stale hash: %s", msg)
	}
	call(t, cs, "update_note", map[string]any{"note": n.Path, "body": "# Möte med Åsa\n\nNy agenda\n", "expected_hash": r.Hash}, &r)
	if r.Content != "---\ntags: [projekt]\ncreated: 2026-11-22T10:42:00+01:00\nupdated: 2026-11-22T10:42:00+01:00\n---\n# Möte med Åsa\n\nNy agenda\n" {
		t.Fatalf("updated content %q", r.Content)
	}

	call(t, cs, "append_to_note", map[string]any{"note": n.Path, "text": "- punkt"}, &r)
	if !strings.HasSuffix(r.Content, "Ny agenda\n- punkt\n") {
		t.Fatalf("appended %q", r.Content)
	}

	call(t, cs, "update_frontmatter", map[string]any{"note": n.Path, "set": map[string]any{"status": "draft", "priority": 2}, "unset": []string{"nothing"}}, &r)
	if !strings.Contains(r.Content, "\npriority: 2\nstatus: draft\n") || r.Properties["status"] != "draft" {
		t.Fatalf("frontmatter %q", r.Content)
	}

	var ls NotesOutput
	call(t, cs, "list_notes", map[string]any{"where": []string{"status=draft"}, "sort": "path"}, &ls)
	if ls.Count != 2 || ls.Notes[0].Path != n.Path || ls.Notes[1].Path != "Projekt/Plan.md" {
		t.Fatalf("where %+v", ls)
	}
	call(t, cs, "list_notes", map[string]any{"view": "drafts", "folder": "Inbox"}, &ls)
	if ls.Count != 1 || ls.Notes[0].Path != n.Path {
		t.Fatalf("view+folder %+v", ls)
	}
	call(t, cs, "list_notes", map[string]any{"tag": "projekt", "limit": 1}, &ls)
	if ls.Count != 1 {
		t.Fatalf("tag+limit %+v", ls)
	}

	var bl BacklinksOutput
	call(t, cs, "get_backlinks", map[string]any{"note": n.Path}, &bl)
	if len(bl.Backlinks) != 1 || bl.Backlinks[0].From != "Projekt/Plan.md" {
		t.Fatalf("backlinks %+v", bl)
	}

	var mv MoveOutput
	call(t, cs, "move_note", map[string]any{"note": n.Path, "folder": "Projekt/Möten"}, &mv)
	if mv.From != n.Path || mv.Path != "Projekt/Möten/Möte med Åsa.md" {
		t.Fatalf("move %+v", mv)
	}
	if _, err := m.Stat("Projekt/Möten/Möte med Åsa.md"); err != nil {
		t.Fatal(err)
	}

	var fo FolderOutput
	call(t, cs, "create_folder", map[string]any{"path": "Arkiv/2027"}, &fo)
	if fo.Path != "Arkiv/2027" {
		t.Fatalf("create_folder %+v", fo)
	}
	if msg := toolError(t, cs, "create_folder", map[string]any{"path": "Arkiv/2027"}); !strings.Contains(msg, "already exists") {
		t.Fatalf("create_folder twice: %q", msg)
	}
	if msg := toolError(t, cs, "create_folder", map[string]any{"path": ".nota/x"}); !strings.Contains(msg, "hidden") {
		t.Fatalf("create_folder hidden: %q", msg)
	}
	var fl FoldersOutput
	call(t, cs, "list_folders", nil, &fl)
	wantFolders := []index.FolderInfo{
		{Path: "Arkiv", Name: "Arkiv"}, {Path: "Arkiv/2027", Name: "2027"},
		{Path: "Inbox", Name: "Inbox"}, // empty since the note was filed
		{Path: "Projekt", Name: "Projekt", Notes: 3}, {Path: "Projekt/Möten", Name: "Möten", Notes: 1},
	}
	if !slices.Equal(fl.Folders, wantFolders) {
		t.Fatalf("list_folders %+v, want %+v", fl.Folders, wantFolders)
	}

	var tags TagsOutput
	call(t, cs, "list_tags", nil, &tags)
	// update_note dropped the inline #möte.
	if !slices.Equal(tags.Tags, []index.TagCount{{Tag: "projekt", Count: 2}}) {
		t.Fatalf("tags %+v", tags)
	}

	var vs ViewsOutput
	call(t, cs, "list_views", nil, &vs)
	if len(vs.Views) != 1 || vs.Views[0].ID != "drafts" || vs.Views[0].Count != 2 {
		t.Fatalf("views %+v", vs)
	}

	// Errors come back as tool errors the model can read.
	if msg := toolError(t, cs, "read_note", map[string]any{"note": "Saknas"}); !strings.Contains(msg, "no note matches") {
		t.Fatalf("missing: %s", msg)
	}
	if msg := toolError(t, cs, "read_note", map[string]any{"note": "Pla"}); !strings.Contains(msg, "ambiguous") {
		t.Fatalf("ambiguous: %s", msg)
	}
	if msg := toolError(t, cs, "create_note", map[string]any{"title": " "}); !strings.Contains(msg, "title or a body") {
		t.Fatalf("empty create: %s", msg)
	}
	if msg := toolError(t, cs, "move_note", map[string]any{"note": "Projekt/Plan.md", "folder": "../out"}); !strings.Contains(msg, "leaves the vault") {
		t.Fatalf("escape: %s", msg)
	}
	if msg := toolError(t, cs, "search_notes", map[string]any{}); msg == "" {
		t.Fatal("search without query should explain")
	}
}

func TestMoveRewritesLinks(t *testing.T) {
	cs, m := connect(t, map[string]string{
		"Projekt/Plan.md": "# Plan\n",
		"Länk.md":         "[[Projekt/Plan#Mål]] and [plan](Projekt/Plan.md)\n",
	})
	var mv MoveOutput
	call(t, cs, "move_note", map[string]any{"note": "Projekt/Plan.md", "folder": "Arkiv"}, &mv)
	if mv.Links.Count != 2 || len(mv.Links.Notes) != 1 || mv.Links.Notes[0].Path != "Länk.md" {
		t.Fatalf("links %+v", mv.Links)
	}
	b, err := m.ReadFile("Länk.md")
	if err != nil || string(b) != "[[Arkiv/Plan#Mål]] and [plan](Arkiv/Plan.md)\n" {
		t.Fatalf("Länk.md = %q, %v", b, err)
	}
}

func TestResources(t *testing.T) {
	cs, _ := connect(t, map[string]string{"Möten/Möte.md": "---\na: 1\n---\nMöte\n", "Projekt/Plan.md": "---\na: 1\n---\nMöte\n"})
	ctx := context.Background()
	tr, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil || len(tr.ResourceTemplates) != 1 || tr.ResourceTemplates[0].URITemplate != URITemplate {
		t.Fatalf("templates %+v %v", tr, err)
	}
	for _, uri := range []string{"nota://note/Projekt/Plan.md", "nota://note/M%C3%B6ten/M%C3%B6te.md", "nota://note/M%C3%B6ten%2FM%C3%B6te"} {
		res, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("%s: %v", uri, err)
		}
		// Clients percent-encode non-ASCII names (RFC 6570); plain ASCII paths work as they are.
		if len(res.Contents) != 1 || res.Contents[0].Text != "---\na: 1\n---\nMöte\n" || res.Contents[0].MIMEType != "text/markdown" {
			t.Fatalf("%s: %+v", uri, res.Contents)
		}
	}
	if _, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: "nota://note/Saknas.md"}); err == nil {
		t.Fatal("missing resource should fail")
	}
}

func TestURI(t *testing.T) {
	for _, p := range []string{"a.md", "Möten/Möte med Åsa.md", "x/100% #1?.md"} {
		got, err := PathFromURI(URI(p))
		if err != nil || got != p {
			t.Errorf("round trip %q: %q %v", p, got, err)
		}
	}
}

// TestCreateFollowsTransformHook checks that create_note reports where a
// transform hook filed the note.
func TestCreateFollowsTransformHook(t *testing.T) {
	cfg, err := hooks.Parse([]byte("[[hook]]\nname = \"sort\"\nevent = \"inbox.added\"\nmode = \"transform\"\nrun = \"x\"\nmatch = { tag = \"jobb\" }\n"))
	if err != nil {
		t.Fatal(err)
	}
	fake := func(context.Context, hooks.Hook, string, []string, []byte) ([]byte, error) {
		return []byte(`{"folder":"Jobb","tags":{"add":["sorterad"]}}`), nil
	}
	cs, m := connectWith(t, nil, func(v *vault.Vault, bus *events.Bus) {
		hooks.New(v, cfg, hooks.Options{Exec: fake}).Attach(bus)
	})
	var n Note
	call(t, cs, "create_note", map[string]any{"body": "Ring Anna", "tags": []string{"jobb"}}, &n)
	if n.Path != "Jobb/Ring Anna.md" || !slices.Contains(n.Tags, "sorterad") {
		t.Fatalf("created %+v", n.NoteInfo)
	}
	if _, err := m.Stat("Jobb/Ring Anna.md"); err != nil {
		t.Fatal(err)
	}
	call(t, cs, "create_note", map[string]any{"body": "Handla"}, &n)
	if n.Path != "Inbox/Handla.md" {
		t.Fatalf("unmatched note filed: %q", n.Path)
	}
}
