package vault

import (
	"errors"
	"testing"
)

func TestImageType(t *testing.T) {
	for name, want := range map[string]string{
		"a.png": "image/png", "B.JPG": "image/jpeg", "c.svg": "image/svg+xml",
		"d.md": "", "e.pdf": "", "png": "",
	} {
		if got := ImageType(name); got != want {
			t.Errorf("ImageType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSaveAttachment(t *testing.T) {
	v, err := OpenFS(NewMemFS(map[string]string{"Attachments/a.png": "old"}), "mem", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := v.SaveAttachment("b.png", []byte("b")); err != nil || p != "Attachments/b.png" {
		t.Fatalf("new name: %q %v", p, err)
	}
	if p, err := v.SaveAttachment("../../x/a.png", []byte("a")); err != nil || p != "Attachments/a 2.png" {
		t.Fatalf("taken name, path dropped: %q %v", p, err)
	}
	if data, _ := v.ReadFile("Attachments/a.png"); string(data) != "old" {
		t.Fatalf("overwrote: %q", data)
	}
	for _, name := range []string{"x.md", "x.exe", "", ".png", "dir/"} {
		if _, err := v.SaveAttachment(name, nil); err == nil {
			t.Errorf("SaveAttachment(%q) succeeded", name)
		}
	}
	if _, err := v.SaveAttachment("x.md", nil); !errors.Is(err, ErrNotImage) {
		t.Errorf("x.md: %v", err)
	}

	// Obsidian's lower-case folder is used rather than a second one.
	v, _ = OpenFS(NewMemFS(map[string]string{"attachments/a.png": "a"}), "mem", Options{})
	if p, err := v.SaveAttachment("b.png", nil); err != nil || p != "attachments/b.png" {
		t.Fatalf("other case: %q %v", p, err)
	}
}

func TestFindFile(t *testing.T) {
	v, err := OpenFS(NewMemFS(map[string]string{
		"Projekt/Plan.md":         "",
		"Projekt/skiss.png":       "",
		"Attachments/logo.png":    "",
		"Arkiv/2024/bild.png":     "",
		"Arkiv/bild.png":          "",
		"djup/a/b/unik.jpg":       "",
		".nota/x.png":             "",
		".trash/borta.png":        "",
		"Attachments/Möte 1.webp": "",
	}), "mem", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ target, from, want string }{
		{"Projekt/skiss.png", "", "Projekt/skiss.png"},
		{"skiss.png", "Projekt/Plan.md", "Projekt/skiss.png"},
		{"logo.png", "Projekt/Plan.md", "Attachments/logo.png"},
		{"bild.png", "Projekt/Plan.md", "Arkiv/bild.png"},
		{"2024/bild.png", "", "Arkiv/2024/bild.png"},
		{"UNIK.JPG", "", "djup/a/b/unik.jpg"},
		{"Mo\u0308te 1.webp", "", "Attachments/Möte 1.webp"},
	} {
		if got, err := v.FindFile(c.target, c.from); err != nil || got != c.want {
			t.Errorf("FindFile(%q, %q) = %q, %v; want %q", c.target, c.from, got, err, c.want)
		}
	}
	for _, target := range []string{"saknas.png", "x.png", "borta.png", "../Projekt/skiss.png", ""} {
		if got, err := v.FindFile(target, "Projekt/Plan.md"); !errors.Is(err, ErrNotFound) {
			t.Errorf("FindFile(%q) = %q, %v; want not found", target, got, err)
		}
	}
	if !v.HasFile("Projekt/skiss.png") || v.HasFile(".nota/x.png") || v.HasFile("Projekt") || v.HasFile("../x") {
		t.Error("HasFile")
	}
}
