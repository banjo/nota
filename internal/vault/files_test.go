package vault

import (
	"errors"
	"testing"

	"github.com/banjo/nota/internal/events"
)

func TestFileMethods(t *testing.T) {
	m := NewMemFS(map[string]string{"Mo\u0308te.md": "nfd\n", "img/a.png": "png"})
	bus := events.New()
	var got []events.Type
	bus.Subscribe(func(e events.Event) { got = append(got, e.Type) })
	v, err := OpenFS(m, "mem", Options{Bus: bus})
	if err != nil {
		t.Fatal(err)
	}
	var walked []string
	v.WalkFiles(func(e Entry) error { walked = append(walked, e.Path); return nil })
	if len(walked) != 2 || walked[0] != "Möte.md" || walked[1] != "img/a.png" {
		t.Fatalf("WalkFiles = %q", walked)
	}
	if data, err := v.ReadFile("Möte.md"); err != nil || string(data) != "nfd\n" {
		t.Fatalf("ReadFile NFC of an NFD name: %q %v", data, err)
	}
	// Attachments: raw writes, no events; notes keep their events.
	if _, created, err := v.WriteFile("img/b.png", []byte("b")); err != nil || !created {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, err := v.WriteFile("IMG/c.png", []byte("c")); !errors.Is(err, ErrCaseCollision) {
		t.Fatalf("case collision: %v", err)
	}
	if _, _, err := v.WriteFile("Möte.md", []byte("nfc\n")); err != nil || m.m["Mo\u0308te.md"] == nil || string(m.m["Mo\u0308te.md"].Data) != "nfc\n" {
		t.Fatalf("WriteFile into the NFD file: %v", err)
	}
	if _, _, err := v.WriteFile(".nota/x", nil); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("ignored path: %v", err)
	}
	if p, err := v.FreeName("img/a.png"); err != nil || p != "img/a 2.png" {
		t.Fatalf("FreeName = %q %v", p, err)
	}
	if p, err := v.FreeName("new/x.md"); err != nil || p != "new/x.md" {
		t.Fatalf("FreeName in a new folder = %q %v", p, err)
	}
	if _, err := v.RenameFile("img/b.png", "img/d.png"); err != nil || !v.FileExists("img/d.png") {
		t.Fatalf("RenameFile: %v", err)
	}
	trash, err := v.DeleteFile("img/a.png")
	if err != nil || trash != ".trash/img/a.png" || v.FileExists("img/a.png") {
		t.Fatalf("DeleteFile = %q %v", trash, err)
	}
	if len(got) != 1 || got[0] != events.NoteSaved {
		t.Fatalf("events %v, want only note.saved", got)
	}
}
