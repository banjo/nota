package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/banjo/nota/internal/events"
)

// caseInsensitive reports whether dir's file system folds letter case.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	p := filepath.Join(dir, "CaseProbe")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	_, err := os.Stat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

func TestLocate(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "Projects/Plan.md", "# Plan\n")
	writeFile(t, v, "Top.md", "# Top\n")
	writeFile(t, v, ".nota/base/x.md", "base")
	writeFile(t, v, ".trash/Old.md", "old")
	writeFile(t, v, "Private/secret.md", "s")
	writeFile(t, v, "Notes.markdown", "m")
	writeFile(t, v, ".notaignore", "Private/\n")
	if err := v.ReloadIgnore(); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := v.Root()
	in := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

	cases := []struct {
		name, path, want string
		ok               bool
	}{
		{"inside", in("Projects/Plan.md"), "Projects/Plan.md", true},
		{"root note", in("Top.md"), "Top.md", true},
		{"dotdot inside", filepath.Join(root, "Projects", "..", "Top.md"), "Top.md", true},
		{"outside", filepath.Join(outside, "x.md"), "", false},
		{"escape with dotdot", filepath.Join(root, "..", filepath.Base(outside), "x.md"), "", false},
		{"sync base copy", in(".nota/base/x.md"), "", false},
		{"trash", in(".trash/Old.md"), "", false},
		{"notaignore", in("Private/secret.md"), "", false},
		{"markdown ext is not a note", in("Notes.markdown"), "", false},
		{"directory", in("Projects"), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel, ok, err := v.Locate(c.path)
			if err != nil {
				t.Fatal(err)
			}
			if rel != c.want || ok != c.ok {
				t.Fatalf("Locate(%s) = %q, %v; want %q, %v", c.path, rel, ok, c.want, c.ok)
			}
		})
	}

	if _, _, err := v.Locate(in("Missing.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: err = %v, want not exist", err)
	}
}

func TestLocateSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "Sub", "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The vault root itself is a link, as ~/Notes may be.
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	v, err := Open(link, Options{})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "o.md"), []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A link outside pointing in, a link inside pointing out, and a
	// symlinked folder inside the vault pointing at a vault folder.
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Symlink(filepath.Join(real, "Sub", "a.md"), filepath.Join(outside, "to-vault.md")))
	must(os.Symlink(filepath.Join(outside, "o.md"), filepath.Join(real, "points-out.md")))
	must(os.Symlink(filepath.Join(real, "Sub"), filepath.Join(real, "Alias")))

	cases := []struct {
		name, path, want string
		ok               bool
	}{
		{"through the root link", filepath.Join(link, "Sub", "a.md"), "Sub/a.md", true},
		{"through the real root", filepath.Join(real, "Sub", "a.md"), "Sub/a.md", true},
		{"link outside into the vault", filepath.Join(outside, "to-vault.md"), "Sub/a.md", true},
		{"link inside pointing out", filepath.Join(real, "points-out.md"), "", false},
		{"symlinked folder inside", filepath.Join(link, "Alias", "a.md"), "Sub/a.md", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel, ok, err := v.Locate(c.path)
			if err != nil {
				t.Fatal(err)
			}
			if rel != c.want || ok != c.ok {
				t.Fatalf("Locate(%s) = %q, %v; want %q, %v", c.path, rel, ok, c.want, c.ok)
			}
		})
	}
}

func TestLocateNFDAndCase(t *testing.T) {
	v := newVault(t)
	// The name on disk is decomposed, as macOS may store it.
	writeFile(t, v, nfd+".md", "# Möte\n")
	rel, ok, err := v.Locate(filepath.Join(v.Root(), nfd+".md"))
	if err != nil || !ok || rel != "Möte.md" {
		t.Fatalf("NFD on disk: %q %v %v, want Möte.md", rel, ok, err)
	}
	writeFile(t, v, "Inbox/Idea.md", "idea")
	upper := filepath.Join(v.Root(), "INBOX", "IDEA.md")
	rel, ok, err = v.Locate(upper)
	if caseInsensitive(t, v.Root()) {
		if err != nil || !ok || rel != "Inbox/Idea.md" {
			t.Fatalf("case variant: %q %v %v, want Inbox/Idea.md", rel, ok, err)
		}
		return
	}
	// Case-sensitive: the variant does not exist. A real other file that
	// differs only in case is not the note.
	if err == nil {
		t.Fatalf("case variant on a case-sensitive FS: %q %v, want an error", rel, ok)
	}
	writeFile(t, v, "Inbox/IDEA.md", "another file")
	rel, ok, err = v.Locate(filepath.Join(v.Root(), "Inbox", "IDEA.md"))
	if err != nil || !ok || rel != "Inbox/IDEA.md" {
		t.Fatalf("other-case file: %q %v %v, want Inbox/IDEA.md", rel, ok, err)
	}
}

func TestLocateFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs")
	}
	v := newVault(t)
	p := filepath.Join(v.Root(), "pipe.md")
	if err := mkfifo(p); err != nil {
		t.Skip("mkfifo:", err)
	}
	rel, ok, err := v.Locate(p)
	if err != nil || ok {
		t.Fatalf("FIFO: %q %v %v, want not ok", rel, ok, err)
	}
}

func TestLocateMemFS(t *testing.T) {
	v, err := OpenFS(NewMemFS(nil), "mem", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Locate("/x.md"); err == nil {
		t.Fatal("Locate on a MemFS: want an error")
	}
}

func TestIsMarkdown(t *testing.T) {
	for name, want := range map[string]bool{
		"a.md": true, "A.MD": true, "b.markdown": true, "c.Markdown": true,
		"d.txt": false, "md": false, "e.mdx": false, `dir\f.md`: true,
	} {
		if got := IsMarkdown(name); got != want {
			t.Errorf("IsMarkdown(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestImport(t *testing.T) {
	bus := events.New()
	var got []events.Event
	bus.Subscribe(func(e events.Event) { got = append(got, e) }, events.NoteCreated, events.InboxAdded)
	v, err := Open(t.TempDir(), Options{Bus: bus})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("\xef\xbb\xbf---\ntags: [a]\n---\n# README\r\nbody\r\n")
	n, err := v.Import("Inbox", "README.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if n.Path != "Inbox/README.md" {
		t.Fatalf("path = %q", n.Path)
	}
	disk, err := os.ReadFile(filepath.Join(v.Root(), "Inbox", "README.md"))
	if err != nil || !bytes.Equal(disk, data) {
		t.Fatalf("bytes changed: %q %v", disk, err)
	}
	if len(got) != 2 || got[0].Type != events.NoteCreated || got[1].Type != events.InboxAdded {
		t.Fatalf("events = %+v", got)
	}

	// A clash, ignoring case, gets " 2"; .markdown becomes .md.
	n, err = v.Import("Inbox", "readme.markdown", []byte("x"))
	if err != nil || n.Path != "Inbox/readme 2.md" {
		t.Fatalf("clash: %v %v", n, err)
	}
	// A new folder is made; unsafe characters are replaced.
	n, err = v.Import("Projects/New", "a:b?.md", []byte("y"))
	if err != nil || n.Path != "Projects/New/a b.md" {
		t.Fatalf("new folder: %+v %v", n, err)
	}
	// The vault root.
	n, err = v.Import("", "Root.md", []byte("r"))
	if err != nil || n.Path != "Root.md" {
		t.Fatalf("root: %+v %v", n, err)
	}
	// Ignored folders and escapes are refused.
	for _, folder := range []string{".nota", ".trash/x", "../out"} {
		if _, err := v.Import(folder, "a.md", []byte("z")); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Import into %q: err = %v, want ErrInvalidPath", folder, err)
		}
	}
	if strings.Contains(strings.Join(paths(t, v), ","), ".nota") {
		t.Fatal("wrote into .nota")
	}
}
