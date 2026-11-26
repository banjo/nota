package vault

import (
	"errors"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/banjo/nota/internal/events"
)

func TestMemFSConforms(t *testing.T) {
	m := NewMemFS(map[string]string{"a.md": "a", "Folder/b.md": "b"})
	if err := m.MkdirAll("Empty/Sub"); err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(m, "a.md", "Folder/b.md", "Empty/Sub"); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteFile("Missing/x.md", nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("write without parent: %v", err)
	}
	if err := m.Remove("Folder"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("remove non-empty dir: %v", err)
	}
	if err := m.Rename("a.md", "Folder/a.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stat("a.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old name after rename: %v", err)
	}
}

func TestDirFSRejectsInvalidNames(t *testing.T) {
	f := DirFS(t.TempDir())
	for _, name := range []string{"../x.md", "/abs.md", "a/../../x.md"} {
		if err := f.WriteFile(name, []byte("x")); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("WriteFile(%q) = %v, want fs.ErrInvalid", name, err)
		}
	}
}

// TestVaultOnMemFS runs the core operations on the in-memory file system:
// the vault must not depend on the OS.
func TestVaultOnMemFS(t *testing.T) {
	m := NewMemFS(map[string]string{
		nfd + ".md":             "# Möte\n",
		"Projects/Plan.md":      "plan",
		".obsidian/x.md":        "ignored",
		".notaignore":           "private/\n",
		"private/secret.md":     "ignored",
		"Projects/image.png":    "x",
		"Projects/notes/sub.md": "sub",
	})
	bus := events.New()
	var got []string
	bus.Subscribe(func(e events.Event) { got = append(got, string(e.Type)+" "+e.Path) })
	v, err := OpenFS(m, "mem", Options{Bus: bus})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Möte.md", "Projects/Plan.md", "Projects/notes/sub.md"}; !slices.Equal(paths(t, v), want) {
		t.Fatalf("List = %q, want %q", paths(t, v), want)
	}
	n, err := v.Read("Möte")
	if err != nil || n.Title != "Möte" {
		t.Fatalf("Read NFD: %+v %v", n, err)
	}
	if _, created, err := v.Write("Möte.md", []byte("new")); err != nil || created {
		t.Fatalf("Write NFC over NFD: %v %v", created, err)
	}
	if b, _ := m.ReadFile(nfd + ".md"); string(b) != "new" {
		t.Fatalf("NFD file = %q", b)
	}
	if _, _, err := v.Write("projects/x.md", []byte("x")); !errors.Is(err, ErrCaseCollision) {
		t.Fatalf("case collision: %v", err)
	}
	c, err := v.Create("Inbox", []byte("Idé\n"))
	if err != nil || c.Path != "Inbox/Idé.md" {
		t.Fatalf("Create = %+v %v", c, err)
	}
	if to, err := v.Move(c.Path, "Archive/2026"); err != nil || to != "Archive/2026/Idé.md" {
		t.Fatalf("Move = %q %v", to, err)
	}
	if tr, err := v.Delete("Projects/Plan.md"); err != nil || tr != ".trash/Projects/Plan.md" {
		t.Fatalf("Delete = %q %v", tr, err)
	}
	if _, err := v.DiskPath("Möte.md"); err == nil {
		t.Fatal("DiskPath on MemFS should fail")
	}
	want := []string{
		"note.saved Möte.md",
		"note.created Inbox/Idé.md",
		"inbox.added Inbox/Idé.md",
		"note.moved Archive/2026/Idé.md",
		"note.deleted Projects/Plan.md",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("events %q, want %q", got, want)
	}
}

func TestSaveStampsUpdated(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 42, 0, 0, time.FixedZone("CET", 3600))
	m := NewMemFS(map[string]string{
		"Fm.md":    "---\ncreated: 2026-01-01\n---\nBody\n",
		"Plain.md": "Body\n",
		"Bad.md":   "---\ntags: [x\n---\nBody\n",
	})
	bus := events.New()
	var saved int
	bus.Subscribe(func(events.Event) { saved++ }, events.NoteSaved)
	v, err := OpenFS(m, "mem", Options{Bus: bus, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	check := func(name, want string) {
		t.Helper()
		b, _ := m.ReadFile(name)
		if string(b) != want {
			t.Fatalf("%s =\n%s\nwant\n%s", name, b, want)
		}
	}
	if _, _, err := v.Save("Fm", []byte("---\ncreated: 2026-01-01\n---\nNew body\n")); err != nil {
		t.Fatal(err)
	}
	check("Fm.md", "---\ncreated: 2026-01-01\nupdated: 2026-11-22T10:42:00+01:00\n---\nNew body\n")
	// The same content again: no write, no event.
	b, _ := m.ReadFile("Fm.md")
	if _, _, err := v.Save("Fm", b); err != nil || saved != 1 {
		t.Fatalf("no-op save: saved=%d err=%v", saved, err)
	}
	// No frontmatter before: none is added for the timestamp.
	if _, _, err := v.Save("Plain", []byte("Body 2\n")); err != nil {
		t.Fatal(err)
	}
	check("Plain.md", "Body 2\n")
	// Frontmatter that does not parse is not edited.
	if _, _, err := v.Save("Bad", []byte("---\ntags: [x\n---\nBody 2\n")); err != nil {
		t.Fatal(err)
	}
	check("Bad.md", "---\ntags: [x\n---\nBody 2\n")
	// A new note is written as given.
	if _, created, err := v.Save("New", []byte("---\na: 1\n---\n")); err != nil || !created {
		t.Fatalf("new: %v %v", created, err)
	}
	check("New.md", "---\na: 1\n---\n")
	// Pinning, moving a pin and unpinning change only `pinned`.
	for _, fm := range []string{"a: 1\npinned: 10\n", "a: 1\npinned: 5\n", "a: 1\n"} {
		if _, _, err := v.Save("New", []byte("---\n"+fm+"---\n")); err != nil {
			t.Fatal(err)
		}
		check("New.md", "---\n"+fm+"---\n")
	}
}
