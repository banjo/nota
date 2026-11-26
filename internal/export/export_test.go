package export

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/publish"
	"github.com/banjo/nota/internal/vault"
)

// A one-pixel PNG.
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func testVault(t *testing.T) (*vault.Vault, *index.Index) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"Projekt/Plan.md": "---\ntags: [plan]\n---\n# Plan\n\nSee [[Möte#Agenda|the meeting]] and [[Hemligt]] %%not this%%.\n\n" +
			"![[bild.png|300]] and ![logo](../Attachments/logo.png)\n\nA line ^abc123\n\nA note^[inline one].\n\n" +
			"`[[Möte]]` in code, [web](https://example.com) and [[#Mål]].\n\n[report](Rapport.pdf)\n",
		"Projekt/Möte.md":          "# Möte\n\n## Agenda\n\nBack to [the plan](Plan.md).\n",
		"Projekt/Rapport.pdf":      "%PDF-1.4",
		"Projekt/Ark/Gammal.md":    "# Gammal\n\n[[Plan]]\n",
		"Hemligt.md":               "# Hemligt\n",
		"Attachments/logo.png":     string(png),
		"Attachments/bild.png":     string(png),
		"Attachments/oanvänd.png":  string(png),
		"Projekt/attachments/x.md": "# X\n",
	}
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
	ix, err := index.OpenVault(v)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Sync(v); err != nil {
		t.Fatal(err)
	}
	return v, ix
}

func run(t *testing.T, v *vault.Vault, ix *index.Index, target string, o Options) (*Export, []byte) {
	t.Helper()
	e, err := New(v, ix, target, o)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if _, err := e.WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	return e, b.Bytes()
}

func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		out[f.Name] = string(b)
	}
	return out
}

func names(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestNoteHTML(t *testing.T) {
	v, ix := testVault(t)
	e, out := run(t, v, ix, "Projekt/Plan.md", Options{Format: HTML, Theme: &publish.Theme{Mode: "light"}})
	if e.Name != "Plan.html" || !strings.HasPrefix(e.Type, "text/html") {
		t.Errorf("name %q type %q", e.Name, e.Type)
	}
	s := string(out)
	for _, want := range []string{
		`<title>Plan</title>`, `data-mode="light"`,
		`src="data:image/png;base64,`, `width="300"`,
		// Other notes are not part of a one-note export.
		`<span class="unresolved">the meeting</span>`,
		`<a href="#m%C3%A5l"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, not := range []string{"not this", "^abc123", `class="top"`, "Made with Nota", "<script"} {
		if strings.Contains(s, not) {
			t.Errorf("has %q", not)
		}
	}
}

func TestFolderHTML(t *testing.T) {
	v, ix := testVault(t)
	e, out := run(t, v, ix, "Projekt", Options{Format: HTML})
	if e.Name != "Projekt.zip" || e.Notes != 4 {
		t.Errorf("name %q notes %d", e.Name, e.Notes)
	}
	files := unzip(t, out)
	want := []string{"Ark/Gammal.html", "Möte.html", "Plan.html", "attachments/bild.png", "attachments/logo.png", "attachments/x.html"}
	if got := names(files); !slices.Equal(got, want) {
		t.Fatalf("files %q, want %q", got, want)
	}
	plan := files["Plan.html"]
	for _, w := range []string{`href="M%C3%B6te.html#agenda"`, `src="attachments/bild.png"`, `src="attachments/logo.png"`, `<span class="unresolved">Hemligt</span>`} {
		if !strings.Contains(plan, w) {
			t.Errorf("Plan.html: missing %q", w)
		}
	}
	if g := files["Ark/Gammal.html"]; !strings.Contains(g, `href="../Plan.html"`) {
		t.Errorf("Gammal.html links: %s", g)
	}
}

func TestNoteMarkdown(t *testing.T) {
	v, ix := testVault(t)
	e, out := run(t, v, ix, "Projekt/Plan.md", Options{Format: Markdown})
	// It shows images, so they come along in a zip.
	if e.Name != "Plan.zip" {
		t.Fatalf("name %q", e.Name)
	}
	files := unzip(t, out)
	if got, want := names(files), []string{"Plan.md", "Rapport.pdf", "attachments/bild.png", "attachments/logo.png"}; !slices.Equal(got, want) {
		t.Fatalf("files %q, want %q", got, want)
	}
	want := "---\ntags: [plan]\n---\n# Plan\n\nSee the meeting and Hemligt .\n\n" +
		"![](attachments/bild.png) and ![logo](attachments/logo.png)\n\nA line\n\nA note[^inline-1].\n\n" +
		"`[[Möte]]` in code, [web](https://example.com) and [Mål](#mål).\n\n[report](Rapport.pdf)\n\n[^inline-1]: inline one\n"
	if got := files["Plan.md"]; got != want {
		t.Errorf("Plan.md:\n%s\nwant:\n%s", got, want)
	}

	// Without images, a note is one file.
	e, out = run(t, v, ix, "Projekt/Möte.md", Options{Format: Markdown})
	if e.Name != "Möte.md" || string(out) != "# Möte\n\n## Agenda\n\nBack to the plan.\n" {
		t.Errorf("%s: %q", e.Name, out)
	}
}

func TestFolderMarkdown(t *testing.T) {
	v, ix := testVault(t)
	_, out := run(t, v, ix, "Projekt", Options{Format: Markdown})
	files := unzip(t, out)
	if p := files["Plan.md"]; !strings.Contains(p, "See [the meeting](Möte.md#agenda) and Hemligt") {
		t.Errorf("Plan.md: %s", p)
	}
	if m := files["Möte.md"]; !strings.Contains(m, "Back to [the plan](Plan.md).") {
		t.Errorf("Möte.md: %s", m)
	}
	if g := files["Ark/Gammal.md"]; g != "# Gammal\n\n[Plan](../Plan.md)\n" {
		t.Errorf("Gammal.md: %q", g)
	}
}

func TestFiles(t *testing.T) {
	v, ix := testVault(t)
	_, out := run(t, v, ix, "Projekt", Options{Format: Files})
	files := unzip(t, out)
	want := []string{"Attachments/bild.png", "Attachments/logo.png", "Projekt/Ark/Gammal.md", "Projekt/Möte.md", "Projekt/Plan.md", "Projekt/Rapport.pdf", "Projekt/attachments/x.md"}
	if got := names(files); !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}
	if files["Projekt/Plan.md"] == "" || !strings.Contains(files["Projekt/Plan.md"], "[[Möte#Agenda|the meeting]]") {
		t.Error("notes are copied as they are")
	}

	e, out := run(t, v, ix, "", Options{Format: Files, Name: "Notes"})
	if e.Name != "Notes.zip" || len(unzip(t, out)) != 9 {
		t.Errorf("%s: %q", e.Name, names(unzip(t, out)))
	}
}

func TestNotFound(t *testing.T) {
	v, ix := testVault(t)
	for _, p := range []string{"Nope.md", "Nope", "../etc"} {
		if _, err := New(v, ix, p, Options{Format: HTML}); err == nil {
			t.Errorf("%s: no error", p)
		}
	}
	if _, err := New(v, ix, "Hemligt.md", Options{Format: "pdf"}); err == nil {
		t.Error("pdf: no error")
	}
}

func TestRelURL(t *testing.T) {
	for _, c := range [][3]string{
		{"Plan.html", "Möte.html", "Möte.html"},
		{"Ark/Gammal.html", "Plan.html", "../Plan.html"},
		{"a/b/c.md", "a/d/e f.md", "../d/e%20f.md"},
		{"x.md", "attachments/bild 1.png", "attachments/bild%201.png"},
	} {
		if got := relURL(c[0], c[1]); got != c[2] {
			t.Errorf("relURL(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
