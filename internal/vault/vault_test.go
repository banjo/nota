package vault

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const nfd = "Mo\u0308te" // "Möte" decomposed, as macOS may store it

func newVault(t *testing.T) *Vault {
	t.Helper()
	v, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func writeFile(t *testing.T, v *Vault, rel, content string) {
	t.Helper()
	p := filepath.Join(v.Root(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func paths(t *testing.T, v *Vault) []string {
	t.Helper()
	es, err := v.List()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Path)
	}
	slices.Sort(out)
	return out
}

func TestTitle(t *testing.T) {
	cases := []struct{ body, want string }{
		{"# Möte med Anna\n\ntext", "Möte med Anna"},
		{"\n\n  Första raden  \nandra", "Första raden"},
		{"## Closed heading ##\n", "Closed heading"},
		{"#tag first\n", "#tag first"},
		{"#\n\nReal title", "Real title"},
		{"", "fallback"},
		{"   \n\t\n", "fallback"},
		{nfd + "\n", "Möte"},
	}
	for _, c := range cases {
		if got := Title([]byte(c.body), "fallback"); got != c.want {
			t.Errorf("Title(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

func TestFilename(t *testing.T) {
	cases := []struct{ title, want string }{
		{"Möte med Åsa & Örjan", "Möte med Åsa & Örjan"},
		{"a/b\\c:d*e?f\"g<h>i|j", "a b c d e f g h i j"},
		{"[[link]] #tag ^block", "link tag block"},
		{"  ..hidden. ", "hidden"},
		{"", "Untitled"},
		{"???", "Untitled"},
		{"CON", "CON note"},
		{nfd, "Möte"},
		{"tab\tand\nnewline", "tab and newline"},
		{strings.Repeat("x", 150), strings.Repeat("x", 100)},
	}
	for _, c := range cases {
		if got := Filename(c.title); got != c.want {
			t.Errorf("Filename(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

func TestCleanPath(t *testing.T) {
	good := map[string]string{
		"a/b.md":       "a/b.md",
		"/a//b/./c.md": "a/b/c.md",
		`a\b.md`:       "a/b.md",
		"":             "",
		"/":            "",
		nfd + ".md":    "Möte.md",
	}
	for in, want := range good {
		got, err := CleanPath(in)
		if err != nil || got != want {
			t.Errorf("CleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"../x.md", "a/../../x", "a/../b"} {
		if _, err := CleanPath(in); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CleanPath(%q) err = %v, want ErrInvalidPath", in, err)
		}
	}
	if got, _ := NotePath("Inbox/Note"); got != "Inbox/Note.md" {
		t.Errorf("NotePath = %q", got)
	}
}

func TestIgnore(t *testing.T) {
	m := ParseIgnoreFile([]byte("# comment\n*.tmp.md\n/drafts/\narchive/**/old.md\nsecret.md\n!keep/secret.md\n"))
	cases := []struct {
		path  string
		dir   bool
		match bool
	}{
		{".git", true, true},
		{".git/config", false, true},
		{"sub/.obsidian/x.md", false, true},
		{".nota/index.db", false, true},
		{".trash/a.md", false, true},
		{"a/.DS_Store", false, true},
		{"note.md", false, false},
		{"x.tmp.md", false, true},
		{"deep/x.tmp.md", false, true},
		{"drafts/a.md", false, true},
		{"sub/drafts/a.md", false, false},
		{"archive/old.md", false, true},
		{"archive/2024/q1/old.md", false, true},
		{"archive/new.md", false, false},
		{"secret.md", false, true},
		{"a/secret.md", false, true},
		{"keep/secret.md", false, false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, c.dir); got != c.match {
			t.Errorf("Match(%q, %v) = %v, want %v", c.path, c.dir, got, c.match)
		}
	}
}

func TestWalkIgnoresAndNormalizes(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "a.md", "a")
	writeFile(t, v, "Folder/"+nfd+".md", "möte")
	writeFile(t, v, ".obsidian/workspace.md", "x")
	writeFile(t, v, ".trash/old.md", "x")
	writeFile(t, v, "image.png", "x")
	writeFile(t, v, "private/x.md", "x")
	writeFile(t, v, IgnoreFile, "private/\n")
	if err := v.ReloadIgnore(); err != nil {
		t.Fatal(err)
	}
	got := paths(t, v)
	want := []string{"Folder/Möte.md", "a.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("List = %q, want %q", got, want)
	}
}

func TestReadNFDOnDisk(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, nfd+".md", "---\ntags: [x]\n---\n# Möte\nbody\n")
	n, err := v.Read("Möte") // NFC input, NFD on disk
	if err != nil {
		t.Fatal(err)
	}
	if n.Path != "Möte.md" || n.Title != "Möte" || n.Body != "# Möte\nbody\n" {
		t.Fatalf("note = %+v", n)
	}
	if tags, _ := n.Props["tags"].([]any); len(tags) != 1 {
		t.Fatalf("props = %v", n.Props)
	}
	// Writing through the NFC path updates the NFD file instead of adding a twin.
	if _, created, err := v.Write("Möte.md", []byte("new")); err != nil || created {
		t.Fatalf("Write: created=%v err=%v", created, err)
	}
	entries, _ := os.ReadDir(v.Root())
	if len(entries) != 1 || entries[0].Name() != nfd+".md" {
		t.Fatalf("disk = %v", entries)
	}
}

func TestCreate(t *testing.T) {
	v := newVault(t)
	n, err := v.Create("Inbox", []byte("# Möte med Åsa\n\ntext\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n.Path != "Inbox/Möte med Åsa.md" || n.Title != "Möte med Åsa" {
		t.Fatalf("created %+v", n)
	}
	n2, err := v.Create("Inbox", []byte("Möte med Åsa\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n2.Path != "Inbox/Möte med Åsa 2.md" {
		t.Fatalf("dedupe: %q", n2.Path)
	}
	n3, err := v.Create("Inbox", []byte("möte MED åsa"))
	if err != nil {
		t.Fatal(err)
	}
	if n3.Path != "Inbox/möte MED åsa 3.md" {
		t.Fatalf("case-insensitive dedupe: %q", n3.Path)
	}
	n4, err := v.Create("", []byte("---\ntags: [a]\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n4.Path != "Untitled.md" || n4.Title != "Untitled" {
		t.Fatalf("untitled: %+v", n4)
	}
	if _, err := v.Create(".nota", []byte("x")); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("create in ignored folder: %v", err)
	}
	if _, err := v.Create("../out", []byte("x")); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("create outside vault: %v", err)
	}
}

func TestCaseCollision(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "Projects/Plan.md", "plan")
	for _, p := range []string{"Projects/plan.md", "projects/Other.md", "PROJECTS/plan.md"} {
		_, _, err := v.Write(p, []byte("x"))
		if !errors.Is(err, ErrCaseCollision) {
			t.Fatalf("Write(%q) err = %v, want ErrCaseCollision", p, err)
		}
		if !strings.Contains(err.Error(), "Projects") {
			t.Errorf("error should name the existing path: %v", err)
		}
	}
	if _, err := v.Move("Projects/Plan.md", "projects"); !errors.Is(err, ErrCaseCollision) {
		t.Fatalf("Move into case-colliding folder: %v", err)
	}
	if _, err := v.Read("projects/plan"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read with wrong case: %v", err)
	}
	// Renaming a note to another case of its own name is allowed.
	if runtime.GOOS != "windows" {
		if got, err := v.Rename("Projects/Plan.md", "Projects/PLAN.md"); err != nil || got != "Projects/PLAN.md" {
			t.Fatalf("case rename: %q %v", got, err)
		}
	}
}

func TestWriteAtomicKeepsModeAndCreatesFolders(t *testing.T) {
	v := newVault(t)
	rel, created, err := v.Write("a/b/c", []byte("hello"))
	if err != nil || !created || rel != "a/b/c.md" {
		t.Fatalf("Write = %q %v %v", rel, created, err)
	}
	p := filepath.Join(v.Root(), "a", "b", "c.md")
	if runtime.GOOS != "windows" {
		if err := os.Chmod(p, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, created, err := v.Write(rel, []byte("again")); err != nil || created {
		t.Fatalf("rewrite: %v %v", created, err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "again" {
		t.Fatalf("content %q", b)
	}
	if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestMoveAndDelete(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "Inbox/Idé.md", "idé")
	writeFile(t, v, "Projects/Idé.md", "other")
	if _, err := v.Move("Inbox/Idé", "Projects"); !errors.Is(err, ErrExists) {
		t.Fatalf("move onto existing: %v", err)
	}
	got, err := v.Move("Inbox/Idé", "Archive/2026")
	if err != nil || got != "Archive/2026/Idé.md" {
		t.Fatalf("Move = %q %v", got, err)
	}
	if _, err := v.Move("Inbox/Idé", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("move missing: %v", err)
	}
	tr, err := v.Delete("Projects/Idé.md")
	if err != nil || tr != ".trash/Projects/Idé.md" {
		t.Fatalf("Delete = %q %v", tr, err)
	}
	writeFile(t, v, "Projects/Idé.md", "again")
	if tr, err = v.Delete("Projects/Idé.md"); err != nil || tr != ".trash/Projects/Idé 2.md" {
		t.Fatalf("second Delete = %q %v", tr, err)
	}
	if got := paths(t, v); !slices.Equal(got, []string{"Archive/2026/Idé.md"}) {
		t.Fatalf("after delete: %q", got)
	}
}

func TestDiscover(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv(EnvVault, "")

	loc, err := Discover("")
	if err != nil || loc.Dir != filepath.Join(home, "Notes") || loc.Source != "default" {
		t.Fatalf("default: %+v %v", loc, err)
	}
	cfg := filepath.Join(home, "cfg", "nota", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("vault = \"~/Cfg Vault\"\ninbox = \"Inkorg\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loc, err = Discover("")
	if err != nil || loc.Dir != filepath.Join(home, "Cfg Vault") || loc.Source != "config" || loc.Config.Inbox != "Inkorg" {
		t.Fatalf("config: %+v %v", loc, err)
	}
	env := filepath.Join(home, "env")
	t.Setenv(EnvVault, env)
	if loc, _ = Discover(""); loc.Dir != env || loc.Source != "env" {
		t.Fatalf("env: %+v", loc)
	}
	flag := filepath.Join(home, "flag")
	if loc, _ = Discover(flag); loc.Dir != flag || loc.Source != "flag" {
		t.Fatalf("flag: %+v", loc)
	}
	if err := os.WriteFile(cfg, []byte("vault = [broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(""); err == nil {
		t.Fatal("broken config should fail")
	}
}

func TestOpenInbox(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new")
	if _, err := Open(dir, Options{}); err == nil {
		t.Fatal("missing vault without Create should fail")
	}
	v, err := Open(dir, Options{Create: true, Inbox: "/Inkorg/"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Inbox() != "Inkorg" {
		t.Fatalf("inbox %q", v.Inbox())
	}
}
