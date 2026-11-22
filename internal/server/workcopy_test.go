package server

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/banjo/nota/internal/protocol"
)

func (e *env) ws(name string, userID int64) *workspace {
	e.t.Helper()
	ws, err := e.srv.workspace(context.Background(), userID, name)
	if err != nil {
		e.t.Fatal(err)
	}
	return ws
}

func (e *env) userID(email string) int64 {
	e.t.Helper()
	u, err := e.srv.Store().UserByEmail(context.Background(), email)
	if err != nil {
		e.t.Fatal(err)
	}
	return u.ID
}

func readWork(t *testing.T, ws *workspace, p string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ws.dir, filepath.FromSlash(p)))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}

func TestWorkingCopyFollowsSync(t *testing.T) {
	dir := t.TempDir()
	e := newEnv(t, Config{DataDir: dir})
	tok := e.user("anton@example.com")
	uid := e.userID("anton@example.com")

	if st, _, perr := e.put(tok, "Projekt/Plan.md", 0, "---\ntags: [plan]\n---\n# Plan\n\nSee [[Möte]].\n"); st != 200 {
		t.Fatalf("put: %d %v", st, perr)
	}
	if st, _, _ := e.put(tok, "Möte.md", 0, "# Möte\n"); st != 200 {
		t.Fatal(st)
	}
	if st, _, _ := e.put(tok, "attachments/pic.png", 0, "\x89PNG"); st != 200 {
		t.Fatal(st)
	}
	ws := e.ws("main", uid)
	if got, ok := readWork(t, ws, "Projekt/Plan.md"); !ok || !strings.Contains(got, "# Plan") {
		t.Fatalf("plan on disk: %q %v", got, ok)
	}
	if _, ok := readWork(t, ws, "attachments/pic.png"); !ok {
		t.Fatal("attachment not materialized")
	}
	ws.mu.Lock()
	ent, err := ws.ix.Get("Projekt/Plan.md")
	bl, _ := ws.ix.Backlinks("Möte.md")
	ws.mu.Unlock()
	if err != nil || ent.Title != "Plan" || len(ent.Tags) != 1 || len(bl) != 1 {
		t.Fatalf("index: %+v %v backlinks %v", ent, err, bl)
	}

	// Moves and deletes follow, and leave no trash behind.
	res, body := e.do(tok, "POST", protocol.MovesPath("main"), nil, `{"from":"Möte.md","to":"Arkiv/Möte.md","base_version":2}`)
	if res.StatusCode != 200 {
		t.Fatalf("move: %d %s", res.StatusCode, body)
	}
	if _, ok := readWork(t, ws, "Möte.md"); ok {
		t.Fatal("moved file still at the old path")
	}
	if _, ok := readWork(t, ws, "Arkiv/Möte.md"); !ok {
		t.Fatal("moved file missing")
	}
	res, body = e.do(tok, "DELETE", protocol.FilePath("main", "attachments/pic.png"), map[string]string{protocol.HeaderBaseVersion: "3"}, "")
	if res.StatusCode != 200 {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	if _, ok := readWork(t, ws, "attachments/pic.png"); ok {
		t.Fatal("deleted file still on disk")
	}
	if _, err := os.Stat(filepath.Join(ws.dir, ".trash")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(ws.dir, ".trash"))
		for _, en := range entries {
			if !en.IsDir() {
				t.Fatalf("trash kept %s", en.Name())
			}
		}
	}
	ws.mu.Lock()
	_, gone := ws.ix.Get("Möte.md")
	moved, err2 := ws.ix.Get("Arkiv/Möte.md")
	ws.mu.Unlock()
	if gone == nil || err2 != nil || moved.Title != "Möte" {
		t.Fatalf("index after move: old err %v, new %+v %v", gone, moved, err2)
	}

	// Nota's own state from a (misbehaving) client never reaches the
	// working copy; shared config does.
	if st, _, _ := e.put(tok, ".nota/index.db", 0, "garbage"); st != 200 {
		t.Fatal(st)
	}
	if st, _, _ := e.put(tok, ".nota/views/drafts.yaml", 0, "name: Drafts\nwhere: { status: draft }\n"); st != 200 {
		t.Fatal(st)
	}
	if got, _ := readWork(t, ws, ".nota/index.db"); got == "garbage" {
		t.Fatal("client overwrote the server's index")
	}
	if _, ok := readWork(t, ws, ".nota/views/drafts.yaml"); !ok {
		t.Fatal("view not materialized")
	}

	// A clean restart trusts the marker; a missing working copy is
	// rebuilt from the log.
	e.srv.closeWorkspaces()
	if _, err := os.Stat(filepath.Join(ws.dir, filepath.FromSlash(stateFile))); err != nil {
		t.Fatalf("no clean-shutdown marker: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "vaults")); err != nil {
		t.Fatal(err)
	}
	ws = e.ws("main", uid)
	if got, ok := readWork(t, ws, "Arkiv/Möte.md"); !ok || got != "# Möte\n" {
		t.Fatalf("rebuilt: %q %v", got, ok)
	}
	if _, ok := readWork(t, ws, "attachments/pic.png"); ok {
		t.Fatal("rebuilt a deleted file")
	}
	// A crash (no marker) with a stale file on disk: reconcile fixes it.
	e.srv.closeWorkspaces()
	_ = os.Remove(filepath.Join(ws.dir, filepath.FromSlash(stateFile)))
	_ = os.WriteFile(filepath.Join(ws.dir, "stray.md"), []byte("not in the log"), 0o644)
	_ = os.WriteFile(filepath.Join(ws.dir, "Projekt", "Plan.md"), []byte("edited behind the server's back"), 0o644)
	ws = e.ws("main", uid)
	if _, ok := readWork(t, ws, "stray.md"); ok {
		t.Fatal("stray file kept")
	}
	if got, _ := readWork(t, ws, "Projekt/Plan.md"); !strings.Contains(got, "# Plan") {
		t.Fatalf("plan not restored: %q", got)
	}
}

func TestEditorWritesGoToTheLog(t *testing.T) {
	e := newEnv(t, Config{})
	tok := e.user("anton@example.com")
	ws := e.ws("main", e.userID("anton@example.com"))
	ws.mu.Lock()
	ed, err := ws.editor(DeviceWeb)
	if err != nil {
		t.Fatal(err)
	}
	n, err := ed.Create("Inbox", []byte("# From the web\n"))
	if err == nil {
		_, err = ed.Move(n.Path, "Notes")
	}
	if err == nil {
		_, _, err = ed.Save("Notes/From the web.md", []byte("# From the web\n\nmore\n"))
	}
	ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	c := e.changes(tok, 0, 100)
	var got []string
	for _, en := range c.Entries {
		got = append(got, en.Path+" "+en.DeviceID+" "+map[bool]string{true: "deleted", false: "live"}[en.Deleted])
	}
	want := []string{"Inbox/From the web.md web deleted", "Notes/From the web.md web live"}
	if strings.Join(got, "|") != strings.Join(want, "|") || c.Cursor != 4 {
		t.Fatalf("changes %v cursor %d, want %v", got, c.Cursor, want)
	}
	res, body := e.do(tok, "GET", protocol.FilePath("main", "Notes/From the web.md"), nil, "")
	if res.StatusCode != 200 || string(body) != "# From the web\n\nmore\n" {
		t.Fatalf("get: %d %q", res.StatusCode, body)
	}
	res, body = e.do(tok, "GET", protocol.HistoryPath("main", "Notes/From the web.md"), nil, "")
	if res.StatusCode != 200 || strings.Count(string(body), `"version"`) != 4 {
		t.Fatalf("history: %d %s", res.StatusCode, body)
	}
}

const fileHook = `[[hook]]
name = "file"
event = "inbox.added"
mode = "transform"
run = "sh hooks/file.sh"
`

// The hook files every inbox note into Filed/ with a tag and logs each
// run outside the vault.
const fileScript = `echo "$NOTA_EVENT $NOTA_NOTE" >> "$HOOK_LOG"
printf '{"tags":{"add":["filed"]},"folder":"Filed"}'
`

func TestServerHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	logf := filepath.Join(t.TempDir(), "runs")
	t.Setenv("HOOK_LOG", logf)
	e := newEnv(t, Config{Hooks: true})
	tok := e.user("anton@example.com")
	for p, c := range map[string]string{"hooks/file.sh": fileScript, ".nota/hooks.toml": fileHook} {
		if st, _, perr := e.put(tok, p, 0, c); st != 200 {
			t.Fatalf("put %s: %d %v", p, st, perr)
		}
	}
	// A note captured on the phone lands in the inbox.
	st, ent, perr := e.put(tok, "Inbox/Idea.md", 0, "---\ncreated: 2026-11-22T10:00:00Z\n---\n# Idea\n")
	if st != 200 || ent.Version != 3 {
		t.Fatalf("put note: %d %+v %v", st, ent, perr)
	}
	c := e.changes(tok, 3, 100)
	var got []string
	for _, en := range c.Entries {
		got = append(got, en.Path+" "+en.DeviceID)
	}
	if strings.Join(got, "|") != "Inbox/Idea.md server-hook|Filed/Idea.md server-hook" {
		t.Fatalf("hook versions: %v", got)
	}
	res, body := e.do(tok, "GET", protocol.FilePath("main", "Filed/Idea.md"), nil, "")
	if res.StatusCode != 200 || !strings.Contains(string(body), "tags: [filed]") {
		t.Fatalf("filed note: %d %q", res.StatusCode, body)
	}
	runs, _ := os.ReadFile(logf)
	if string(runs) != "inbox.added Inbox/Idea.md\n" {
		t.Fatalf("hook runs: %q", runs)
	}
	// The hook's own changes do not trigger it again, and notes outside
	// the inbox are left alone.
	if st, _, _ := e.put(tok, "Elsewhere.md", 0, "# Elsewhere\n"); st != 200 {
		t.Fatal(st)
	}
	if runs, _ := os.ReadFile(logf); strings.Count(string(runs), "\n") != 1 {
		t.Fatalf("hook ran again: %q", runs)
	}
}

func TestServerHooksOffByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	logf := filepath.Join(t.TempDir(), "runs")
	t.Setenv("HOOK_LOG", logf)
	e := newEnv(t, Config{})
	tok := e.user("anton@example.com")
	e.put(tok, "hooks/file.sh", 0, fileScript)
	e.put(tok, ".nota/hooks.toml", 0, fileHook)
	e.put(tok, "Inbox/Idea.md", 0, "# Idea\n")
	if c := e.changes(tok, 3, 100); len(c.Entries) != 0 {
		t.Fatalf("hooks ran while off: %+v", c.Entries)
	}
	if _, err := os.Stat(logf); err == nil {
		t.Fatal("hook script ran")
	}
}
