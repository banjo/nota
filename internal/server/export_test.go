package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/banjo/nota/internal/protocol"
)

func (b *browser) export(req ExportRequest) (int, http.Header, []byte) {
	b.t.Helper()
	data, _ := json.Marshal(req)
	r, _ := http.NewRequest("POST", b.e.ts.URL+api+"/export", bytes.NewReader(data))
	r.Header.Set("Nota-Protocol", "1")
	r.Header.Set(HeaderClient, "web")
	r.Header.Set("Origin", b.origin)
	r.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(r)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, body
}

func TestExport(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")
	for p, c := range map[string]string{
		"Projekt/Plan.md":      "# Plan\n\nSe [[Mål]] och [[Privat]].\n\n![[bild.png]]\n",
		"Projekt/Mål.md":       "# Mål\n",
		"Privat.md":            "# Privat\n",
		"Attachments/bild.png": "\x89PNG bild",
	} {
		if st, _, perr := e.put(tok, p, 0, c); st != 200 {
			t.Fatalf("put %s: %d %v", p, st, perr)
		}
	}

	st, h, body := b.export(ExportRequest{Path: "Projekt/Plan.md", Format: "html", Theme: &protocol.ShareTheme{Mode: "dark"}})
	if st != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/html") ||
		h.Get("Content-Disposition") != `attachment; filename=Plan.html` {
		t.Fatalf("html: %d %v", st, h)
	}
	for _, want := range []string{`data-mode="dark"`, `src="data:image/png;base64,`, `<span class="unresolved">Mål</span>`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("html: missing %q", want)
		}
	}

	st, h, body = b.export(ExportRequest{Path: "Projekt", Format: "md"})
	if st != 200 || h.Get("Content-Type") != "application/zip" || h.Get("Content-Disposition") != `attachment; filename=Projekt.zip` {
		t.Fatalf("folder: %d %v", st, h)
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "Mål.md,Plan.md,attachments/bild.png" {
		t.Errorf("folder zip: %q", names)
	}

	// The whole vault is named after it; non-ASCII names are encoded.
	if st, h, _ = b.export(ExportRequest{Format: "zip"}); st != 200 || h.Get("Content-Disposition") != `attachment; filename=main.zip` {
		t.Errorf("vault: %d %v", st, h)
	}
	if st, h, _ = b.export(ExportRequest{Path: "Projekt/Mål.md", Format: "md"}); st != 200 || !strings.Contains(h.Get("Content-Disposition"), "filename*=utf-8''M%C3%A5l.md") {
		t.Errorf("utf-8 name: %d %v", st, h)
	}

	for _, c := range []struct {
		req  ExportRequest
		want int
	}{
		{ExportRequest{Path: "Nope.md", Format: "html"}, 404},
		{ExportRequest{Path: "Projekt/Plan.md", Format: "pdf"}, 400},
		{ExportRequest{Path: "Projekt/Plan.md", Format: "html", Theme: &protocol.ShareTheme{Tokens: map[string]string{"ink": "url(x)"}}}, 400},
	} {
		if st, _, body := b.export(c.req); st != c.want {
			t.Errorf("%+v: %d %s", c.req, st, body)
		}
	}
}
