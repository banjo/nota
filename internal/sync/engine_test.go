package sync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/server"
	"github.com/banjo/nota/internal/vault"
)

var quiet = slog.New(slog.DiscardHandler)

type harness struct {
	t     *testing.T
	srv   *server.Server
	ts    *httptest.Server
	user  int64
	vault string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	srv, err := server.New(ctx, server.Config{DataDir: t.TempDir(), Logger: quiet, MaxFileSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
		srv.Close()
	})
	u, err := srv.Store().AddUser(ctx, "anton@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Store().CreateVault(ctx, u.ID, "main"); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, srv: srv, ts: ts, user: u.ID, vault: "main"}
}

var testNow = time.Date(2026, 11, 22, 10, 42, 0, 0, time.UTC)

// node is one machine: a vault on disk with its own engine and token.
type node struct {
	t    *testing.T
	name string
	dir  string
	v    *vault.Vault
	e    *Engine
	bus  *events.Bus
	rt   *flakyTransport
}

func (h *harness) node(name string) *node {
	h.t.Helper()
	tok, _, err := h.srv.Store().CreateToken(context.Background(), h.user, name)
	if err != nil {
		h.t.Fatal(err)
	}
	dir := filepath.Join(h.t.TempDir(), name)
	bus := events.New()
	v, err := vault.Open(dir, vault.Options{Create: true, Bus: bus})
	if err != nil {
		h.t.Fatal(err)
	}
	rt := &flakyTransport{rt: http.DefaultTransport}
	c := &Client{BaseURL: h.ts.URL, Vault: h.vault, Token: tok, HTTP: &http.Client{Transport: rt}}
	e, err := Open(v, c, Options{Device: name, Now: func() time.Time { return testNow }, Logger: quiet})
	if err != nil {
		h.t.Fatal(err)
	}
	n := &node{t: h.t, name: name, dir: dir, v: v, e: e, bus: bus, rt: rt}
	h.t.Cleanup(func() { e.Close(); bus.Close() })
	return n
}

// flakyTransport can lose responses: the server applies the request but
// the client sees a network error, like a connection dropped mid-way.
type flakyTransport struct {
	rt       http.RoundTripper
	loseNext atomic.Int32 // number of PUT responses to lose
	down     atomic.Bool  // refuse every request
}

func (f *flakyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.down.Load() {
		return nil, errors.New("network is down")
	}
	res, err := f.rt.RoundTrip(r)
	if err == nil && r.Method == "PUT" && f.loseNext.Load() > 0 {
		f.loseNext.Add(-1)
		res.Body.Close()
		return nil, errors.New("connection reset by peer")
	}
	return res, err
}

func (n *node) write(p, content string) {
	n.t.Helper()
	full := filepath.Join(n.dir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		n.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		n.t.Fatal(err)
	}
}

func (n *node) read(p string) string {
	n.t.Helper()
	data, err := n.v.ReadFile(p)
	if err != nil {
		n.t.Fatalf("%s: read %s: %v", n.name, p, err)
	}
	return string(data)
}

func (n *node) rm(p string) {
	n.t.Helper()
	if err := os.Remove(filepath.Join(n.dir, filepath.FromSlash(p))); err != nil {
		n.t.Fatal(err)
	}
}

func (n *node) sync() *Result {
	n.t.Helper()
	res, err := n.e.Sync(context.Background())
	if err != nil {
		n.t.Fatalf("%s: sync: %v", n.name, err)
	}
	return res
}

// files returns every synced file (ignored ones excluded) and its content.
func (n *node) files() map[string]string {
	n.t.Helper()
	out := map[string]string{}
	err := n.v.WalkFiles(func(e vault.Entry) error {
		data, err := n.v.ReadEntry(e)
		out[e.Path] = string(data)
		return err
	})
	if err != nil {
		n.t.Fatal(err)
	}
	return out
}

func sameFiles(t *testing.T, a, b *node) {
	t.Helper()
	fa, fb := a.files(), b.files()
	for p, ca := range fa {
		if cb, ok := fb[p]; !ok {
			t.Errorf("%s has %s, %s does not", a.name, p, b.name)
		} else if ca != cb {
			t.Errorf("%s differs:\n%s: %q\n%s: %q", p, a.name, ca, b.name, cb)
		}
	}
	for p := range fb {
		if _, ok := fa[p]; !ok {
			t.Errorf("%s has %s, %s does not", b.name, p, a.name)
		}
	}
}

func TestPropagation(t *testing.T) {
	h := newHarness(t)
	a, b := h.node("a"), h.node("b")

	a.write("Inbox/Idé.md", "# Idé\n\nfirst\n")
	a.write("attachments/bild.png", "\x89PNG...")
	a.write(".nota/private.txt", "never synced")
	a.write(".obsidian/app.json", "{}")
	res := a.sync()
	if !slices.Equal(res.Pushed, []string{"Inbox/Idé.md", "attachments/bild.png"}) {
		t.Fatalf("pushed %v", res.Pushed)
	}
	res = b.sync()
	if !slices.Equal(res.Pulled, []string{"Inbox/Idé.md", "attachments/bild.png"}) || res.Cursor != 2 {
		t.Fatalf("pulled %+v", res)
	}
	if b.read("Inbox/Idé.md") != "# Idé\n\nfirst\n" || b.v.FileExists(".obsidian/app.json") {
		t.Fatal("content")
	}

	// Edit on B, delete on A's side afterwards.
	b.write("Inbox/Idé.md", "# Idé\n\nsecond\n")
	if res := b.sync(); !slices.Equal(res.Pushed, []string{"Inbox/Idé.md"}) {
		t.Fatalf("b pushed %+v", res)
	}
	if res := a.sync(); !slices.Equal(res.Pulled, []string{"Inbox/Idé.md"}) || a.read("Inbox/Idé.md") != "# Idé\n\nsecond\n" {
		t.Fatalf("a pulled %+v", res)
	}
	a.rm("attachments/bild.png")
	if res := a.sync(); !slices.Equal(res.Deleted, []string{"attachments/bild.png"}) {
		t.Fatalf("a deleted %+v", res)
	}
	if res := b.sync(); !slices.Equal(res.Removed, []string{"attachments/bild.png"}) {
		t.Fatalf("b removed %+v", res)
	}
	// Remote deletes go to the trash, never away.
	if data, err := os.ReadFile(filepath.Join(b.dir, ".trash/attachments/bild.png")); err != nil || string(data) != "\x89PNG..." {
		t.Fatalf("trash: %q %v", data, err)
	}
	sameFiles(t, a, b)

	// Recreating a deleted path works.
	b.write("attachments/bild.png", "new")
	b.sync()
	a.sync()
	if a.read("attachments/bild.png") != "new" {
		t.Fatal("recreate")
	}
	sameFiles(t, a, b)
}

func TestIdempotent(t *testing.T) {
	h := newHarness(t)
	a, b := h.node("a"), h.node("b")
	a.write("a.md", "a\n")
	a.write("sub/b.md", "b\n")
	a.sync()
	b.sync()
	for _, n := range []*node{a, b, a, b} {
		if res := n.sync(); res.Changed() || len(res.Errors) > 0 || res.Cursor != 2 {
			t.Fatalf("%s: second pass did something: %+v", n.name, res)
		}
	}
}

func TestMergeDifferentLines(t *testing.T) {
	h := newHarness(t)
	a, b := h.node("a"), h.node("b")
	base := "# Plan\n\n- one\n- two\n- three\n- four\n"
	a.write("Plan.md", base)
	a.sync()
	b.sync()

	a.write("Plan.md", "# Plan\n\n- ONE (a)\n- two\n- three\n- four\n")
	b.write("Plan.md", "# Plan\n\n- one\n- two\n- three\n- FOUR (b)\n- five (b)\n")
	a.sync()
	res := b.sync()
	want := "# Plan\n\n- ONE (a)\n- two\n- three\n- FOUR (b)\n- five (b)\n"
	if !slices.Equal(res.Merged, []string{"Plan.md"}) || len(res.Conflicts) != 0 || b.read("Plan.md") != want {
		t.Fatalf("merge: %+v\n%q", res, b.read("Plan.md"))
	}
	if res := a.sync(); !slices.Equal(res.Pulled, []string{"Plan.md"}) || a.read("Plan.md") != want {
		t.Fatalf("a: %+v", res)
	}
	sameFiles(t, a, b)
}

func TestConflictCopy(t *testing.T) {
	h := newHarness(t)
	a, b := h.node("a"), h.node("laptop")
	a.write("Möte.md", "# Möte\n\nTid: 10:00\n")
	a.sync()
	b.sync()

	a.write("Möte.md", "# Möte\n\nTid: 11:00\n")
	b.write("Möte.md", "# Möte\n\nTid: 14:30\n")
	a.sync()
	res := b.sync()
	cp := "Möte (conflict 2026-11-22 1042 laptop).md"
	if len(res.Conflicts) != 1 || res.Conflicts[0] != (Conflict{Path: "Möte.md", Copy: cp}) || len(res.Pushed) != 0 {
		t.Fatalf("conflict: %+v", res)
	}
	// The original took the remote text, the copy keeps the local one.
	if b.read("Möte.md") != "# Möte\n\nTid: 11:00\n" || b.read(cp) != "# Möte\n\nTid: 14:30\n" {
		t.Fatalf("texts: %q / %q", b.read("Möte.md"), b.read(cp))
	}
	if res := a.sync(); !slices.Equal(res.Pulled, []string{cp}) {
		t.Fatalf("a: %+v", res)
	}
	sameFiles(t, a, b)

	// Attachments never merge: both changed means a conflict copy.
	a.write("bild.png", "v1")
	a.sync()
	b.sync()
	a.write("bild.png", "v2-a")
	b.write("bild.png", "v2-b")
	a.sync()
	res = b.sync()
	if len(res.Conflicts) != 1 || res.Conflicts[0].Copy != "bild (conflict 2026-11-22 1042 laptop).png" ||
		b.read(res.Conflicts[0].Copy) != "v2-b" || b.read("bild.png") != "v2-a" {
		t.Fatalf("blob conflict: %+v", res)
	}
	// A second conflict in the same minute gets a free name.
	a.sync()
	a.write("Möte.md", "x\n")
	b.write("Möte.md", "y\n")
	a.sync()
	res = b.sync()
	if len(res.Conflicts) != 1 || res.Conflicts[0].Copy != "Möte (conflict 2026-11-22 1042 laptop) 2.md" {
		t.Fatalf("second conflict: %+v", res)
	}
	a.sync()
	sameFiles(t, a, b)
}

func TestDeleteVsEdit(t *testing.T) {
	for _, order := range []string{"edit first", "delete first"} {
		t.Run(order, func(t *testing.T) {
			h := newHarness(t)
			a, b := h.node("a"), h.node("b")
			a.write("Note.md", "v1\n")
			a.sync()
			b.sync()
			a.rm("Note.md")
			b.write("Note.md", "v2 from b\n")
			if order == "edit first" {
				b.sync()
				res := a.sync() // the delete meets the edit: the edit wins
				if !slices.Equal(res.Restored, []string{"Note.md"}) || a.read("Note.md") != "v2 from b\n" {
					t.Fatalf("a: %+v", res)
				}
			} else {
				a.sync()
				res := b.sync() // the edit meets the tombstone: the edit wins
				if !slices.Equal(res.Restored, []string{"Note.md"}) {
					t.Fatalf("b: %+v", res)
				}
				if res := a.sync(); !slices.Equal(res.Pulled, []string{"Note.md"}) || a.read("Note.md") != "v2 from b\n" {
					t.Fatalf("a: %+v", res)
				}
			}
			b.sync()
			sameFiles(t, a, b)
		})
	}
}

func TestRename(t *testing.T) {
	h := newHarness(t)
	a, b := h.node("a"), h.node("b")
	a.write("Inbox/Plan.md", "# Plan\n")
	a.sync()
	b.sync()

	if err := os.MkdirAll(filepath.Join(a.dir, "Projekt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(a.dir, "Inbox/Plan.md"), filepath.Join(a.dir, "Projekt/Plan.md")); err != nil {
		t.Fatal(err)
	}
	res := a.sync()
	if !slices.Equal(res.Moved, []Move{{From: "Inbox/Plan.md", To: "Projekt/Plan.md"}}) {
		t.Fatalf("a: %+v", res)
	}
	var moved []events.Event
	b.bus.Subscribe(func(e events.Event) { moved = append(moved, e) }, events.NoteMoved)
	res = b.sync()
	if !slices.Equal(res.Renamed, []Move{{From: "Inbox/Plan.md", To: "Projekt/Plan.md"}}) || len(res.Removed) != 0 {
		t.Fatalf("b: %+v", res)
	}
	if len(moved) != 1 || moved[0].Origin != events.OriginSync || b.v.FileExists("Inbox/Plan.md") {
		t.Fatalf("b events %+v", moved)
	}
	// The history follows the move.
	hist, err := b.e.Client().History(context.Background(), "Projekt/Plan.md")
	if err != nil || len(hist.Versions) != 3 || hist.Versions[2].Path != "Inbox/Plan.md" {
		t.Fatalf("history %+v %v", hist, err)
	}
	sameFiles(t, a, b)
}

func TestUnicodeNames(t *testing.T) {
	h := newHarness(t)
	mac, linux := h.node("mac"), h.node("linux")
	nfd := "Mo\u0308ten/Bo\u0308cker o\u0308ver a\u030arsskiftet.md" // as macOS writes it
	nfc := "Möten/Böcker över årsskiftet.md"
	mac.write(nfd, "# Böcker\n\nett\n")
	if res := mac.sync(); !slices.Equal(res.Pushed, []string{nfc}) {
		t.Fatalf("mac pushed %+v", res)
	}
	if res := linux.sync(); !slices.Equal(res.Pulled, []string{nfc}) || linux.read(nfc) != "# Böcker\n\nett\n" {
		t.Fatalf("linux pulled %+v", res)
	}
	if _, err := os.Stat(filepath.Join(linux.dir, nfc)); err != nil {
		t.Fatalf("linux stores NFC: %v", err)
	}
	// An edit from Linux lands in the NFD file on the Mac, not a twin.
	linux.write(nfc, "# Böcker\n\nett\ntvå\n")
	linux.sync()
	if res := mac.sync(); !slices.Equal(res.Pulled, []string{nfc}) {
		t.Fatalf("mac pulled %+v", res)
	}
	var names []string
	filepath.WalkDir(mac.dir, func(p string, d fs.DirEntry, _ error) error {
		if d.IsDir() && d.Name() == ".nota" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(mac.dir, p)
			names = append(names, rel)
		}
		return nil
	})
	if !slices.Equal(names, []string{filepath.FromSlash(nfd)}) {
		t.Fatalf("mac disk: %q", names)
	}
	if mac.read(nfc) != "# Böcker\n\nett\ntvå\n" {
		t.Fatal("mac content")
	}
	for _, n := range []*node{mac, linux} {
		if res := n.sync(); res.Changed() {
			t.Fatalf("%s not idle: %+v", n.name, res)
		}
	}
}

func TestCaseCollision(t *testing.T) {
	h := newHarness(t)
	a, b := h.node("a"), h.node("b")
	a.write("Idé.md", "a's\n")
	a.sync()

	// B (case-sensitive disk) already has IDÉ.md: the server refuses it,
	// and B cannot write Idé.md next to it. Neither is lost.
	b.write("IDÉ.md", "b's\n")
	res := b.sync()
	if len(res.Errors) != 2 || res.Errors[0].Code != protocol.CodeCaseCollision || res.Errors[1].Code != protocol.CodeCaseCollision {
		t.Fatalf("b: %+v", res)
	}
	if res.Cursor != 0 {
		t.Fatalf("cursor moved past the unapplied entry: %d", res.Cursor)
	}
	// The user renames their file; now both sync.
	if err := os.Rename(filepath.Join(b.dir, "IDÉ.md"), filepath.Join(b.dir, "Idé (b).md")); err != nil {
		t.Fatal(err)
	}
	res = b.sync()
	if len(res.Errors) != 0 || !slices.Equal(res.Pushed, []string{"Idé (b).md"}) || !slices.Equal(res.Pulled, []string{"Idé.md"}) {
		t.Fatalf("b after rename: %+v", res)
	}
	a.sync()
	sameFiles(t, a, b)
}

func TestOffline(t *testing.T) {
	h := newHarness(t)
	a, b := h.node("a"), h.node("b")
	a.write("a.md", "1\n")
	a.sync()
	b.sync()

	a.rt.down.Store(true)
	a.write("a.md", "1\n2\n")
	a.write("new.md", "offline\n")
	for range 3 {
		if _, err := a.e.Sync(context.Background()); err == nil {
			t.Fatal("sync while offline should fail")
		}
	}
	b.write("b.md", "from b\n")
	b.sync()
	a.rt.down.Store(false)
	res := a.sync()
	if !slices.Equal(res.Pushed, []string{"a.md", "new.md"}) || !slices.Equal(res.Pulled, []string{"b.md"}) {
		t.Fatalf("back online: %+v", res)
	}
	b.sync()
	sameFiles(t, a, b)
}

func TestCrashSafety(t *testing.T) {
	h := newHarness(t)
	a, b := h.node("a"), h.node("b")
	a.write("1.md", "one\n")
	a.write("2.md", "two\n")
	a.write("3.md", "three\n")

	// The second upload fails before it reaches the server.
	calls := 0
	a.e.opts.beforePush = func(string) error {
		if calls++; calls == 2 {
			return errors.New("killed")
		}
		return nil
	}
	if _, err := a.e.Sync(context.Background()); err == nil {
		t.Fatal("want the injected failure")
	}
	rows, _ := a.e.st.all()
	if len(rows) != 1 || rows["1.md"].Version != 1 || a.e.st.cursor() != 0 {
		t.Fatalf("state advanced past the failure: %+v cursor %d", rows, a.e.st.cursor())
	}
	a.e.opts.beforePush = nil

	// The server applies an upload but the response is lost.
	a.rt.loseNext.Store(1)
	if _, err := a.e.Sync(context.Background()); err == nil {
		t.Fatal("want the lost response")
	}
	if rows, _ := a.e.st.all(); len(rows) != 1 {
		t.Fatalf("state advanced without a response: %+v", rows)
	}
	// The next pass finds the server already has it, and adopts it.
	res := a.sync()
	if len(res.Conflicts) != 0 || len(res.Errors) != 0 {
		t.Fatalf("recovery: %+v", res)
	}
	b.sync()
	sameFiles(t, a, b)
	if res := a.sync(); res.Changed() {
		t.Fatalf("not idle: %+v", res)
	}
	vs, _ := h.srv.Store().Vaults(context.Background(), h.user)
	if vs[0].Cursor != 3 {
		t.Fatalf("server has %d versions, want 3 (no duplicates)", vs[0].Cursor)
	}
}

func TestLock(t *testing.T) {
	h := newHarness(t)
	a := h.node("a")
	c := *a.e.c
	if _, err := Open(a.v, &c, Options{Logger: quiet}); !errors.Is(err, ErrLocked) {
		t.Fatalf("second engine: %v", err)
	}
	var le *LockedError
	_, err := Open(a.v, &c, Options{Logger: quiet})
	if !errors.As(err, &le) || le.Holder.PID != os.Getpid() || !strings.Contains(err.Error(), "already being synced") {
		t.Fatalf("lock error %v", err)
	}
	a.e.Close()
	e2, err := Open(a.v, &c, Options{Logger: quiet})
	if err != nil {
		t.Fatalf("after close: %v", err)
	}
	e2.Close()

	// A lock left by a dead process is taken over; so is a very old one.
	lockPath := filepath.Join(a.v.MetaDir(), LockFile)
	host, _ := os.Hostname()
	os.WriteFile(lockPath, []byte(fmt.Sprintf(`{"pid":999999999,"host":%q,"started":"2026-01-01T00:00:00Z"}`, host)), 0o644)
	e3, err := Open(a.v, &c, Options{Logger: quiet})
	if err != nil {
		t.Fatalf("dead holder: %v", err)
	}
	e3.Close()
	os.WriteFile(lockPath, []byte(`{"pid":1,"host":"elsewhere","started":"2026-01-01T00:00:00Z"}`), 0o644)
	if _, err := Open(a.v, &c, Options{Logger: quiet}); !errors.Is(err, ErrLocked) {
		t.Fatalf("fresh foreign lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(lockPath, old, old)
	e4, err := Open(a.v, &c, Options{Logger: quiet})
	if err != nil {
		t.Fatalf("stale lock: %v", err)
	}
	e4.Close()
}

func TestSyncCompletedEvent(t *testing.T) {
	h := newHarness(t)
	a := h.node("a")
	var got []events.Event
	a.bus.Subscribe(func(e events.Event) { got = append(got, e) })
	a.write("x.md", "x\n")
	a.sync()
	if len(got) != 1 || got[0].Type != events.SyncCompleted || got[0].Origin != events.OriginSync {
		t.Fatalf("events %+v", got)
	}
}

// TestConverge makes random edits, creates, deletes and renames on two
// machines over many rounds with syncs in between, then checks that both
// end up with the same files and that every conflict copy still holds the
// local text it was made from.
func TestConverge(t *testing.T) {
	seeds := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if testing.Short() {
		seeds = seeds[:2]
	}
	for _, seed := range seeds {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			h := newHarness(t)
			nodes := []*node{h.node("a"), h.node("b")}
			rng := rand.New(rand.NewPCG(seed, seed*7))
			copies := map[string]string{} // conflict copy -> local text it saved
			next, merges := 0, 0
			record := func(n *node, res *Result) {
				merges += len(res.Merged)
				for _, c := range res.Conflicts {
					copies[c.Copy] = n.read(c.Copy)
				}
			}
			for range 40 {
				for _, n := range nodes {
					for range rng.IntN(5) {
						randomOp(t, n, rng, &next)
					}
				}
				for _, i := range rng.Perm(2)[:1+rng.IntN(2)] {
					record(nodes[i], nodes[i].sync())
				}
			}
			for range 4 {
				for _, n := range nodes {
					record(n, n.sync())
				}
			}
			sameFiles(t, nodes[0], nodes[1])
			files := nodes[0].files()
			for cp, text := range copies {
				if files[cp] != text {
					t.Errorf("conflict copy %s = %q, want %q", cp, files[cp], text)
				}
			}
			for _, n := range nodes {
				if res := n.sync(); res.Changed() || len(res.Errors) > 0 {
					t.Errorf("%s not idle: %+v", n.name, res)
				}
			}
			t.Logf("seed %d: %d files, %d merges, %d conflict copies", seed, len(files), merges, len(copies))
		})
	}
}

func randomOp(t *testing.T, n *node, rng *rand.Rand, next *int) {
	var notes []string
	for p := range n.files() {
		if !strings.Contains(p, "(conflict") {
			notes = append(notes, p)
		}
	}
	slices.Sort(notes)
	op := rng.IntN(10)
	if len(notes) == 0 || (op < 2 && len(notes) < 6) {
		*next++
		p := fmt.Sprintf("n%d-%s.md", *next, n.name)
		if rng.IntN(4) == 0 {
			p = "dir/" + p
		}
		lines := []string{fmt.Sprintf("# %s\n", p)}
		for i := range 3 + rng.IntN(5) {
			lines = append(lines, fmt.Sprintf("line %d\n", i))
		}
		n.write(p, strings.Join(lines, ""))
		return
	}
	p := notes[rng.IntN(len(notes))]
	switch {
	case op == 2:
		n.rm(p)
	case op == 3:
		*next++
		to := fmt.Sprintf("moved%d.md", *next)
		if err := os.Rename(filepath.Join(n.dir, filepath.FromSlash(p)), filepath.Join(n.dir, to)); err != nil {
			t.Fatal(err)
		}
	default:
		lines := splitLines(n.read(p))
		i := rng.IntN(len(lines) + 1)
		w := fmt.Sprintf("%s edit %d\n", n.name, rng.IntN(1000))
		switch {
		case i == len(lines) || rng.IntN(3) == 0:
			lines = slices.Insert(lines, i, w)
		case rng.IntN(2) == 0 && len(lines) > 1:
			lines = slices.Delete(lines, i, i+1)
		default:
			lines[i] = w
		}
		n.write(p, strings.Join(lines, ""))
	}
}
