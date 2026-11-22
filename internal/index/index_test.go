package index

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/banjo/nota/internal/vault"
)

var fixture = map[string]string{
	"Inbox/Idé.md":           "Idé om #nota/sync\n\nLänk till [[Möte med Anna]] och [[Plan|planen]].\n",
	"Projekt/Plan.md":        "---\ntags: [projekt, nota]\nstatus: draft\npriority: 2\naliases: [Planen]\n---\n# Plan\n\nSee [[Inbox/Idé]] and ![[diagram.png]].\n",
	"Projekt/Arkiv/Plan.md":  "---\nstatus: done\n---\n# Gammal plan\n",
	"Möten/Möte med Anna.md": "---\ntags: projekt, möte\nstatus: draft\nupdated: 2026-11-20\n---\n# Möte med Anna\n\nVi pratade om [[planen]] och [relativ](../Projekt/Plan.md).\n```\n[[Inte en länk]] #intetag\n```\n",
	"Rot.md":                 "Rot note mentioning sync engine and kaffe.\n",
	".obsidian/x.md":         "ignored",
	"broken.md":              "---\ntags: [unclosed\n---\nBroken yaml still indexed\n",
}

func setup(t *testing.T) (*vault.Vault, *Index) {
	t.Helper()
	dir := t.TempDir()
	for p, c := range fixture {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := vault.Open(dir, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ix, err := OpenVault(v)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	st, err := ix.Sync(v)
	if err != nil {
		t.Fatal(err)
	}
	if st.Added != 6 || st.Notes != 6 {
		t.Fatalf("first sync: %+v", st)
	}
	return v, ix
}

func paths(es []Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.Path)
	}
	return out
}

func query(t *testing.T, ix *Index, q Query) []string {
	t.Helper()
	es, err := ix.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	return paths(es)
}

func TestQueries(t *testing.T) {
	_, ix := setup(t)
	cases := []struct {
		name string
		q    Query
		want []string
	}{
		{"folder recursive", Query{Folder: "Projekt"}, []string{"Projekt/Arkiv/Plan.md", "Projekt/Plan.md"}},
		{"folder no prefix match", Query{Folder: "Proj"}, []string{}},
		{"tag frontmatter list+string", Query{Tag: "projekt"}, []string{"Möten/Möte med Anna.md", "Projekt/Plan.md"}},
		{"nested parent tag", Query{Tag: "#nota"}, []string{"Inbox/Idé.md", "Projekt/Plan.md"}},
		{"nested exact tag", Query{Tag: "NOTA/sync"}, []string{"Inbox/Idé.md"}},
		{"code blocks have no tags", Query{Tag: "intetag"}, []string{}},
		{"where", Query{Where: []Cond{{Key: "status", Value: "Draft"}}}, []string{"Möten/Möte med Anna.md", "Projekt/Plan.md"}},
		{"where number", Query{Where: []Cond{{Key: "priority", Value: "2"}}}, []string{"Projekt/Plan.md"}},
		{"where list item", Query{Where: []Cond{{Key: "tags", Value: "nota"}}}, []string{"Projekt/Plan.md"}},
		{"where not", Query{Folder: "Projekt", Where: []Cond{{Key: "status", Value: "draft", Not: true}}}, []string{"Projekt/Arkiv/Plan.md"}},
		{"sort title desc", Query{Where: []Cond{{Key: "status", Value: "draft"}}, Sort: Sort{Field: "title", Desc: true}}, []string{"Projekt/Plan.md", "Möten/Möte med Anna.md"}},
		{"limit", Query{Limit: 2}, []string{"Inbox/Idé.md", "Möten/Möte med Anna.md"}},
	}
	for _, c := range cases {
		if got := query(t, ix, c.q); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	e, err := ix.Get("Projekt/Plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "Plan" || !slices.Equal(e.Tags, []string{"projekt", "nota"}) || e.Props["status"] != "draft" {
		t.Fatalf("entry %+v", e)
	}
	m, _ := ix.Get("Möten/Möte med Anna.md")
	if got := m.Updated.Format(time.DateOnly); got != "2026-11-20" {
		t.Fatalf("updated from frontmatter: %s", got)
	}
	b, err := ix.Get("broken.md")
	if err != nil || b.Title != "Broken yaml still indexed" || len(b.Props) != 0 {
		t.Fatalf("broken: %+v %v", b, err)
	}
}

func TestSearch(t *testing.T) {
	_, ix := setup(t)
	cases := map[string][]string{
		"sync":             {"Inbox/Idé.md", "Rot.md"},
		"möte anna":        {"Inbox/Idé.md", "Möten/Möte med Anna.md"},
		"pla":              {"Inbox/Idé.md", "Möten/Möte med Anna.md", "Projekt/Arkiv/Plan.md", "Projekt/Plan.md"},
		`"sync engine"`:    {"Rot.md"},
		"kaffe OR gammal":  {"Projekt/Arkiv/Plan.md", "Rot.md"},
		"plan -gammal":     {"Inbox/Idé.md", "Möten/Möte med Anna.md", "Projekt/Plan.md"},
		"a:b c-d \"quote":  {},
		"Idé":              {"Inbox/Idé.md", "Projekt/Plan.md"},
		"   gammal   plan": {"Projekt/Arkiv/Plan.md"},
	}
	for q, want := range cases {
		es, err := ix.Search(q, Query{})
		if err != nil {
			t.Errorf("Search(%q): %v", q, err)
			continue
		}
		got := paths(es)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("Search(%q) = %q, want %q", q, got, want)
		}
	}
	// Title matches rank above body matches.
	if es, _ := ix.Search("möte", Query{}); len(es) != 2 || es[0].Path != "Möten/Möte med Anna.md" {
		t.Errorf("ranking: %q", paths(es))
	}
	if _, err := ix.Search("  -x ", Query{}); err == nil {
		t.Error("only negative terms should fail")
	}
	es, _ := ix.Search("plan", Query{Folder: "Projekt/Arkiv"})
	if got := paths(es); !slices.Equal(got, []string{"Projekt/Arkiv/Plan.md"}) {
		t.Errorf("filtered search: %q", got)
	}
	if es, _ := ix.Search("kaffe", Query{}); len(es) != 1 || es[0].Snippet == "" {
		t.Errorf("snippet missing: %+v", es)
	}
}

func TestLinksAndBacklinks(t *testing.T) {
	_, ix := setup(t)
	links, err := ix.Links("Inbox/Idé.md")
	if err != nil {
		t.Fatal(err)
	}
	var to []string
	for _, l := range links {
		to = append(to, l.Target+"->"+l.To)
	}
	// "Plan" resolves by file name; two match, the shorter path wins.
	want := []string{"Möte med Anna->Möten/Möte med Anna.md", "Plan->Projekt/Plan.md"}
	if !slices.Equal(to, want) {
		t.Fatalf("links %q, want %q", to, want)
	}
	back, err := ix.Backlinks("Projekt/Plan.md")
	if err != nil {
		t.Fatal(err)
	}
	var from []string
	for _, l := range back {
		from = append(from, l.From+":"+l.Target)
	}
	want = []string{"Inbox/Idé.md:Plan", "Möten/Möte med Anna.md:planen", "Möten/Möte med Anna.md:Projekt/Plan.md"}
	if !slices.Equal(from, want) {
		t.Fatalf("backlinks %q, want %q", from, want)
	}
	if back, _ := ix.Backlinks("Inbox/Idé.md"); len(back) != 1 || back[0].From != "Projekt/Plan.md" {
		t.Fatalf("backlinks by path: %+v", back)
	}
	if p, _ := ix.Resolve("Gammal plan", "Rot.md"); p != "Projekt/Arkiv/Plan.md" {
		t.Fatalf("resolve by title: %q", p)
	}
	if p, _ := ix.Resolve("Arkiv/Plan", "Rot.md"); p != "Projekt/Arkiv/Plan.md" {
		t.Fatalf("resolve by path suffix: %q", p)
	}
	if p, _ := ix.Resolve("Plan", "Projekt/Arkiv/Other.md"); p != "Projekt/Arkiv/Plan.md" {
		t.Fatalf("same folder wins: %q", p)
	}
	if p, _ := ix.Resolve("nowhere", "Rot.md"); p != "" {
		t.Fatalf("unresolved: %q", p)
	}
}

func TestLookup(t *testing.T) {
	_, ix := setup(t)
	cases := map[string][]string{
		"Projekt/Plan.md":                 {"Projekt/Plan.md"},
		"projekt/plan":                    {"Projekt/Plan.md"},
		"plan":                            {"Projekt/Arkiv/Plan.md", "Projekt/Plan.md"},
		"gammal plan":                     {"Projekt/Arkiv/Plan.md"},
		"möte med":                        {"Möten/Möte med Anna.md"},
		"Mo\u0308ten/Mo\u0308te med Anna": {"Möten/Möte med Anna.md"},
		"Ro":                              {"Rot.md"},
		"zzz":                             nil,
	}
	for ref, want := range cases {
		got, err := ix.Lookup(ref)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Lookup(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
}

func TestIncrementalSync(t *testing.T) {
	v, ix := setup(t)
	root := v.Root()
	st, err := ix.Sync(v)
	if err != nil || st.Changed() || st.Unchanged != 6 {
		t.Fatalf("no-op sync: %+v %v", st, err)
	}
	// Same content, new mtime: only touched.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "Rot.md"), future, future); err != nil {
		t.Fatal(err)
	}
	if st, _ = ix.Sync(v); st.Changed() {
		t.Fatalf("touch counted as change: %+v", st)
	}
	if err := os.WriteFile(filepath.Join(root, "Rot.md"), []byte("Rot\n#ny tag\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "broken.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Ny.md"), []byte("Ny"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = ix.Sync(v)
	if err != nil || st.Added != 1 || st.Updated != 1 || st.Removed != 1 || st.Notes != 6 {
		t.Fatalf("sync after edits: %+v %v", st, err)
	}
	if got := query(t, ix, Query{Tag: "ny"}); !slices.Equal(got, []string{"Rot.md"}) {
		t.Fatalf("tag after update: %q", got)
	}
	if es, _ := ix.Search("kaffe", Query{}); len(es) != 0 {
		t.Fatal("old text still searchable")
	}
	// Update and Remove for single notes.
	if _, _, err := v.Write("Ny.md", []byte("Ny #single")); err != nil {
		t.Fatal(err)
	}
	if err := ix.Update(v, "Ny.md"); err != nil {
		t.Fatal(err)
	}
	if got := query(t, ix, Query{Tag: "single"}); !slices.Equal(got, []string{"Ny.md"}) {
		t.Fatalf("after Update: %q", got)
	}
	if _, err := v.Delete("Ny.md"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Update(v, "Ny.md"); err != nil {
		t.Fatal(err)
	}
	if n, _ := ix.Count(); n != 5 {
		t.Fatalf("count after delete: %d", n)
	}
	st, err = ix.Rebuild(v)
	if err != nil || st.Added != 5 || st.Notes != 5 {
		t.Fatalf("rebuild: %+v %v", st, err)
	}
	var tables int
	ix.DB().QueryRow(`SELECT count(*) FROM tags`).Scan(&tables)
	if tables == 0 {
		t.Fatal("rebuild left no tags")
	}
}

func TestSchemaVersionMismatchRebuilds(t *testing.T) {
	v, ix := setup(t)
	if _, err := ix.DB().Exec(`UPDATE schema_version SET version = 0`); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	ix2, err := OpenVault(v)
	if err != nil {
		t.Fatal(err)
	}
	defer ix2.Close()
	if !ix2.Recreated {
		t.Fatal("stale index not recreated")
	}
	if n, _ := ix2.Count(); n != 0 {
		t.Fatalf("stale rows kept: %d", n)
	}
	if st, err := ix2.Sync(v); err != nil || st.Added != 6 {
		t.Fatalf("resync: %+v %v", st, err)
	}
	// Garbage in place of the database is replaced as well.
	ix2.Close()
	if err := os.WriteFile(Path(v), []byte("not a database, just text that is long enough to have a header......"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Remove(Path(v) + "-wal")
	os.Remove(Path(v) + "-shm")
	ix3, err := OpenVault(v)
	if err != nil {
		t.Fatalf("corrupt index: %v", err)
	}
	ix3.Close()
}

func TestParseHelpers(t *testing.T) {
	if _, err := ParseSort("updated sideways"); err == nil {
		t.Error("bad direction accepted")
	}
	if s, _ := ParseSort("Updated DESC"); !reflect.DeepEqual(s, Sort{Field: "updated", Desc: true}) {
		t.Errorf("sort %+v", s)
	}
	if c, _ := ParseCond("status!=done"); !c.Not || c.Key != "status" || c.Value != "done" {
		t.Errorf("cond %+v", c)
	}
	if c, _ := ParseCond("url=https://x?a=b"); c.Key != "url" || c.Value != "https://x?a=b" {
		t.Errorf("cond %+v", c)
	}
	if _, err := ParseCond("=x"); err == nil {
		t.Error("empty key accepted")
	}
}
