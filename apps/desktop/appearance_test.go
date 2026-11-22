package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppearanceNormalize(t *testing.T) {
	a := Appearance{Mode: "sepia", Resolved: "dim", Background: "red", CaptureBackground: "#26262A"}.normalize()
	want := Appearance{Mode: "system", CaptureBackground: "#26262A"}
	if a != want {
		t.Fatalf("normalize = %+v, want %+v", a, want)
	}
}

func TestAppearanceBackgrounds(t *testing.T) {
	for _, tc := range []struct {
		a             Appearance
		main, capture string
	}{
		{Appearance{Mode: "system"}, "light-dark(#ffffff, #1c1c1e)", "light-dark(#ffffff, #26262a)"},
		{Appearance{Mode: "light"}, "#ffffff", "#ffffff"},
		{Appearance{Mode: "dark"}, "#1c1c1e", "#26262a"},
		{Appearance{Mode: "dark", Resolved: "dark", Background: "#221e1a", CaptureBackground: "#2c2722"}, "#221e1a", "#2c2722"},
		{Appearance{Mode: "light", Resolved: "light", Background: "#f8f4ec"}, "#f8f4ec", "#ffffff"},
		// System keeps following the OS: the page's colour fills only the
		// side it was read in.
		{Appearance{Mode: "system", Resolved: "light", Background: "#f8f4ec", CaptureBackground: "#fbf8f2"}, "light-dark(#f8f4ec, #1c1c1e)", "light-dark(#fbf8f2, #26262a)"},
		{Appearance{Mode: "system", Resolved: "dark", Background: "#221e1a"}, "light-dark(#ffffff, #221e1a)", "light-dark(#ffffff, #26262a)"},
	} {
		main, capture := tc.a.backgrounds()
		if main != tc.main || capture != tc.capture {
			t.Errorf("%+v: backgrounds = %q, %q; want %q, %q", tc.a, main, capture, tc.main, tc.capture)
		}
	}
}

func TestAppearanceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nota", "appearance.json")
	if got := loadAppearance(path); got != (Appearance{Mode: "system"}) {
		t.Fatalf("missing file = %+v, want System", got)
	}
	a := Appearance{Mode: "dark", Resolved: "dark", Background: "#171718", CaptureBackground: "#222224"}
	if err := saveAppearance(path, a); err != nil {
		t.Fatal(err)
	}
	if got := loadAppearance(path); got != a {
		t.Fatalf("load = %+v, want %+v", got, a)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadAppearance(path); got.Mode != "system" {
		t.Fatalf("broken file = %+v, want System", got)
	}
}

func TestSetAppearanceSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appearance.json")
	d := &Desktop{appearanceFile: path}
	// No windows and no running app: only the save path runs.
	if err := d.SetAppearance(Appearance{Mode: "light", Resolved: "light", Background: "#f8f4ec"}); err != nil {
		t.Fatal(err)
	}
	if got := loadAppearance(path); got.Mode != "light" || got.Background != "#f8f4ec" {
		t.Fatalf("saved %+v", got)
	}
}
