package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/replace"
)

// testVault opens a vault in a temp dir with a few notes. Published
// change batches are collected in the returned func.
func testVault(t *testing.T, files map[string]string) (*Vault, string, func() []string) {
	t.Helper()
	t.Setenv("NOTA_NOW", "2026-11-22T10:42:00+01:00")
	dir := t.TempDir()
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := openVault(dir, "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.ix.Close() })
	var mu sync.Mutex
	var got []string
	s.changes.delay = time.Millisecond
	s.changes.publish = func(c Changes) {
		mu.Lock()
		got = append(got, c.Paths...)
		mu.Unlock()
	}
	changed := func() []string {
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		out := slices.Clone(got)
		got = nil
		slices.Sort(out)
		return out
	}
	return s, dir, changed
}

func read(t *testing.T, dir, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const plan = "---\ntags: [nota, sync]\nstatus: draft\ncustom: \"kept  as is\" # comment\nupdated: 2026-11-01T09:00:00+01:00\n---\n# Sync-protokoll\n\nSe [[CLI-kommandon]].\n"

func TestTreeListAndGet(t *testing.T) {
	s, _, _ := testVault(t, map[string]string{
		"Projekt/Nota/Sync-protokoll.md": plan,
		"Projekt/Nota/CLI-kommandon.md":  "# CLI-kommandon\n\nLänkar till [[Sync-protokoll]].\n",
		"Inbox/Ringa elbolaget.md":       "Ringa elbolaget om fakturan\n",
		".nota/views/utkast.yaml":        "name: Utkast\nwhere: { status: draft }\n",
	})
	tr, err := s.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if tr.Inbox != "Inbox" || len(tr.Notes) != 3 {
		t.Fatalf("tree = %+v", tr)
	}
	var folders []string
	for _, f := range tr.Folders {
		folders = append(folders, f.Path)
	}
	if !slices.Equal(folders, []string{"Inbox", "Projekt", "Projekt/Nota"}) || tr.Folders[1].Notes != 2 {
		t.Fatalf("folders = %+v", tr.Folders)
	}
	n, err := s.Get("Projekt/Nota/Sync-protokoll.md")
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Sync-protokoll" || n.Properties["status"] != "draft" || !slices.Equal(n.Tags, []string{"nota", "sync"}) {
		t.Fatalf("note = %+v", n)
	}
	if len(n.Backlinks) != 1 || n.Backlinks[0].From != "Projekt/Nota/CLI-kommandon.md" {
		t.Fatalf("backlinks = %+v", n.Backlinks)
	}
	if n.Body != "# Sync-protokoll\n\nSe [[CLI-kommandon]].\n" || n.Content != plan || len(n.Hash) != 64 {
		t.Fatalf("body/content/hash = %q %q %q", n.Body, n.Content, n.Hash)
	}
	vs, err := s.Views()
	if err != nil || len(vs) != 1 || vs[0].Name != "Utkast" || vs[0].Count != 1 {
		t.Fatalf("views = %+v, %v", vs, err)
	}
	ls, err := s.List(ListQuery{View: "utkast"})
	if err != nil || len(ls) != 1 {
		t.Fatalf("view list = %+v, %v", ls, err)
	}
	ls, err = s.List(ListQuery{Tag: "nota"})
	if err != nil || len(ls) != 1 {
		t.Fatalf("tag list = %+v, %v", ls, err)
	}
	hits, err := s.Search("fakturan", ListQuery{})
	if err != nil || len(hits) != 1 || hits[0].Path != "Inbox/Ringa elbolaget.md" {
		t.Fatalf("search = %+v, %v", hits, err)
	}
	if hits, err := s.Search("  ", ListQuery{}); err != nil || len(hits) != 0 {
		t.Fatalf("empty search = %+v, %v", hits, err)
	}
	ts, err := s.Tags()
	if err != nil || len(ts) != 2 {
		t.Fatalf("tags = %+v, %v", ts, err)
	}
}

func TestSaveKeepsFrontmatterAndChecksHash(t *testing.T) {
	s, dir, changed := testVault(t, map[string]string{"Projekt/Plan.md": plan})
	n, err := s.Get("Projekt/Plan.md")
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Save(n.Path, "# Sync-protokoll\n\nNy text.\n", n.Hash)
	if err != nil || res.Conflict || res.Note == nil {
		t.Fatalf("save = %+v, %v", res, err)
	}
	// Only "updated" and the body change; every other byte is kept.
	got := read(t, dir, "Projekt/Plan.md")
	stamp := regexp.MustCompile(`(?m)^updated: .*$`)
	if u := stamp.FindString(got); !strings.Contains(u, "2026-11-22") {
		t.Fatalf("updated not stamped: %q", u)
	}
	want := strings.Replace(plan, "Se [[CLI-kommandon]].", "Ny text.", 1)
	if stamp.ReplaceAllString(got, "U") != stamp.ReplaceAllString(want, "U") {
		t.Fatalf("file =\n%s\nwant (apart from updated)\n%s", got, want)
	}
	want = got
	if res.Note.Hash == n.Hash {
		t.Fatal("hash did not change")
	}
	if c := changed(); !slices.Equal(c, []string{"Projekt/Plan.md"}) {
		t.Fatalf("changed = %v", c)
	}
	// A save based on the old hash is a conflict and writes nothing.
	res2, err := s.Save(n.Path, "stale\n", n.Hash)
	if err != nil || !res2.Conflict || res2.Current == nil || res2.Current.Hash != res.Note.Hash {
		t.Fatalf("stale save = %+v, %v", res2, err)
	}
	if got := read(t, dir, "Projekt/Plan.md"); got != want {
		t.Fatal("a conflicting save wrote the file")
	}
	// An empty base overwrites ("keep mine").
	if res3, err := s.Save(n.Path, "# Sync-protokoll\n\nMin text.\n", ""); err != nil || res3.Conflict {
		t.Fatalf("forced save = %+v, %v", res3, err)
	}
}

func TestPatchCreateMoveDelete(t *testing.T) {
	s, dir, changed := testVault(t, map[string]string{"Projekt/Plan.md": plan, "Fotboll/Träning.md": "# Träning\n"})
	n, _ := s.Get("Projekt/Plan.md")
	res, err := s.Patch(n.Path, PatchRequest{Set: map[string]any{"status": "done", "prio": 2}, Tags: &TagEdit{Add: []string{"viktigt"}, Remove: []string{"sync"}}}, n.Hash)
	if err != nil || res.Conflict {
		t.Fatalf("patch = %+v, %v", res, err)
	}
	got := read(t, dir, "Projekt/Plan.md")
	for _, want := range []string{"status: done\n", "prio: 2\n", `custom: "kept  as is" # comment`, "tags: [nota, viktigt]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("patched file lacks %q:\n%s", want, got)
		}
	}
	created, err := s.Create(CreateRequest{Body: "Smash burgers med kimchi-mayo\n\nGochujang + majonnäs.", Tags: []string{"mat"}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Folder != "Inbox" || created.Title != "Smash burgers med kimchi-mayo" || created.Properties["created"] == nil {
		t.Fatalf("created = %+v", created)
	}
	mv, err := s.Move(created.Path, "Recept")
	if err != nil || mv.Path != "Recept/Smash burgers med kimchi-mayo.md" {
		t.Fatalf("move = %+v, %v", mv, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Inbox", "Smash burgers med kimchi-mayo.md")); !os.IsNotExist(err) {
		t.Fatal("the moved note is still in the inbox")
	}
	in, err := s.Inbox()
	if err != nil || len(in) != 0 {
		t.Fatalf("inbox = %+v, %v", in, err)
	}
	root, err := s.Create(CreateRequest{Title: "Rotnot", Folder: "/"})
	if err != nil || root.Path != "Rotnot.md" {
		t.Fatalf("root note = %+v, %v", root, err)
	}
	trash, err := s.Delete("Fotboll/Träning.md")
	if err != nil || !strings.HasPrefix(trash, ".trash/") {
		t.Fatalf("delete = %q, %v", trash, err)
	}
	c := changed()
	for _, p := range []string{"Projekt/Plan.md", "Recept/Smash burgers med kimchi-mayo.md", "Inbox/Smash burgers med kimchi-mayo.md", "Fotboll/Träning.md"} {
		if !slices.Contains(c, p) {
			t.Fatalf("changed %v lacks %s", c, p)
		}
	}
	if _, err := s.History(t.Context(), "Projekt/Plan.md"); err != ErrNoHistory {
		t.Fatalf("history without sync = %v", err)
	}
}

func TestOutsideEditsReachTheIndex(t *testing.T) {
	s, dir, changed := testVault(t, map[string]string{"Hem/Julklappar.md": "# Julklappar\n"})
	if err := os.WriteFile(filepath.Join(dir, "Hem", "Julklappar.md"), []byte("# Julklappar 2026\n\nKaffekvarn.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// What mygo.WatchFiles would report.
	s.applyFileEvents(watchBatch("Hem/Julklappar.md"))
	n, err := s.Get("Hem/Julklappar.md")
	if err != nil || n.Title != "Julklappar 2026" {
		t.Fatalf("note = %+v, %v", n, err)
	}
	if hits, _ := s.Search("kaffekvarn", ListQuery{}); len(hits) != 1 {
		t.Fatalf("search after outside edit = %+v", hits)
	}
	if c := changed(); !slices.Equal(c, []string{"Hem/Julklappar.md"}) {
		t.Fatalf("changed = %v", c)
	}
	// Nothing new on disk: no batch.
	s.applyFileEvents(watchBatch("Hem/Julklappar.md"))
	if c := changed(); len(c) != 0 {
		t.Fatalf("changed without a change = %v", c)
	}
}

func TestCreateFolderShowsInTree(t *testing.T) {
	s, dir, changed := testVault(t, map[string]string{"Projekt/Plan.md": "# Plan\n"})
	got, err := s.CreateFolder("Projekt/Ideer/2027")
	if err != nil || got != "Projekt/Ideer/2027" {
		t.Fatalf("CreateFolder = %q, %v", got, err)
	}
	if c := changed(); !slices.Equal(c, []string{"Projekt/Ideer/2027"}) {
		t.Fatalf("changed = %v", c)
	}
	if _, err := os.Stat(filepath.Join(dir, "Projekt", "Ideer", "2027")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFolder("projekt"); err == nil {
		t.Fatal("a case-colliding folder was made")
	}
	if _, err := s.CreateFolder("Projekt/Ideer"); err == nil {
		t.Fatal("an existing folder was made again")
	}
	tr, err := s.Tree()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, f := range tr.Folders {
		counts[f.Path] = f.Notes
	}
	want := map[string]int{"Projekt": 1, "Projekt/Ideer": 0, "Projekt/Ideer/2027": 0}
	if !maps.Equal(counts, want) {
		t.Fatalf("folders = %v, want %v", counts, want)
	}
	// Another program makes a folder: the watcher reports it.
	if err := os.Mkdir(filepath.Join(dir, "Arkiv"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileCreate, Path: "Arkiv", IsDir: true}})
	if c := changed(); !slices.Equal(c, []string{"Arkiv"}) {
		t.Fatalf("changed after an outside mkdir = %v", c)
	}
}

func TestPluginsAndTheirChanges(t *testing.T) {
	s, dir, changed := testVault(t, map[string]string{"A.md": "# A\n"})
	if ps, err := s.Plugins(); err != nil || ps == nil || len(ps) != 0 {
		t.Fatalf("no plugins = %v, %v", ps, err)
	}
	p := filepath.Join(dir, ".nota", "plugins", "night")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"contributes":{"themes":[{"id":"ink","dark":{"background":"#000000","text":"#ffffff","ink":"#8be9fd"}}]}}`
	if err := os.WriteFile(filepath.Join(p, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	ps, err := s.Plugins()
	if err != nil || len(ps) != 1 || ps[0].ID != "night" || ps[0].Contributes.Themes[0].ID != "ink" {
		t.Fatalf("plugins = %+v, %v", ps, err)
	}
	// An edit to a manifest tells the frontend, though no note changed.
	s.applyFileEvents([]mygo.FileEvent{
		{Op: mygo.FileCreate, Path: ".nota/plugins/night", IsDir: true},
		{Op: mygo.FileWrite, Path: ".nota/plugins/night/manifest.json"},
	})
	if c := changed(); !slices.Equal(c, []string{".nota/plugins"}) {
		t.Fatalf("changed = %v", c)
	}
}

func watchBatch(paths ...string) []mygo.FileEvent {
	out := make([]mygo.FileEvent, len(paths))
	for i, p := range paths {
		out[i] = mygo.FileEvent{Op: mygo.FileWrite, Path: p}
	}
	return out
}

func TestWatchIgnored(t *testing.T) {
	for _, c := range []struct {
		path string
		dir  bool
		want bool
	}{
		{".git", true, true}, {".trash", true, true}, {"Projekt/node_modules", true, true},
		{".nota", true, false}, {".nota/base", true, true}, {".nota/index.db", false, true}, {".nota/index.db-wal", false, true},
		{".nota/views", true, false}, {".nota/views/utkast.yaml", false, false},
		{".nota/plugins", true, false}, {".nota/plugins/night/manifest.json", false, false},
		{"Projekt", true, false}, {"Projekt/Plan.md", false, false},
	} {
		if got := watchIgnored(c.path, c.dir); got != c.want {
			t.Errorf("watchIgnored(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestRenameAndMoveRewriteLinks(t *testing.T) {
	s, dir, _ := testVault(t, map[string]string{
		"Projekt/Plan.md": "# Plan\n",
		"Länk.md":         "[[Plan]] and [[Projekt/Plan|planen]]\n",
	})
	r, err := s.Rename("Projekt/Plan.md", "Projekt/Roadmap.md")
	if err != nil || r.Links.Count != 2 {
		t.Fatalf("rename %+v %v", r, err)
	}
	if got := read(t, dir, "Länk.md"); got != "[[Roadmap]] and [[Projekt/Roadmap|planen]]\n" {
		t.Fatalf("after rename: %q", got)
	}
	if r, err = s.Move("Projekt/Roadmap.md", "/"); err != nil || r.Path != "Roadmap.md" || r.Links.Count != 1 {
		t.Fatalf("move %+v %v", r, err)
	}
	if r, err = s.RenameFolder("Projekt", "Arbete"); err != nil || r.Links.Count != 0 {
		t.Fatalf("folder %+v %v", r, err)
	}
	if got := read(t, dir, "Länk.md"); got != "[[Roadmap]] and [[Roadmap|planen]]\n" {
		t.Fatalf("after move: %q", got)
	}
}

func TestFindReplaceAndUndo(t *testing.T) {
	s, dir, _ := testVault(t, map[string]string{"Projekt/Plan.md": plan, "Annat.md": "Inget\n"})
	q := ReplaceQuery{Options: replace.Options{Find: "sync", Replace: "Synk"}, Folder: "Projekt"}
	notes, err := s.FindReplace(q)
	if err != nil || len(notes) != 1 || notes[0].Path != "Projekt/Plan.md" {
		t.Fatalf("find = %+v, %v", notes, err)
	}
	before := read(t, dir, "Projekt/Plan.md")
	res, err := s.Replace(q, []replace.Pick{{Path: notes[0].Path, Hash: notes[0].Hash}})
	if err != nil || res.Count != len(notes[0].Matches) || len(res.Stale) != 0 {
		t.Fatalf("replace = %+v, %v", res, err)
	}
	if got := read(t, dir, "Projekt/Plan.md"); !strings.Contains(got, "# Synk-protokoll\n") || !strings.Contains(got, "tags: [nota, sync]") {
		t.Fatalf("after replace:\n%s", got)
	}
	// Saving the old body on the written hash undoes it (apart from updated).
	if r, err := s.Save(res.Notes[0].Path, res.Notes[0].Before, res.Notes[0].Hash); err != nil || r.Conflict {
		t.Fatalf("undo = %+v, %v", r, err)
	}
	stamp := regexp.MustCompile(`(?m)^updated: .*$`)
	if got := read(t, dir, "Projekt/Plan.md"); stamp.ReplaceAllString(got, "U") != stamp.ReplaceAllString(before, "U") {
		t.Fatalf("after undo:\n%s\nwant\n%s", got, before)
	}
}
