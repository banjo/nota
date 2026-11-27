package attachments

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
)

func setup(t *testing.T, files map[string]string) (*vault.Vault, *index.Index) {
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
	ix, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Sync(v); err != nil {
		t.Fatal(err)
	}
	return v, ix
}

func TestLoad(t *testing.T) {
	v, ix := setup(t, map[string]string{
		"attachments/skiss.png":          "png",
		"attachments/Pasted image 1.png": "png",
		"attachments/old/unused.jpg":     "jpg",
		"attachments/doc.pdf":            "pdf",
		"attachments/Readme.md":          "# a note, left out",
		"Projekt/skiss.png":              "another one, closer to Projekt/Plan",
		"Projekt/Plan.md":                "![[skiss.png]] ![](../attachments/Pasted%20image%201.png) [pdf](/attachments/doc.pdf)\n",
		"Möte.md":                        "![[skiss.png|300]] and again ![[attachments/skiss.png]]\n",
	})
	os.Chtimes(filepath.Join(v.Root(), "attachments/doc.pdf"), time.Now(), time.Now().Add(time.Hour))

	l, err := Load(v, ix)
	if err != nil {
		t.Fatal(err)
	}
	if l.Folder != "attachments" || len(l.Files) != 4 {
		t.Fatalf("list %+v", l)
	}
	if l.Files[0].Path != "attachments/doc.pdf" || l.Files[0].Image {
		t.Errorf("newest first, a pdf is no image: %+v", l.Files[0])
	}
	used := map[string][]string{}
	for _, f := range l.Files {
		used[f.Path] = f.UsedBy
	}
	for p, want := range map[string][]string{
		"attachments/skiss.png":          {"Möte.md"},
		"attachments/Pasted image 1.png": {"Projekt/Plan.md"},
		"attachments/doc.pdf":            {"Projekt/Plan.md"},
		"attachments/old/unused.jpg":     {},
	} {
		if got := used[p]; len(got) != len(want) || (len(want) > 0 && got[0] != want[0]) {
			t.Errorf("%s used by %v, want %v", p, got, want)
		}
	}
}

func TestLoadNoFolder(t *testing.T) {
	v, ix := setup(t, map[string]string{"a.md": "a"})
	l, err := Load(v, ix)
	if err != nil || l.Folder != "" || len(l.Files) != 0 {
		t.Fatalf("%+v %v", l, err)
	}
}
