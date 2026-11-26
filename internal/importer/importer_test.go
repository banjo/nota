package importer

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/banjo/nota/internal/vault"
)

func testVault(t *testing.T, files map[string]string) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	for p, c := range files {
		f := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := vault.Open(dir, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func read(t *testing.T, v *vault.Vault, p string) string {
	t.Helper()
	b, err := v.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

var mod = time.Date(2024, 5, 6, 7, 8, 9, 0, time.Local)

func source(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for p, c := range files {
		m[p] = &fstest.MapFile{Data: []byte(c), ModTime: mod}
	}
	return m
}

func TestImport(t *testing.T) {
	v := testVault(t, map[string]string{
		"Obsidian/Plan.md":     "# Already here\n",
		"Attachments/bild.png": "old",
	})
	src := source(map[string]string{
		"Plan.md": "---\ndate created: 2023-01-02\nmodified: 2023-02-03 10:00\ntags: [a]\n---\n# Plan\n\n" +
			"[[Möte]], [[Sub/Möte|the meeting]], [[Bad: name?]] and ![[bild.png|200]].\n\n[m](Sub/M%C3%B6te.md)\n",
		"Sub/Möte.md":        "# Möte\n\nBack to [[Plan]] and [the plan](Plan.md) and [up](../Plan.md#Goal).\n",
		"Bad: name?.md":      "No frontmatter.\n",
		"bild.png":           "png",
		"Old.markdown":       "# Old\n",
		".obsidian/app.json": "{}",
		"Sub/.DS_Store":      "x",
		"__MACOSX/._Plan.md": "x",
		"Notes/created.md":   "---\ncreated: tomorrow\n---\nKept.\n",
	})
	files, err := FromFS(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 6 {
		t.Fatalf("files %d, want 6 (hidden and junk left out)", len(files))
	}

	dry, err := Run(v, files, Options{Folder: "Obsidian", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Notes != 5 || dry.Files != 1 || !dry.DryRun || v.FileExists("Obsidian/Möte.md") || v.FileExists("Obsidian/Sub/Möte.md") {
		t.Fatalf("dry run: %+v", dry)
	}

	r, err := Run(v, files, Options{Folder: "Obsidian"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Item{}
	for _, it := range r.Items {
		got[it.Source] = it
	}
	for src, want := range map[string]Item{
		"Plan.md":          {Path: "Obsidian/Plan 2.md", Status: Renamed, Reason: reasonExists},
		"Bad: name?.md":    {Path: "Obsidian/Bad name.md", Status: Renamed, Reason: reasonName},
		"Old.markdown":     {Path: "Obsidian/Old.md", Status: Renamed, Reason: reasonExt},
		"Sub/Möte.md":      {Path: "Obsidian/Sub/Möte.md", Status: Imported},
		"bild.png":         {Path: "Obsidian/bild.png", Status: Imported},
		"Notes/created.md": {Path: "Obsidian/Notes/created.md", Status: Imported},
	} {
		it := got[src]
		if it.Path != want.Path || it.Status != want.Status || it.Reason != want.Reason {
			t.Errorf("%s: %+v, want %+v", src, it, want)
		}
	}
	if r.Notes != 5 || r.Files != 1 || r.Renamed != 3 || r.Folder != "Obsidian" {
		t.Errorf("report %+v", r)
	}

	plan := read(t, v, "Obsidian/Plan 2.md")
	for _, want := range []string{
		"created: 2023-01-02T00:00:00", "updated: 2023-02-03T10:00:00", "tags: [a]",
		"[[Möte]], [[Sub/Möte|the meeting]], [[Bad name|Bad: name?]] and ![[bild.png|200]].",
		"[m](Sub/M%C3%B6te.md)",
	} {
		if !strings.Contains(plan, want) {
			t.Errorf("Plan 2.md lacks %q:\n%s", want, plan)
		}
	}
	if strings.Contains(plan, "date created") || strings.Contains(plan, "modified:") {
		t.Errorf("Plan 2.md keeps the other app's keys:\n%s", plan)
	}
	mote := read(t, v, "Obsidian/Sub/Möte.md")
	if !strings.Contains(mote, "Back to [[Plan 2|Plan]] and [the plan](../Plan%202.md) and [up](../Plan%202.md#Goal).") {
		t.Errorf("Möte.md links:\n%s", mote)
	}
	stamp := mod.Format(time.RFC3339)
	if bad := read(t, v, "Obsidian/Bad name.md"); bad != "---\ncreated: "+stamp+"\nupdated: "+stamp+"\n---\nNo frontmatter.\n" {
		t.Errorf("Bad name.md:\n%s", bad)
	}
	if c := read(t, v, "Obsidian/Notes/created.md"); !strings.Contains(c, "created: tomorrow") || !strings.Contains(c, "updated: "+stamp) {
		t.Errorf("created.md:\n%s", c)
	}
	if read(t, v, "Obsidian/Plan.md") != "# Already here\n" || read(t, v, "Attachments/bild.png") != "old" {
		t.Error("an existing file changed")
	}

	// Again: every name is taken now, nothing is replaced.
	r2, err := Run(v, files, Options{Folder: "Obsidian"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Notes != 5 || r2.Renamed != 6 || !v.FileExists("Obsidian/Sub/Möte 2.md") {
		t.Errorf("second import %+v", r2)
	}
	if m := read(t, v, "Obsidian/Sub/Möte 2.md"); !strings.Contains(m, "[[Plan 3|Plan]]") {
		t.Errorf("second Möte:\n%s", m)
	}
}

func TestImportFolderCase(t *testing.T) {
	v := testVault(t, map[string]string{"notes/a.md": "a"})
	r, err := Run(v, []File{{Path: "b.md", Read: func() ([]byte, error) { return []byte("b"), nil }}}, Options{Folder: "Notes"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Folder != "notes" || r.Items[0].Path != "notes/b.md" {
		t.Errorf("report %+v", r)
	}
}

func TestDates(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"# A\n", "---\ncreated: 2024-05-06T07:08:09+02:00\nupdated: 2024-05-06T07:08:09+02:00\n---\n# A\n"},
		{"---\nCreated: May 1, 2023 3:04 PM\nLast edited time: May 2, 2023\n---\nB\n", "---\ncreated: 2023-05-01T15:04:00+02:00\nupdated: 2023-05-02T00:00:00+02:00\n---\nB\n"},
		{"---\ndate: 2020-01-01\n---\nC\n", "---\ndate: 2020-01-01\ncreated: 2024-05-06T07:08:09+02:00\nupdated: 2024-05-06T07:08:09+02:00\n---\nC\n"},
		{"---\n: bad: [\n---\nD\n", "---\n: bad: [\n---\nD\n"},
	} {
		in := time.Date(2024, 5, 6, 7, 8, 9, 0, time.FixedZone("CEST", 2*3600))
		loc := time.Local
		time.Local = time.FixedZone("CEST", 2*3600)
		got := string(dates([]byte(c.in), time.Time{}, in))
		time.Local = loc
		if got != c.want {
			t.Errorf("dates(%q):\n%s\nwant:\n%s", c.in, got, c.want)
		}
	}
}

func TestFromZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range []string{"Export-1/Plan.md", "Export-1/Sub/Möte.md", "__MACOSX/Export-1/._Plan.md", "Export-1/.DS_Store"} {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Deflate, Modified: time.Date(2024, 1, 2, 3, 4, 6, 0, time.UTC)})
		w.Write([]byte("# " + n))
	}
	zw.Close()
	files, root, err := FromZip(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if root != "Export-1" || len(files) != 2 || files[0].Path != "Plan.md" || files[1].Path != "Sub/Möte.md" {
		t.Fatalf("root %q files %+v", root, files)
	}
	if b, _ := files[1].Read(); string(b) != "# Export-1/Sub/Möte.md" {
		t.Errorf("read %q", b)
	}
	if files[0].Modified.IsZero() {
		t.Error("no modification time")
	}
}

// Notion's export: a page id in every name, links by encoded path.
func TestImportNotion(t *testing.T) {
	v := testVault(t, nil)
	id1, id2 := "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	files, err := FromFS(source(map[string]string{
		"Projekt " + id1 + ".md":                  "# Projekt\n\n[Möte](Projekt%20" + id1 + "/M%C3%B6te%20" + id2 + ".md)\n",
		"Projekt " + id1 + "/Möte " + id2 + ".md": "# Möte\n\nCreated: x\n",
	}), false)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Run(v, files, Options{Folder: "Notion"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Renamed != 2 || r.Items[0].Reason != reasonNotion {
		t.Errorf("report %+v", r)
	}
	if p := read(t, v, "Notion/Projekt.md"); !strings.Contains(p, "[Möte](Projekt/M%C3%B6te.md)") && !strings.Contains(p, "[Möte](Projekt/Möte.md)") {
		t.Errorf("Projekt.md:\n%s", p)
	}
	if !v.FileExists("Notion/Projekt/Möte.md") {
		t.Error("no Möte")
	}
}

func TestFromFSLogseq(t *testing.T) {
	files, err := FromFS(source(map[string]string{
		"pages/Plan.md":              "- a",
		"journals/2024_01_02.md":     "- b",
		"logseq/config.edn":          "{}",
		"logseq/bak/pages/Plan/1.md": "- old",
	}), false)
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %+v", err, files)
	}
}
