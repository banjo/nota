package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// openerRig is an Opener over a real vault with recorded side effects.
type openerRig struct {
	o       *Opener
	s       *Vault
	vault   string
	out     string // a folder outside the vault
	mu      sync.Mutex
	nudges  int
	shows   int
	trashed []string
}

func newOpenerRig(t *testing.T, files map[string]string) *openerRig {
	t.Helper()
	s, dir, _ := testVault(t, files)
	r := &openerRig{s: s, vault: dir, out: t.TempDir()}
	r.o = newOpener(openerEnv{
		locate: s.locateNote,
		imp:    s.importNote,
		nudge:  func(int64) { r.mu.Lock(); r.nudges++; r.mu.Unlock() },
		show:   func() { r.mu.Lock(); r.shows++; r.mu.Unlock() },
		trash: func(f string) error {
			r.mu.Lock()
			r.trashed = append(r.trashed, f)
			r.mu.Unlock()
			return os.Remove(f)
		},
		home: filepath.Dir(r.out),
	})
	return r
}

// counts waits until the page was shown and nudged n times (or a
// second passed) and returns how often.
func (r *openerRig) counts(n int) (shows, nudges int) {
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		shows, nudges = r.shows, r.nudges
		r.mu.Unlock()
		if (shows >= n && nudges >= n) || time.Now().After(deadline) {
			return shows, nudges
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (r *openerRig) write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(r.out, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// open delivers files as the OS would and waits until the page could
// take them all.
func (r *openerRig) open(t *testing.T, files ...string) []Opened {
	t.Helper()
	for _, f := range files {
		r.o.receive(f)
	}
	deadline := time.Now().Add(5 * time.Second)
	var got []Opened
	for len(got) < len(files) && time.Now().Before(deadline) {
		got = append(got, r.o.Take()...)
		if len(got) < len(files) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if len(got) != len(files) {
		t.Fatalf("took %d of %d files", len(got), len(files))
	}
	return got
}

func TestOpenerClassifies(t *testing.T) {
	r := newOpenerRig(t, map[string]string{"Projects/Plan.md": "# Plan\n"})
	outside := r.write(t, "Downloads/README.md", "# Read me\n")
	md := r.write(t, "notes.markdown", "x")
	txt := r.write(t, "a.txt", "x")
	bin := r.write(t, "bin.md", "abc\x00def")
	big := filepath.Join(r.out, "big.md")
	if err := os.WriteFile(big, bytes.Repeat([]byte("a"), MaxOpenSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	got := r.open(t, filepath.Join(r.vault, "Projects", "Plan.md"), outside, md, txt, bin, big,
		filepath.Join(r.out, "missing.md"))
	if got[0].Kind != "note" || got[0].NotePath != "Projects/Plan.md" || got[0].ID != "" {
		t.Errorf("vault note: %+v", got[0])
	}
	if got[1].Kind != "external" || got[1].ID == "" || got[1].Name != "README.md" {
		t.Errorf("outside: %+v", got[1])
	}
	if want := "~" + string(filepath.Separator) + filepath.Join(filepath.Base(r.out), "Downloads"); got[1].Dir != want {
		t.Errorf("dir = %q, want %q", got[1].Dir, want)
	}
	if got[2].Kind != "external" {
		t.Errorf(".markdown: %+v", got[2])
	}
	for i, want := range map[int]string{3: "Markdown", 4: "text file", 5: "too large", 6: "isn't there"} {
		if got[i].Kind != "error" || !strings.Contains(got[i].Error, want) {
			t.Errorf("file %d: %+v, want an error with %q", i, got[i], want)
		}
	}
	if shows, nudges := r.counts(7); shows != 7 || nudges != 7 {
		t.Errorf("shows %d nudges %d, want 7 each", shows, nudges)
	}
	if len(r.o.Take()) != 0 {
		t.Error("Take did not drain the queue")
	}
	// Previews lists the outside files, for a page that loads again.
	if p := r.o.Previews(); len(p) != 2 || p[0].ID != got[1].ID || p[1].ID != got[2].ID {
		t.Errorf("previews %+v", p)
	}
}

func TestOpenerDedupesAndOrders(t *testing.T) {
	r := newOpenerRig(t, nil)
	a := r.write(t, "a.md", "a")
	b := r.write(t, "b.md", "b")
	got := r.open(t, a, b, a)
	if got[0].Name != "a.md" || got[1].Name != "b.md" {
		t.Fatalf("order: %+v", got)
	}
	if got[2].ID != got[0].ID {
		t.Errorf("same file twice: ids %s and %s", got[0].ID, got[2].ID)
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(r.out, "link.md")
		if err := os.Symlink(a, link); err != nil {
			t.Fatal(err)
		}
		if l := r.open(t, link); l[0].ID != got[0].ID || l[0].Name != "a.md" {
			t.Errorf("through a link: %+v, want id %s", l[0], got[0].ID)
		}
	}
}

func TestOpenerReadAndSave(t *testing.T) {
	r := newOpenerRig(t, map[string]string{"Inbox/README.md": "taken"})
	content := "---\ntags: [x]\nfoo: bar\n---\n# Read me\n\nHello.\n"
	file := r.write(t, "README.md", content)
	id := r.open(t, file)[0].ID

	doc, err := r.o.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Read me" || doc.Body != "# Read me\n\nHello.\n" || doc.Content != content ||
		doc.Properties["foo"] != "bar" || !doc.UTF8 || doc.Hash != protocol.Hash([]byte(content)) {
		t.Fatalf("doc %+v", doc)
	}

	// A stale hash is a conflict: nothing is written.
	out, err := r.o.Save(id, SaveRequest{Hash: "stale"})
	if err != nil || !out.Conflict {
		t.Fatalf("stale: %+v %v", out, err)
	}
	// Saved as is: the bytes exactly, " 2" next to the existing note.
	out, err = r.o.Save(id, SaveRequest{Hash: doc.Hash})
	if err != nil || out.Path != "Inbox/README 2.md" {
		t.Fatalf("save: %+v %v", out, err)
	}
	saved, _ := os.ReadFile(filepath.Join(r.vault, "Inbox", "README 2.md"))
	if string(saved) != content {
		t.Fatalf("saved %q", saved)
	}
	if orig, _ := os.ReadFile(file); string(orig) != content {
		t.Fatal("the original changed")
	}
	if _, err := r.o.Read(id); err == nil {
		t.Error("the preview is still open after saving")
	}
	// The index knows the new note.
	if _, err := r.s.Get("Inbox/README 2.md"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenerSaveDraft(t *testing.T) {
	r := newOpenerRig(t, nil)
	content := "---\nfoo: bar\n---\n# Draft\n\nold\n"
	file := r.write(t, "d.md", content)
	id := r.open(t, file)[0].ID
	doc, err := r.o.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	// The original changes on disk while the draft is open: the draft
	// still saves, with the frontmatter the user saw.
	if err := os.WriteFile(file, []byte("changed elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := r.o.Save(id, SaveRequest{Folder: "/", Edited: true, Body: "# Draft\n\nnew\n", Hash: doc.Hash})
	if err != nil || out.Conflict || out.Path != "d.md" {
		t.Fatalf("save draft: %+v %v", out, err)
	}
	saved, _ := os.ReadFile(filepath.Join(r.vault, "d.md"))
	if string(saved) != "---\nfoo: bar\n---\n# Draft\n\nnew\n" {
		t.Fatalf("saved %q", saved)
	}
	if orig, _ := os.ReadFile(file); string(orig) != "changed elsewhere" {
		t.Fatal("the original was written")
	}
}

func TestOpenerMove(t *testing.T) {
	r := newOpenerRig(t, nil)
	file := r.write(t, "m.md", "# M\n")
	id := r.open(t, file)[0].ID
	doc, _ := r.o.Read(id)
	// Moving needs the hash, and a matching one.
	if out, err := r.o.Save(id, SaveRequest{Move: true}); err != nil || !out.Conflict {
		t.Fatalf("move without hash: %+v %v", out, err)
	}
	if out, err := r.o.Save(id, SaveRequest{Move: true, Edited: true, Body: "x", Hash: "stale"}); err != nil || !out.Conflict {
		t.Fatalf("move with a stale hash: %+v %v", out, err)
	}
	if len(r.trashed) != 0 {
		t.Fatal("trashed on a conflict")
	}
	out, err := r.o.Save(id, SaveRequest{Folder: "Projects", Move: true, Hash: doc.Hash})
	if err != nil || out.Path != "Projects/m.md" || out.TrashError != "" {
		t.Fatalf("move: %+v %v", out, err)
	}
	if len(r.trashed) != 1 || filepath.Base(r.trashed[0]) != "m.md" {
		t.Fatalf("trashed %v", r.trashed)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("the original is still there")
	}
}

func TestOpenerOverwriteOriginal(t *testing.T) {
	r := newOpenerRig(t, nil)
	file := r.write(t, "o.md", "---\na: 1\n---\nold\n")
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	id := r.open(t, file)[0].ID
	doc, _ := r.o.Read(id)
	if out, err := r.o.OverwriteOriginal(id, "new\n", ""); err != nil || !out.Conflict {
		t.Fatalf("no hash: %+v %v", out, err)
	}
	if out, err := r.o.OverwriteOriginal(id, "new\n", doc.Hash); err != nil || out.Conflict {
		t.Fatalf("overwrite: %+v %v", out, err)
	}
	got, _ := os.ReadFile(file)
	if string(got) != "---\na: 1\n---\nnew\n" {
		t.Fatalf("file %q", got)
	}
	if st, _ := os.Stat(file); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	// The old hash no longer matches.
	if out, _ := r.o.OverwriteOriginal(id, "again\n", doc.Hash); !out.Conflict {
		t.Error("a second write with the old hash went through")
	}
}

func TestOpenerNonUTF8(t *testing.T) {
	r := newOpenerRig(t, nil)
	file := r.write(t, "latin1.md", "caf\xe9\n")
	id := r.open(t, file)[0].ID
	doc, err := r.o.Read(id)
	if err != nil || doc.UTF8 || doc.Body != "caf\uFFFD\n" {
		t.Fatalf("doc %+v %v", doc, err)
	}
	if _, err := r.o.Save(id, SaveRequest{Hash: doc.Hash}); err == nil {
		t.Fatal("saved a non-UTF-8 file")
	}
}

func TestOpenerFollow(t *testing.T) {
	r := newOpenerRig(t, map[string]string{"Projects/Plan.md": "# Plan\n"})
	file := r.write(t, "docs/index.md", "[next](next.md)")
	r.write(t, "docs/next.md", "# Next")
	r.write(t, "secret.txt", "s")
	id := r.open(t, file)[0].ID
	op, err := r.o.Follow(id, "next.md#part")
	if err != nil || op.Kind != "external" || op.Name != "next.md" {
		t.Fatalf("relative link: %+v %v", op, err)
	}
	// A relative link into the vault opens the note.
	rel, _ := filepath.Rel(filepath.Join(r.out, "docs"), filepath.Join(r.vault, "Projects", "Plan.md"))
	op, err = r.o.Follow(id, filepath.ToSlash(rel))
	if err != nil || op.Kind != "note" || op.NotePath != "Projects/Plan.md" {
		t.Fatalf("link into the vault: %+v %v", op, err)
	}
	for _, href := range []string{"../secret.txt", "/etc/passwd.md", "file:///etc/x.md", "javascript:alert(1)",
		"https://example.com/x.md", "missing.md", ""} {
		if op, err := r.o.Follow(id, href); err == nil {
			t.Errorf("Follow(%q) = %+v, want an error", href, op)
		}
	}
}

func TestOpenerWithoutVault(t *testing.T) {
	// Onboarding: no vault, everything is an outside file and Save fails;
	// what is open is passed on to the relaunch.
	out := t.TempDir()
	o := newOpener(openerEnv{})
	a := filepath.Join(out, "a.md")
	if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.add(a)
	q := o.Take()
	if len(q) != 1 || q[0].Kind != "external" {
		t.Fatalf("queue %+v", q)
	}
	if _, err := o.Save(q[0].ID, SaveRequest{}); err != ErrNoVault {
		t.Fatalf("save err = %v", err)
	}
	if p := o.pending(); len(p) != 1 || filepath.Base(p[0]) != "a.md" {
		t.Fatalf("pending %v", p)
	}
	o.Dismiss(q[0].ID)
	if p := o.pending(); len(p) != 0 {
		t.Fatalf("pending after dismiss %v", p)
	}
}

func TestRestartPassesOpenFiles(t *testing.T) {
	d, _, _ := testDesktop(t)
	var args []string
	d.relaunch = func(files ...string) { args = files }
	d.op = newOpener(openerEnv{})
	f := filepath.Join(t.TempDir(), "x.md")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.op.add(f)
	d.restart()
	if len(args) != 1 || filepath.Base(args[0]) != "x.md" {
		t.Fatalf("relaunch args %v", args)
	}
}

func TestOpenerWatchesPreviews(t *testing.T) {
	var mu sync.Mutex
	var changed []string
	watching := map[string]context.Context{}
	o := newOpener(openerEnv{
		watch: func(ctx context.Context, file string, fn func()) {
			mu.Lock()
			watching[filepath.Base(file)] = ctx
			mu.Unlock()
			fn() // as if the file changed once
		},
		changed: func(id string) { mu.Lock(); changed = append(changed, id); mu.Unlock() },
	})
	f := filepath.Join(t.TempDir(), "w.md")
	if err := os.WriteFile(f, []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.add(f)
	id := o.Take()[0].ID
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(changed)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	ctx := watching["w.md"]
	got := append([]string{}, changed...)
	mu.Unlock()
	if len(got) != 1 || got[0] != id || ctx == nil {
		t.Fatalf("changed %v, watching %v", got, watching)
	}
	o.Dismiss(id)
	if ctx.Err() == nil {
		t.Fatal("the watch still runs after Dismiss")
	}
}

func TestOpenerSaveDraftOfAGoneFile(t *testing.T) {
	r := newOpenerRig(t, nil)
	file := r.write(t, "gone.md", "---\na: 1\n---\nold\n")
	id := r.open(t, file)[0].ID
	doc, _ := r.o.Read(id)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err := r.o.Save(id, SaveRequest{Hash: doc.Hash}); err == nil {
		t.Fatal("saved a gone file without a draft")
	}
	if _, err := r.o.Save(id, SaveRequest{Edited: true, Body: "x", Hash: doc.Hash, Move: true}); err == nil {
		t.Fatal("moved a gone file")
	}
	out, err := r.o.Save(id, SaveRequest{Edited: true, Body: "new\n", Hash: doc.Hash})
	if err != nil || out.Path != "Inbox/gone.md" {
		t.Fatalf("draft of a gone file: %+v %v", out, err)
	}
	got, _ := os.ReadFile(filepath.Join(r.vault, "Inbox", "gone.md"))
	if string(got) != "---\na: 1\n---\nnew\n" {
		t.Fatalf("saved %q", got)
	}
}
