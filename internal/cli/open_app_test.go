package cli

import (
	"errors"
	"slices"
	"testing"
)

func TestLauncherFor(t *testing.T) {
	none := func(string) bool { return false }
	all := func(string) bool { return true }
	cases := []struct {
		goos, env string
		exists    func(string) bool
		argv      []string
		wait      bool
		noApp     bool
	}{
		{goos: "darwin", exists: none, argv: []string{"open", "-b", "com.banjoanton.nota"}, wait: true},
		{goos: "linux", exists: all, argv: []string{"/opt/nota/nota"}},
		{goos: "linux", exists: none, noApp: true},
		{goos: "windows", exists: all, noApp: true},
		{goos: "linux", env: " /x/Nota.AppImage  --flag ", exists: none, argv: []string{"/x/Nota.AppImage", "--flag"}, wait: true},
		{goos: "darwin", env: "open -a Nota", exists: none, argv: []string{"open", "-a", "Nota"}, wait: true},
	}
	for _, c := range cases {
		l, err := launcherFor(c.goos, c.env, c.exists)
		var e *Error
		if c.noApp {
			if !errors.As(err, &e) || e.Kind != "no_app" || e.Code != ExitError {
				t.Errorf("%s %q: %v, want a no_app error", c.goos, c.env, err)
			}
			continue
		}
		if err != nil || !slices.Equal(l.argv, c.argv) || l.wait != c.wait {
			t.Errorf("%s %q: %+v %v, want %v wait=%v", c.goos, c.env, l, err, c.argv, c.wait)
		}
	}
}

func TestIsMarkdownFile(t *testing.T) {
	for name, want := range map[string]bool{"a.md": true, "B.MD": true, "c.Markdown": true, "d.txt": false, "md": false, "e.md.png": false} {
		if isMarkdownFile(name) != want {
			t.Errorf("%s: want %v", name, want)
		}
	}
}
