package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/banjo/nota/internal/protocol"
)

func vaultNames(t *testing.T, b *browser) []string {
	t.Helper()
	var l protocol.VaultList
	if st, m := b.call("GET", "/v1/api/vaults", nil, &l); st != 200 {
		t.Fatalf("list vaults: %d %v", st, m)
	}
	out := []string{}
	for _, v := range l.Vaults {
		out = append(out, v.Name)
	}
	return out
}

// syncPut writes a file through the sync protocol, as a device does.
func syncPut(t *testing.T, e *env, tok, vault, path, content string) protocol.Entry {
	t.Helper()
	res, body := e.do(tok, "PUT", protocol.FilePath(vault, path), map[string]string{protocol.HeaderBaseVersion: "0"}, content)
	if res.StatusCode != 200 {
		t.Fatalf("put %s/%s: %d %s", vault, path, res.StatusCode, body)
	}
	var en protocol.Entry
	_ = json.Unmarshal(body, &en)
	return en
}

func TestRenameVault(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")
	if st, _ := b.call("POST", api+"/notes", CreateRequest{Title: "Plan", Body: "x\n"}, nil); st != 201 {
		t.Fatal(st)
	}
	ctx := context.Background()
	u, _ := e.srv.Store().UserByEmail(ctx, "anton@example.com")
	id, _ := e.srv.Store().VaultID(ctx, u.ID, "main")

	var v protocol.VaultInfo
	if st, m := b.call("PATCH", "/v1/api/vaults/main", RenameRequest{Name: "Work"}, &v); st != 200 || v.Name != "Work" || v.Cursor != 1 {
		t.Fatalf("rename: %d %v %+v", st, m, v)
	}
	if got := vaultNames(t, b); len(got) != 1 || got[0] != "Work" {
		t.Fatalf("vaults after rename: %v", got)
	}
	// Same vault: id, notes and history stay.
	if nid, _ := e.srv.Store().VaultID(ctx, u.ID, "Work"); nid != id {
		t.Fatalf("id changed: %d -> %d", id, nid)
	}
	if st, _ := b.call("GET", "/v1/api/vaults/Work/notes/Inbox/Plan.md", nil, nil); st != 200 {
		t.Fatalf("note under the new name: %d", st)
	}
	// A device still configured with the old name keeps syncing with it,
	// and the web app sees its write under the new name.
	syncPut(t, e, tok, "main", "from-laptop.md", "hi\n")
	if st, _ := b.call("GET", "/v1/api/vaults/Work/notes/from-laptop.md", nil, nil); st != 200 {
		t.Fatalf("old-name write under the new name: %d", st)
	}
	if c := e.changes(tok, 0, 10); len(c.Entries) != 2 {
		t.Fatalf("changes by the old name: %+v", c)
	}

	// The old name is not free for another vault (devices may still use it).
	if st, m := b.call("POST", "/v1/api/vaults", map[string]string{"name": "main"}, nil); st != 409 || code(m) != protocol.CodeExists {
		t.Fatalf("create with an earlier name: %d %v", st, m)
	}
	if st, _ := b.call("POST", "/v1/api/vaults", map[string]string{"name": "Other"}, nil); st != 201 {
		t.Fatal(st)
	}
	if st, m := b.call("PATCH", "/v1/api/vaults/Other", RenameRequest{Name: "main"}, nil); st != 409 || code(m) != protocol.CodeExists {
		t.Fatalf("rename onto an earlier name: %d %v", st, m)
	}
	if st, m := b.call("PATCH", "/v1/api/vaults/Other", RenameRequest{Name: "Work"}, nil); st != 409 || code(m) != protocol.CodeExists {
		t.Fatalf("rename onto another vault: %d %v", st, m)
	}
	// ...but the vault itself may go back to it; then "Work" is the alias.
	if st, m := b.call("PATCH", "/v1/api/vaults/Work", RenameRequest{Name: "main"}, &v); st != 200 || v.Name != "main" {
		t.Fatalf("rename back: %d %v", st, m)
	}
	if st, _ := b.call("GET", "/v1/api/vaults/Work/tree", nil, nil); st != 200 {
		t.Fatalf("second alias: %d", st)
	}
	// Renaming to the current name is a no-op.
	if st, _ := b.call("PATCH", "/v1/api/vaults/main", RenameRequest{Name: "main"}, &v); st != 200 || v.Name != "main" {
		t.Fatalf("same name: %d %+v", st, v)
	}

	// Validation, 404, and an unknown vault by any name.
	for _, bad := range []string{"", "-x", "a b", strings.Repeat("a", 65), "ö"} {
		if st, m := b.call("PATCH", "/v1/api/vaults/main", RenameRequest{Name: bad}, nil); st != 400 || code(m) != protocol.CodeBadRequest {
			t.Fatalf("bad name %q: %d %v", bad, st, m)
		}
	}
	if st, m := b.call("PATCH", "/v1/api/vaults/nope", RenameRequest{Name: "x"}, nil); st != 404 || code(m) != protocol.CodeNotFound {
		t.Fatalf("unknown vault: %d %v", st, m)
	}
	// A device token can't rename: it needs a browser session.
	res, body := e.do(tok, "PATCH", "/v1/api/vaults/main", nil, `{"name":"x"}`)
	if res.StatusCode != 403 || !strings.Contains(string(body), CodeForbidden) {
		t.Fatalf("token rename: %d %s", res.StatusCode, body)
	}
}

func TestRenameVaultSSEFollowsAlias(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")
	if st, _ := b.call("PATCH", "/v1/api/vaults/main", RenameRequest{Name: "Work"}, nil); st != 200 {
		t.Fatal(st)
	}
	// A device that follows the old name gets the nudge for a write made
	// under the new one.
	req, _ := http.NewRequest("GET", e.ts.URL+protocol.EventsPath("main")+"?protocol=1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("events: %d", res.StatusCode)
	}
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var all string
		for {
			n, err := res.Body.Read(buf)
			all += string(buf[:n])
			if strings.Contains(all, `"cursor":1`) || err != nil {
				got <- all
				return
			}
		}
	}()
	syncPut(t, e, tok, "Work", "a.md", "a\n")
	if s := <-got; !strings.Contains(s, `"cursor":1`) {
		t.Fatalf("no change event by the old name: %q", s)
	}
}

func TestDeleteVault(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")
	ctx := context.Background()
	st := e.srv.Store()
	u, _ := st.UserByEmail(ctx, "anton@example.com")
	if s, _ := b.call("POST", "/v1/api/vaults", map[string]string{"name": "scratch"}, nil); s != 201 {
		t.Fatal(s)
	}
	const shared, only = "in both vaults\n", "only in scratch\n"
	syncPut(t, e, tok, "main", "keep.md", shared)
	syncPut(t, e, tok, "scratch", "copy.md", shared)
	syncPut(t, e, tok, "scratch", "gone.md", only)
	// Open the working copy, then rename so the delete goes through an alias.
	if s, _ := b.call("GET", "/v1/api/vaults/scratch/tree", nil, nil); s != 200 {
		t.Fatal(s)
	}
	if s, _ := b.call("PATCH", "/v1/api/vaults/scratch", RenameRequest{Name: "Scratch2"}, nil); s != 200 {
		t.Fatal(s)
	}
	id, _ := st.VaultID(ctx, u.ID, "Scratch2")
	dir := filepath.Join(e.srv.cfg.DataDir, "vaults", strconv.FormatInt(id, 10))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("no working copy: %v", err)
	}
	blobDir := filepath.Join(e.srv.cfg.DataDir, "blobs")

	// The typed confirmation must be the current name.
	for _, c := range []string{"", "scratch", "scratch2"} {
		if s, m := b.call("DELETE", "/v1/api/vaults/Scratch2", DeleteVaultRequest{Confirm: c}, nil); s != 400 || code(m) != protocol.CodeBadRequest {
			t.Fatalf("confirm %q: %d %v", c, s, m)
		}
	}
	// A token can't delete; neither can another user (404, like a missing vault).
	if res, body := e.do(tok, "DELETE", "/v1/api/vaults/Scratch2", nil, `{"confirm":"Scratch2"}`); res.StatusCode != 403 {
		t.Fatalf("token delete: %d %s", res.StatusCode, body)
	}
	eva, _ := webUser(t, e, "eva@example.com")
	if s, m := eva.call("DELETE", "/v1/api/vaults/Scratch2", DeleteVaultRequest{Confirm: "Scratch2"}, nil); s != 404 || code(m) != protocol.CodeNotFound {
		t.Fatalf("other user: %d %v", s, m)
	}

	if s, m := b.call("DELETE", "/v1/api/vaults/scratch", DeleteVaultRequest{Confirm: "Scratch2"}, nil); s != 204 {
		t.Fatalf("delete: %d %v", s, m)
	}
	if got := vaultNames(t, b); len(got) != 1 || got[0] != "main" {
		t.Fatalf("vaults after delete: %v", got)
	}
	for _, n := range []string{"Scratch2", "scratch"} {
		if s, _ := b.call("GET", "/v1/api/vaults/"+n+"/tree", nil, nil); s != 404 {
			t.Fatalf("%s after delete: %d", n, s)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("working copy left: %v", err)
	}
	// Content only the deleted vault had is gone (row and file); content
	// another vault has stays.
	if _, err := os.Stat(BlobPath(blobDir, protocol.Hash([]byte(only)))); !os.IsNotExist(err) {
		t.Fatalf("unshared blob left: %v", err)
	}
	if n, _ := countRows(ctx, st.db, `SELECT count(*) FROM blobs WHERE hash = ?`, protocol.Hash([]byte(only))); n != 0 {
		t.Fatalf("unshared blob row left: %d", n)
	}
	if _, err := os.Stat(BlobPath(blobDir, protocol.Hash([]byte(shared)))); err != nil {
		t.Fatalf("shared blob gone: %v", err)
	}
	if res, body := e.do(tok, "GET", protocol.FilePath("main", "keep.md"), nil, ""); res.StatusCode != 200 || string(body) != shared {
		t.Fatalf("main after delete: %d %q", res.StatusCode, body)
	}
	// The names are free again, and a new vault starts empty.
	if s, _ := b.call("POST", "/v1/api/vaults", map[string]string{"name": "scratch"}, nil); s != 201 {
		t.Fatalf("reuse the earlier name: %d", s)
	}
	var tr Tree
	if s, _ := b.call("GET", "/v1/api/vaults/scratch/tree", nil, &tr); s != 200 || len(tr.Notes) != 0 {
		t.Fatalf("new vault with the old name: %d %+v", s, tr)
	}
	if s, _ := b.call("DELETE", "/v1/api/vaults/nope", DeleteVaultRequest{Confirm: "nope"}, nil); s != 404 {
		t.Fatalf("unknown vault: %d", s)
	}
}

func TestVaultRoutesNeedCSRFHeader(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, _ := webUser(t, e, "anton@example.com")
	send := func(method, path, body string, client bool) int {
		req, _ := http.NewRequest(method, e.ts.URL+path, strings.NewReader(body))
		req.Header.Set("Nota-Protocol", "1")
		req.Header.Set("Origin", testOrigin)
		if client {
			req.Header.Set(HeaderClient, "web")
		}
		u, _ := url.Parse(e.ts.URL)
		for _, ck := range b.client.Jar.Cookies(u) {
			req.AddCookie(ck)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if s := send("PATCH", "/v1/api/vaults/main", `{"name":"x"}`, false); s != 403 {
		t.Fatalf("rename without Nota-Client: %d", s)
	}
	if s := send("DELETE", "/v1/api/vaults/main", `{"confirm":"main"}`, false); s != 403 {
		t.Fatalf("delete without Nota-Client: %d", s)
	}
	if got := vaultNames(t, b); len(got) != 1 || got[0] != "main" {
		t.Fatalf("vaults changed: %v", got)
	}
	if s := send("PATCH", "/v1/api/vaults/main", `{"name":"x"}`, true); s != 200 {
		t.Fatalf("rename with the header: %d", s)
	}
}
