package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func scheme(t *testing.T, s *Vault, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	files{s}.ServeHTTP(w, httptest.NewRequest(method, url, strings.NewReader(body)))
	return w
}

func TestFileScheme(t *testing.T) {
	s, dir, changed := testVault(t, map[string]string{
		"Projekt/Plan.md":        "# Plan\n",
		"Projekt/skiss.png":      "PNG",
		"Attachments/Möte 1.svg": "<svg/>",
		".nota/secret.png":       "no",
		"Projekt/page.html":      "<script>",
	})
	// A vault path, as ![](…) resolves it.
	w := scheme(t, s, "GET", "notafile://vault/Projekt/skiss.png", "")
	if w.Code != 200 || w.Body.String() != "PNG" || w.Header().Get("Content-Type") != "image/png" ||
		w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("image: %d %q %v", w.Code, w.Body, w.Header())
	}
	// An embed's target, found from its note.
	if w := scheme(t, s, "GET", "notafile://vault/M%C3%B6te%201.svg?from=Projekt/Plan.md", ""); w.Code != 200 || w.Body.String() != "<svg/>" || w.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("by name: %d %q", w.Code, w.Body)
	}
	// Nothing else: not Nota's own files, not other types, not outside.
	for _, u := range []string{
		"notafile://vault/.nota/secret.png",
		"notafile://vault/Projekt/page.html",
		"notafile://vault/Projekt/Plan.md",
		"notafile://vault/../x.png",
		"notafile://vault/saknas.png",
		"notafile://other/Projekt/skiss.png",
	} {
		if w := scheme(t, s, "GET", u, ""); w.Code != 404 {
			t.Errorf("%s: %d", u, w.Code)
		}
	}

	// A pasted image: saved under a free name, and the tree hears of it.
	w = scheme(t, s, "POST", "notafile://attach/skiss.png", "NEW")
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"path":"Attachments/skiss.png"`) {
		t.Fatalf("attach: %d %q", w.Code, w.Body)
	}
	if got := read(t, dir, "Attachments/skiss.png"); got != "NEW" {
		t.Fatalf("saved %q", got)
	}
	if c := changed(); !slices.Equal(c, []string{"Attachments/skiss.png"}) {
		t.Fatalf("changed = %v", c)
	}
	if w := scheme(t, s, "POST", "notafile://attach/x.exe", "MZ"); w.Code != http.StatusBadRequest {
		t.Fatalf("not an image: %d", w.Code)
	}

	// An image changed by another program (or sync) reloads in notes.
	s.applyFileEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "Projekt/skiss.png"}, {Op: mygo.FileWrite, Path: "Projekt/page.html"}})
	if c := changed(); !slices.Equal(c, []string{"Projekt/skiss.png"}) {
		t.Fatalf("changed after an outside edit = %v", c)
	}
}
