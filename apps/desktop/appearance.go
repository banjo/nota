package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

	"github.com/egoist/mygo"
)

// Appearance is the frontend's light/dark mode and page colours, for
// what the page cannot paint itself: the windows' native appearance
// (NSAppearance on macOS, GTK's dark preference on Linux) and the
// colour behind the page while it loads or a resize outruns it. The
// frontend keeps the choice in localStorage (nota:theme, nota:palette);
// this copy lets the next start open in it before any page has loaded.
type Appearance struct {
	// Mode is the choice: "system", "light" or "dark".
	Mode string `json:"mode"`
	// Resolved is the mode in effect when the colours were read.
	Resolved string `json:"resolved,omitempty"`
	// Background is the main window's page colour, CaptureBackground the
	// quick-capture panel's, as #rrggbb. Others are ignored.
	Background        string `json:"background,omitempty"`
	CaptureBackground string `json:"captureBackground,omitempty"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// normalize maps unknown modes to "system" and drops colours that are
// not #rrggbb, so nothing from the page reaches the native side unchecked.
func (a Appearance) normalize() Appearance {
	switch a.Mode {
	case "light", "dark":
	default:
		a.Mode = "system"
	}
	if a.Resolved != "light" && a.Resolved != "dark" {
		a.Resolved = ""
	}
	if !hexColor.MatchString(a.Background) {
		a.Background = ""
	}
	if !hexColor.MatchString(a.CaptureBackground) {
		a.CaptureBackground = ""
	}
	return a
}

func (a Appearance) source() mygo.ThemeSource {
	switch a.Mode {
	case "light":
		return mygo.ThemeLight
	case "dark":
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

// Nota's own page colours (packages/ui/src/tokens.css), for a mode
// without a colour from the page.
const (
	lightBackground       = "#ffffff"
	darkBackground        = "#1c1c1e"
	darkCaptureBackground = "#26262a"
)

// backgrounds returns the windows' BackgroundColor values.
func (a Appearance) backgrounds() (main, capture string) {
	return a.pick(a.Background, lightBackground, darkBackground),
		a.pick(a.CaptureBackground, lightBackground, darkCaptureBackground)
}

// pick is c in an explicit mode. Under System the window must follow
// the OS, so c fills only the side it was read in and Nota's default
// the other: the colour is right after a restart in either case.
func (a Appearance) pick(c, light, dark string) string {
	switch a.Mode {
	case "light":
		if c != "" {
			return c
		}
		return light
	case "dark":
		if c != "" {
			return c
		}
		return dark
	}
	if c != "" && a.Resolved == "dark" {
		dark = c
	} else if c != "" && a.Resolved == "light" {
		light = c
	}
	return "light-dark(" + light + ", " + dark + ")"
}

func loadAppearance(path string) Appearance {
	var a Appearance
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &a) // a broken file is System
	}
	return a.normalize()
}

func saveAppearance(path string, a Appearance) error {
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// appearancePath is where the appearance is kept, next to MyGo's window
// state; "" when the platform has no user data folder.
func appearancePath() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "appearance.json")
}

// SetAppearance gives the windows the frontend's mode and page colours
// and remembers them for the next start. The frontend calls it whenever
// the theme changes, the OS's mode included while the mode is System.
func (d *Desktop) SetAppearance(a Appearance) error {
	a = a.normalize()
	d.mu.Lock()
	same := a == d.appearance
	d.appearance = a
	w, c, path := d.main, d.capture, d.appearanceFile
	d.mu.Unlock()
	if same {
		return nil // both windows report the same change
	}
	if path != "" {
		if err := saveAppearance(path, a); err != nil {
			warn("saving the appearance failed", "err", err)
		}
	}
	if w == nil && c == nil {
		return nil // no windows: the app is not running (tests)
	}
	mygo.Theme.SetSource(a.source())
	main, capture := a.backgrounds()
	if w != nil {
		if err := w.SetBackgroundColor(main); err != nil {
			return err
		}
	}
	if c != nil {
		if err := c.SetBackgroundColor(capture); err != nil {
			return err
		}
	}
	return nil
}
