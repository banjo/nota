package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/banjo/nota/internal/plugins"
	"github.com/banjo/nota/internal/protocol"
)

// webUser is a signed-in browser with a vault "main", plus a device
// token of the same account for sync.
func webUser(t *testing.T, e *env, email string) (*browser, string) {
	t.Helper()
	b := signedIn(t, e, email)
	if st, m := b.call("POST", "/v1/api/vaults", map[string]string{"name": "main"}, nil); st != 201 {
		t.Fatalf("create vault: %d %v", st, m)
	}
	u, _ := e.srv.Store().UserByEmail(context.Background(), email)
	tok, _, err := e.srv.Store().CreateToken(context.Background(), u.ID, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	return b, tok
}

const api = "/v1/api/vaults/main"

func notePathURL(p string) string { return api + "/notes/" + protocol.EscapePath(p) }

// callBase is call with a Base-Version header.
func (b *browser) callBase(method, path string, base int64, body any, out any) (int, map[string]any) {
	b.t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, b.e.ts.URL+path, strings.NewReader(string(data)))
	req.Header.Set("Nota-Protocol", "1")
	req.Header.Set(HeaderClient, "web")
	req.Header.Set("Origin", b.origin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(protocol.HeaderBaseVersion, strconv.FormatInt(base, 10))
	res, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &m)
	if out != nil && res.StatusCode/100 == 2 {
		_ = json.Unmarshal(raw, out)
	}
	return res.StatusCode, m
}

func TestWebAPINotes(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")

	// Create: default inbox, title as file name, tags in frontmatter.
	var n Note
	if st, m := b.call("POST", api+"/notes", CreateRequest{Title: "Plan", Body: "See [[Möte]].\n", Tags: []string{"work"}}, &n); st != 201 {
		t.Fatalf("create: %d %v", st, m)
	}
	if n.Path != "Inbox/Plan.md" || n.Version != 1 || n.Title != "Plan" || len(n.Tags) != 1 || n.Hash != protocol.Hash([]byte(n.Content)) {
		t.Fatalf("created: %+v", n)
	}
	// Sync clients see it as a version by the web device.
	c := e.changes(tok, 0, 10)
	if len(c.Entries) != 1 || c.Entries[0].Path != "Inbox/Plan.md" || c.Entries[0].DeviceID != DeviceWeb {
		t.Fatalf("changes: %+v", c)
	}
	// A note pushed by sync shows up in the web API, with backlinks.
	if st, _, perr := e.put(tok, "Möte.md", 0, "---\nstatus: draft\ntags: [work]\n---\n# Möte\n"); st != 200 {
		t.Fatalf("sync put: %d %v", st, perr)
	}
	var mote Note
	if st, m := b.call("GET", notePathURL("Möte"), nil, &mote); st != 200 {
		t.Fatalf("get: %d %v", st, m)
	}
	if mote.Version != 2 || mote.Properties["status"] != "draft" || mote.Body != "# Möte\n" || len(mote.Backlinks) != 1 || mote.Backlinks[0].From != "Inbox/Plan.md" {
		t.Fatalf("note: %+v", mote)
	}

	// Save with the right base works; a stale one is a 409 like sync.
	var saved Note
	newBody := "# Möte\n\nTid: 10:00\n"
	if st, m := b.callBase("PUT", notePathURL("Möte.md"), 2, SaveRequest{Body: &newBody}, &saved); st != 200 || saved.Version != 3 {
		t.Fatalf("save: %d %v %+v", st, m, saved)
	}
	// Saves through Nota keep `updated` current; the rest stays as it was.
	if !strings.HasPrefix(saved.Content, "---\nstatus: draft\ntags: [work]\nupdated: ") || !strings.HasSuffix(saved.Content, "\n---\n"+newBody) {
		t.Fatalf("save lost the frontmatter: %q", saved.Content)
	}
	st, m := b.callBase("PUT", notePathURL("Möte.md"), 2, SaveRequest{Body: &newBody}, nil)
	if st != 409 || code(m) != protocol.CodeVersionMismatch || m["current"].(map[string]any)["version"].(float64) != 3 {
		t.Fatalf("stale save: %d %v", st, m)
	}
	// The sync client pulls the web edit.
	res, body := e.do(tok, "GET", protocol.FilePath("main", "Möte.md"), nil, "")
	if res.StatusCode != 200 || !strings.HasSuffix(string(body), "Tid: 10:00\n") || res.Header.Get(protocol.HeaderDevice) != DeviceWeb {
		t.Fatalf("sync get: %d %q", res.StatusCode, body)
	}
	// Creating at an exact path with base 0; a second time is a 409.
	content := "# Exact\n"
	if st, m := b.callBase("PUT", notePathURL("Projekt/Exact.md"), 0, SaveRequest{Content: &content}, nil); st != 200 {
		t.Fatalf("create by PUT: %d %v", st, m)
	}
	if st, m := b.callBase("PUT", notePathURL("Projekt/Exact.md"), 0, SaveRequest{Content: &content}, nil); st != 409 {
		t.Fatalf("PUT base 0 on an existing note: %d %v", st, m)
	}

	// Properties: only the touched lines change.
	var patched Note
	if st, m := b.call("PATCH", notePathURL("Möte.md"), PatchRequest{Set: map[string]any{"status": "done", "prio": 2}}, &patched); st != 200 {
		t.Fatalf("patch: %d %v", st, m)
	}
	if !strings.HasPrefix(patched.Content, "---\nstatus: done\ntags: [work]\nupdated: ") || !strings.HasSuffix(patched.Content, "\nprio: 2\n---\n# Möte\n\nTid: 10:00\n") {
		t.Fatalf("patched: %q", patched.Content)
	}
	pr := PatchRequest{Unset: []string{"prio"}}
	pr.Tags.Add = []string{"möte"}
	if st, m := b.call("PATCH", notePathURL("Möte.md"), pr, &patched); st != 200 || strings.Contains(patched.Content, "prio") || !strings.Contains(patched.Content, "tags: [work, möte]") {
		t.Fatalf("unset+tag: %d %v %q", st, m, patched.Content)
	}

	// Move into a folder, then rename; history follows.
	var mv MoveResponse
	if st, m := b.call("POST", api+"/move", MoveRequest{Path: "Inbox/Plan", Folder: "Projekt"}, &mv); st != 200 || mv.Path != "Projekt/Plan.md" {
		t.Fatalf("move: %d %v %+v", st, m, mv)
	}
	if st, m := b.call("POST", api+"/move", MoveRequest{Path: "Projekt/Exact.md", Folder: "Projekt"}, nil); st != 200 {
		t.Fatalf("move in place: %d %v", st, m)
	}
	if st, m := b.call("POST", api+"/move", MoveRequest{Path: "Projekt/Plan.md", To: "Projekt/Exact.md"}, nil); st != 409 || code(m) != protocol.CodeExists {
		t.Fatalf("move onto a note: %d %v", st, m)
	}
	if st, m := b.call("GET", notePathURL("Inbox/Plan.md"), nil, nil); st != 404 {
		t.Fatalf("old path: %d %v", st, m)
	}

	// Delete: a tombstone for sync, gone from the API.
	if st, m := b.call("DELETE", notePathURL("Projekt/Exact.md"), nil, nil); st != 200 || m["deleted"] != true {
		t.Fatalf("delete: %d %v", st, m)
	}
	if st, _ := b.call("GET", notePathURL("Projekt/Exact.md"), nil, nil); st != 404 {
		t.Fatalf("deleted note: %d", st)
	}
	res, _ = e.do(tok, "GET", protocol.FilePath("main", "Projekt/Exact.md"), nil, "")
	if res.StatusCode != 410 {
		t.Fatalf("sync get of the deleted note: %d", res.StatusCode)
	}

	// History and restore.
	var h protocol.History
	if st, m := b.call("GET", api+"/history/"+protocol.EscapePath("Möte.md"), nil, &h); st != 200 || len(h.Versions) != 4 {
		t.Fatalf("history: %d %v %+v", st, m, h)
	}
	first := h.Versions[len(h.Versions)-1]
	var old HistoryEntry
	if st, m := b.call("GET", api+"/history/"+protocol.EscapePath("Möte.md")+"?version="+itoa(first.Version), nil, &old); st != 200 || old.Content == nil || *old.Content != "---\nstatus: draft\ntags: [work]\n---\n# Möte\n" {
		t.Fatalf("old version: %d %v %+v", st, m, old)
	}
	var restored protocol.Entry
	if st, m := b.call("POST", api+"/history/"+protocol.EscapePath("Möte.md")+"/restore", RestoreRequest{Version: first.Version}, &restored); st != 200 || restored.Hash != first.Hash || restored.Version <= h.Versions[0].Version {
		t.Fatalf("restore: %d %v %+v", st, m, restored)
	}
	if st, _ := b.call("GET", api+"/history/"+protocol.EscapePath("Möte.md"), nil, &h); st != 200 || len(h.Versions) != 5 {
		t.Fatalf("history after restore: %+v", h)
	}
	if st, m := b.call("POST", api+"/history/Möte.md/restore", RestoreRequest{Version: 999}, nil); st != 404 {
		t.Fatalf("restore unknown version: %d %v", st, m)
	}

	// Read-only views of the vault.
	var tree Tree
	if st, m := b.call("GET", api+"/tree", nil, &tree); st != 200 || len(tree.Notes) != 2 || tree.Inbox != "Inbox" ||
		// Inbox is still on disk, empty, after its note was filed.
		len(tree.Folders) != 2 || tree.Folders[0] != (TreeFolder{Path: "Inbox", Name: "Inbox"}) ||
		tree.Folders[1] != (TreeFolder{Path: "Projekt", Name: "Projekt", Notes: 1}) || tree.Cursor == 0 {
		t.Fatalf("tree: %d %v %+v", st, m, tree)
	}
	var fr FolderResponse
	if st, m := b.call("POST", api+"/folders", FolderRequest{Path: "Tomt/Under"}, &fr); st != 201 || fr.Path != "Tomt/Under" {
		t.Fatalf("create folder: %d %v %+v", st, m, fr)
	}
	if st, m := b.call("POST", api+"/folders", FolderRequest{Path: "Tomt/Under"}, nil); st != 409 || code(m) != protocol.CodeExists {
		t.Fatalf("create folder again: %d %v", st, m)
	}
	if st, m := b.call("POST", api+"/folders", FolderRequest{Path: "tomt"}, nil); st != 409 || code(m) != protocol.CodeCaseCollision {
		t.Fatalf("create folder, case: %d %v", st, m)
	}
	if st, m := b.call("POST", api+"/folders", FolderRequest{Path: ".nota/x"}, nil); st != 400 || code(m) != protocol.CodeInvalidPath {
		t.Fatalf("create hidden folder: %d %v", st, m)
	}
	var withEmpty Tree
	if st, m := b.call("GET", api+"/tree", nil, &withEmpty); st != 200 || !slices.Contains(withEmpty.Folders, TreeFolder{Path: "Tomt/Under", Name: "Under"}) ||
		!slices.Contains(withEmpty.Folders, TreeFolder{Path: "Tomt", Name: "Tomt"}) {
		t.Fatalf("tree with empty folders: %d %v %+v", st, m, withEmpty)
	}
	var list NotesList
	if st, _ := b.call("GET", api+"/notes?tag=work", nil, &list); st != 200 || list.Count != 2 {
		t.Fatalf("tag filter: %+v", list)
	}
	if st, _ := b.call("GET", api+"/notes?where="+url.QueryEscape("status=draft"), nil, &list); st != 200 || list.Count != 1 || list.Notes[0].Path != "Möte.md" {
		t.Fatalf("where filter: %+v", list)
	}
	if st, _ := b.call("GET", api+"/notes?folder=Projekt", nil, &list); st != 200 || list.Count != 1 {
		t.Fatalf("folder filter: %+v", list)
	}
	// Renaming a folder moves its notes; renaming it back restores them.
	var rf RenameFolderResponse
	if st, m := b.call("POST", api+"/folders/rename", RenameFolderRequest{Path: "Projekt", To: "Arbete"}, &rf); st != 200 || rf.From != "Projekt" || rf.Path != "Arbete" {
		t.Fatalf("rename folder: %d %v %+v", st, m, rf)
	}
	if st, _ := b.call("GET", api+"/notes?folder=Arbete", nil, &list); st != 200 || list.Count != 1 {
		t.Fatalf("folder after rename: %+v", list)
	}
	if st, m := b.call("POST", api+"/folders/rename", RenameFolderRequest{Path: "Arbete", To: "Tomt"}, nil); st != 409 || code(m) != protocol.CodeExists {
		t.Fatalf("rename onto a folder: %d %v", st, m)
	}
	if st, m := b.call("POST", api+"/folders/rename", RenameFolderRequest{Path: "Nej", To: "Ja"}, nil); st != 404 {
		t.Fatalf("rename a missing folder: %d %v", st, m)
	}
	if st, m := b.call("POST", api+"/folders/rename", RenameFolderRequest{Path: "Arbete", To: "Projekt"}, nil); st != 200 {
		t.Fatalf("rename back: %d %v", st, m)
	}
	if st, m := b.call("GET", api+"/notes?sort=bogus", nil, nil); st != 400 {
		t.Fatalf("bad sort: %d %v", st, m)
	}
	if st, _ := b.call("GET", api+"/search?q=tid", nil, &list); st != 200 || list.Count != 0 {
		t.Fatalf("search after restore: %+v", list) // the restored version has no "Tid"
	}
	if st, _ := b.call("GET", api+"/search?q=see", nil, &list); st != 200 || list.Count != 1 || list.Notes[0].Snippet == "" {
		t.Fatalf("search: %+v", list)
	}
	if st, m := b.call("GET", api+"/search?q=", nil, nil); st != 400 {
		t.Fatalf("empty search: %d %v", st, m)
	}
	var tags struct {
		Tags []struct {
			Tag   string
			Count int
		}
	}
	if st, _ := b.call("GET", api+"/tags", nil, &tags); st != 200 || len(tags.Tags) != 1 || tags.Tags[0].Count != 2 {
		t.Fatalf("tags: %+v", tags)
	}
	if st, _ := b.call("POST", api+"/notes", CreateRequest{Body: "Quick thought\n"}, nil); st != 201 {
		t.Fatal(st)
	}
	if st, _ := b.call("GET", api+"/inbox", nil, &list); st != 200 || list.Count != 1 || list.Notes[0].Title != "Quick thought" {
		t.Fatalf("inbox: %+v", list)
	}
	// A view synced into .nota/views/ lists and filters.
	if st, _, _ := e.put(tok, ".nota/views/drafts.yaml", 0, "name: Drafts\nwhere: { status: draft }\n"); st != 200 {
		t.Fatal(st)
	}
	var vs struct{ Views []ViewInfo }
	if st, m := b.call("GET", api+"/views", nil, &vs); st != 200 || len(vs.Views) != 1 || vs.Views[0].ID != "drafts" || vs.Views[0].Count != 1 {
		t.Fatalf("views: %d %v %+v", st, m, vs)
	}
	if st, _ := b.call("GET", api+"/notes?view=drafts", nil, &list); st != 200 || list.Count != 1 {
		t.Fatalf("view list: %+v", list)
	}
	if st, _ := b.call("GET", api+"/notes?view=nope", nil, nil); st != 404 {
		t.Fatalf("unknown view: %d", st)
	}
	// A plugin synced into .nota/plugins/ brings its themes.
	var ps struct{ Plugins []plugins.Plugin }
	if st, m := b.call("GET", api+"/plugins", nil, &ps); st != 200 || ps.Plugins == nil || len(ps.Plugins) != 0 {
		t.Fatalf("no plugins: %d %v %+v", st, m, ps)
	}
	manifest := `{"id":"night","contributes":{"themes":[{"id":"ink","dark":{"background":"#000000","text":"#ffffff","ink":"#8be9fd"}}]}}`
	if st, _, _ := e.put(tok, ".nota/plugins/night/manifest.json", 0, manifest); st != 200 {
		t.Fatal(st)
	}
	if st, m := b.call("GET", api+"/plugins", nil, &ps); st != 200 || len(ps.Plugins) != 1 ||
		ps.Plugins[0].Dir != ".nota/plugins/night" || ps.Plugins[0].Contributes.Themes[0].Dark["ink"] != "#8be9fd" {
		t.Fatalf("plugins: %d %v %+v", st, m, ps)
	}
	// Raw files (attachments) for the browser.
	if st, _, _ := e.put(tok, "attachments/a.png", 0, "PNG"); st != 200 {
		t.Fatal(st)
	}
	if st, _ := b.call("GET", api+"/files/attachments/a.png", nil, nil); st != 200 {
		t.Fatalf("file: %d", st)
	}
}

// raw sends body as is, as the web app's fetch does for files.
func (b *browser) raw(method, path, body string, hdr map[string]string) (*http.Response, string) {
	b.t.Helper()
	req, _ := http.NewRequest(method, b.e.ts.URL+path, strings.NewReader(body))
	req.Header.Set(HeaderClient, "web")
	req.Header.Set("Origin", b.origin)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, string(data)
}

func TestWebAPIAttachments(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen, MaxFileSize: 64})
	b, tok := webUser(t, e, "anton@example.com")
	attach := func(name, body string) (int, string) {
		res, data := b.raw("POST", api+"/attachments?protocol=1&name="+url.QueryEscape(name), body, nil)
		var out AttachResult
		_ = json.Unmarshal([]byte(data), &out)
		return res.StatusCode, out.Path
	}
	// Pasted images land in Attachments/ under a free name, as versions
	// that sync clients pull.
	if st, p := attach("Pasted image 1.png", "PNG1"); st != 201 || p != "Attachments/Pasted image 1.png" {
		t.Fatalf("attach: %d %q", st, p)
	}
	if st, p := attach("Pasted image 1.png", "PNG2"); st != 201 || p != "Attachments/Pasted image 1 2.png" {
		t.Fatalf("attach again: %d %q", st, p)
	}
	if st, _ := attach("x.html", "<script>"); st != 400 {
		t.Fatalf("not an image: %d", st)
	}
	if st, _ := attach("big.png", strings.Repeat("x", 65)); st != 413 {
		t.Fatalf("too large: %d", st)
	}
	if res, body := e.do(tok, "GET", "/v1/vaults/main/files/Attachments/Pasted%20image%201.png", nil, ""); res.StatusCode != 200 || string(body) != "PNG1" {
		t.Fatalf("synced: %d %q", res.StatusCode, body)
	}

	// Served with their type, safely, and kept by the browser.
	res, body := b.raw("GET", api+"/files/Attachments/Pasted%20image%201.png?protocol=1", "", nil)
	h := res.Header
	if res.StatusCode != 200 || body != "PNG1" || h.Get("Content-Type") != "image/png" ||
		h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") ||
		h.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("file: %d %q %v", res.StatusCode, body, h)
	}
	if res, _ := b.raw("GET", api+"/files/Attachments/Pasted%20image%201.png?protocol=1", "", map[string]string{"If-None-Match": h.Get("ETag")}); res.StatusCode != 304 {
		t.Fatalf("revalidate: %d", res.StatusCode)
	}
	if st, _, _ := e.put(tok, "notes/page.html", 0, "<script>alert(1)</script>"); st != 200 {
		t.Fatal(st)
	}
	if res, _ := b.raw("GET", api+"/files/notes/page.html?protocol=1", "", nil); res.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("other files: %v", res.Header)
	}

	// ?from= finds an embed's target as Obsidian does.
	if st, _ := b.call("POST", api+"/notes", CreateRequest{Title: "Plan", Folder: "Projekt"}, nil); st != 201 {
		t.Fatal(st)
	}
	if res, body := b.raw("GET", api+"/files/Pasted%20image%201%202.png?protocol=1&from=Projekt/Plan.md", "", nil); res.StatusCode != 200 || body != "PNG2" {
		t.Fatalf("by name: %d %q", res.StatusCode, body)
	}
	if res, _ := b.raw("GET", api+"/files/saknas.png?protocol=1&from=Projekt/Plan.md", "", nil); res.StatusCode != 404 {
		t.Fatalf("missing: %d", res.StatusCode)
	}
}

func TestWebAPIIsolationAndCSRF(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	a, aTok := webUser(t, e, "anton@example.com")
	bb, bTok := webUser(t, e, "eva@example.com")
	if st, _ := a.call("POST", api+"/notes", CreateRequest{Title: "Secret"}, nil); st != 201 {
		t.Fatal(st)
	}
	// Eva has her own "main": Anton's note is not in it, by session or token.
	if st, _ := bb.call("GET", notePathURL("Inbox/Secret.md"), nil, nil); st != 404 {
		t.Fatalf("other user's note: %d", st)
	}
	var list NotesList
	if st, _ := bb.call("GET", api+"/notes", nil, &list); st != 200 || list.Count != 0 {
		t.Fatalf("other user's list: %+v", list)
	}
	res, _ := e.do(bTok, "GET", notePathURL("Inbox/Secret.md"), nil, "")
	if res.StatusCode != 404 {
		t.Fatalf("other user's token: %d", res.StatusCode)
	}
	if st, _ := bb.call("GET", "/v1/api/vaults/nope/tree", nil, nil); st != 404 {
		t.Fatalf("unknown vault: %d", st)
	}
	// The token API works without the CSRF header; the cookie does not.
	res, body := e.do(aTok, "POST", api+"/notes", nil, `{"title":"From a token"}`)
	if res.StatusCode != 201 {
		t.Fatalf("token create: %d %s", res.StatusCode, body)
	}
	var n Note
	_ = json.Unmarshal(body, &n)
	c := e.changes(aTok, 1, 10)
	if len(c.Entries) != 1 || c.Entries[0].DeviceID != "laptop" {
		t.Fatalf("token write device: %+v", c.Entries)
	}
	u, _ := url.Parse(e.ts.URL)
	req, _ := http.NewRequest("POST", e.ts.URL+api+"/notes", strings.NewReader(`{"title":"csrf"}`))
	req.Header.Set("Nota-Protocol", "1")
	for _, ck := range a.client.Jar.Cookies(u) {
		req.AddCookie(ck)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("cookie write without Nota-Client: %d", res.StatusCode)
	}
	// No credentials at all.
	res, _ = e.do("", "GET", api+"/tree", nil, "")
	if res.StatusCode != 401 {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
	// The protocol version is required here too.
	res, _ = e.do(aTok, "GET", api+"/tree", map[string]string{protocol.HeaderProtocol: ""}, "")
	if res.StatusCode != 426 {
		t.Fatalf("no protocol header: %d", res.StatusCode)
	}
	// Preflight for a save from the web origin.
	req, _ = http.NewRequest("OPTIONS", e.ts.URL+notePathURL("x.md"), nil)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "base-version,content-type,nota-client,nota-protocol")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 204 || !strings.Contains(res.Header.Get("Access-Control-Allow-Methods"), "PUT") ||
		!strings.Contains(res.Header.Get("Access-Control-Allow-Headers"), protocol.HeaderBaseVersion) {
		t.Fatalf("preflight: %d %v", res.StatusCode, res.Header)
	}
}

func TestWebAPIEventStream(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen, KeepAlive: 50 * time.Millisecond})
	b, tok := webUser(t, e, "anton@example.com")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Like EventSource: no custom headers, the protocol in the query.
	req, _ := http.NewRequestWithContext(ctx, "GET", e.ts.URL+api+"/events?protocol=1", nil)
	req.Header.Set("Origin", testOrigin)
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" || res.Header.Get("X-Accel-Buffering") != "no" ||
		res.Header.Get("Access-Control-Allow-Origin") != testOrigin {
		t.Fatalf("stream: %d %v", res.StatusCode, res.Header)
	}
	sc := bufio.NewScanner(res.Body)
	next := func() string {
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data:") {
				return l
			}
		}
		t.Fatalf("stream ended: %v", sc.Err())
		return ""
	}
	if got := next(); got != `data: {"cursor":0}` {
		t.Fatalf("first event %q", got)
	}
	// A sync write nudges the web app.
	e.put(tok, "a.md", 0, "a\n")
	if got := next(); got != `data: {"cursor":1}` {
		t.Fatalf("after sync write %q", got)
	}
	// A web write nudges too.
	b.call("POST", api+"/notes", CreateRequest{Title: "x"}, nil)
	if got := next(); got != `data: {"cursor":2}` {
		t.Fatalf("after web write %q", got)
	}
}

func TestWebAPIMoveRewritesLinks(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")
	if st, _, err := e.put(tok, "Projekt/Plan.md", 0, "# Plan\n"); st != 200 {
		t.Fatalf("put: %d %v", st, err)
	}
	if st, _, err := e.put(tok, "Länk.md", 0, "[[Projekt/Plan]] [p](Projekt/Plan.md)\n"); st != 200 {
		t.Fatalf("put: %d %v", st, err)
	}
	var mv MoveResponse
	if st, m := b.call("POST", api+"/move", MoveRequest{Path: "Projekt/Plan.md", To: "Projekt/Roadmap.md"}, &mv); st != 200 ||
		mv.Links.Count != 2 || len(mv.Links.Notes) != 1 || mv.Links.Notes[0].Path != "Länk.md" {
		t.Fatalf("move: %d %v %+v", st, m, mv)
	}
	var rf RenameFolderResponse
	if st, m := b.call("POST", api+"/folders/rename", RenameFolderRequest{Path: "Projekt", To: "Arbete"}, &rf); st != 200 || rf.Links.Count != 2 {
		t.Fatalf("rename folder: %d %v %+v", st, m, rf)
	}
	// The rewrite is a version of the web device, like any edit.
	res, body := e.do(tok, "GET", protocol.FilePath("main", "Länk.md"), nil, "")
	if res.StatusCode != 200 || string(body) != "[[Arbete/Roadmap]] [p](Arbete/Roadmap.md)\n" || res.Header.Get(protocol.HeaderDevice) != DeviceWeb {
		t.Fatalf("Länk.md: %d %q", res.StatusCode, body)
	}
}

func TestWebAPITrash(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, _ := webUser(t, e, "anton@example.com")
	for _, p := range []string{"Projekt/Plan.md", "Idé.md", "Flyttad.md"} {
		content := "# " + p + "\n"
		if st, m := b.callBase("PUT", notePathURL(p), 0, SaveRequest{Content: &content}, nil); st != 200 {
			t.Fatalf("create %s: %d %v", p, st, m)
		}
	}
	for _, p := range []string{"Projekt/Plan.md", "Idé.md"} {
		if st, m := b.call("DELETE", notePathURL(p), nil, nil); st != 200 {
			t.Fatalf("delete %s: %d %v", p, st, m)
		}
	}
	// A move deletes the old path too, but that's no delete of a note.
	if st, m := b.call("POST", api+"/move", MoveRequest{Path: "Flyttad.md", Folder: "Arkiv"}, nil); st != 200 {
		t.Fatalf("move: %d %v", st, m)
	}
	var tr struct{ Items []TrashItem }
	if st, m := b.call("GET", api+"/trash", nil, &tr); st != 200 || len(tr.Items) != 2 ||
		tr.Items[0].ID != "Idé.md" || tr.Items[1].Path != "Projekt/Plan.md" || tr.Items[0].Size != int64(len("# Idé.md\n")) {
		t.Fatalf("trash: %d %v %+v", st, m, tr)
	}
	var doc struct{ Content string }
	if st, m := b.call("GET", api+"/trash/"+protocol.EscapePath("Idé.md"), nil, &doc); st != 200 || doc.Content != "# Idé.md\n" {
		t.Fatalf("trash note: %d %v %+v", st, m, doc)
	}
	// Restore where it was, then into a folder when the path is taken.
	var out struct{ Path string }
	if st, m := b.call("POST", api+"/trash/restore", TrashRestoreRequest{ID: "Idé.md"}, &out); st != 200 || out.Path != "Idé.md" {
		t.Fatalf("restore: %d %v %+v", st, m, out)
	}
	var n Note
	if st, _ := b.call("GET", notePathURL("Idé.md"), nil, &n); st != 200 || n.Content != "# Idé.md\n" {
		t.Fatalf("restored note: %+v", n)
	}
	content := "# New plan\n"
	if st, m := b.callBase("PUT", notePathURL("Projekt/Plan.md"), 0, SaveRequest{Content: &content}, nil); st != 200 {
		t.Fatalf("recreate: %d %v", st, m)
	}
	// Plan.md is live again, so it is no longer in the trash.
	if st, m := b.call("POST", api+"/trash/restore", TrashRestoreRequest{ID: "Projekt/Plan.md"}, nil); st != 404 {
		t.Fatalf("restore live: %d %v", st, m)
	}
	if st, m := b.call("DELETE", notePathURL("Idé.md"), nil, nil); st != 200 {
		t.Fatalf("delete again: %d %v", st, m)
	}
	content = "# Other\n"
	if st, m := b.callBase("PUT", notePathURL("Arkiv/Idé.md"), 0, SaveRequest{Content: &content}, nil); st != 200 {
		t.Fatalf("create: %d %v", st, m)
	}
	if st, m := b.call("POST", api+"/trash/restore", TrashRestoreRequest{ID: "Idé.md", Folder: "Arkiv"}, nil); st != 409 || code(m) != protocol.CodeExists {
		t.Fatalf("restore over a note: %d %v", st, m)
	}
	if st, m := b.call("POST", api+"/trash/restore", TrashRestoreRequest{ID: "Idé.md", Folder: "Gammalt"}, &out); st != 200 || out.Path != "Gammalt/Idé.md" {
		t.Fatalf("restore into folder: %d %v %+v", st, m, out)
	}
	if st, _ := b.call("GET", api+"/trash", nil, &tr); st != 200 || len(tr.Items) != 0 {
		t.Fatalf("trash after: %+v", tr)
	}
}
