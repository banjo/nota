package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// EnvApp is the command that opens a file in the desktop app, for
// `nota open --app`: split on spaces like $EDITOR, with the file's
// absolute path appended. nota waits for it to exit. Unset, macOS uses
// Launch Services (open -b) and Linux runs the installed app directly.
const EnvApp = "NOTA_APP"

// appBundleID is the desktop app's identifier (apps/desktop/mygo.config.ts).
const appBundleID = "com.banjoanton.nota"

// linuxApp is where the .deb and .rpm packages install the app (MyGo puts
// it in /opt/<slug>/<slug>).
var linuxApp = "/opt/nota/nota"

// appLauncher is how a file reaches the desktop app.
type appLauncher struct {
	argv []string
	// wait: the command hands the file over and exits (open -b, $NOTA_APP);
	// otherwise it is the app itself, started in the background.
	wait bool
}

// launcherFor picks the launcher for goos, given $NOTA_APP and whether a
// file exists.
func launcherFor(goos, env string, exists func(string) bool) (appLauncher, error) {
	if f := strings.Fields(env); len(f) > 0 {
		return appLauncher{argv: f, wait: true}, nil
	}
	switch goos {
	case "darwin":
		return appLauncher{argv: []string{"open", "-b", appBundleID}, wait: true}, nil
	case "linux", "freebsd", "openbsd", "netbsd":
		if exists(linuxApp) {
			return appLauncher{argv: []string{linuxApp}}, nil
		}
		return appLauncher{}, &Error{Code: ExitError, Kind: "no_app",
			Message: fmt.Sprintf("the Nota app isn't installed at %s; set $%s to the command that runs it (an AppImage, or the app from the tarball)", linuxApp, EnvApp)}
	}
	return appLauncher{}, &Error{Code: ExitError, Kind: "no_app",
		Message: fmt.Sprintf("nota open --app isn't supported on %s; set $%s to the command that opens a file in Nota", goos, EnvApp)}
}

// isMarkdownFile reports whether name has an extension the app opens.
func isMarkdownFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// appTarget is the absolute path the app should open for ref: an existing
// Markdown file (in the vault or not), else the note ref names.
func (a *app) appTarget(ref string) (string, error) {
	st, err := os.Stat(ref)
	switch {
	case err == nil && st.IsDir():
		return "", usageErr("%s is a folder; give a Markdown file or a note", ref)
	case err == nil && !isMarkdownFile(ref):
		return "", usageErr("%s isn't a Markdown file (.md or .markdown)", ref)
	case err == nil:
		return filepath.Abs(ref)
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	p, err := a.resolve(ref)
	if err != nil {
		return "", err
	}
	disk, err := a.vault.DiskPath(p)
	if err != nil {
		return "", err
	}
	return filepath.Abs(disk)
}

// openInApp hands ref to the desktop app: a note of the vault opens as
// that note (in Reading mode), any other Markdown file as a preview.
func (a *app) openInApp(ref string) error {
	target, err := a.appTarget(ref)
	if err != nil {
		return err
	}
	l, err := launcherFor(runtime.GOOS, os.Getenv(EnvApp), func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	})
	if err != nil {
		return err
	}
	c := exec.Command(l.argv[0], append(l.argv[1:], target)...)
	a.log.Debug("open in app", "cmd", c.Args)
	if !l.wait {
		// The app keeps running after nota exits; a running app takes the
		// file over its single-instance socket and this one exits.
		detach(c)
		if err := c.Start(); err != nil {
			return fmt.Errorf("start %s: %w", l.argv[0], err)
		}
		go func() { _ = c.Wait() }()
	} else {
		var stderr bytes.Buffer
		c.Stdout, c.Stderr = a.stderr, &stderr
		if err := c.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if runtime.GOOS == "darwin" && os.Getenv(EnvApp) == "" {
				return &Error{Code: ExitError, Kind: "no_app",
					Message: fmt.Sprintf("couldn't open Nota.app (is it installed? brew install --cask banjo/tap/nota): %s", firstNonEmpty(msg, err.Error()))}
			}
			if msg != "" {
				return fmt.Errorf("%s: %w: %s", l.argv[0], err, msg)
			}
			return fmt.Errorf("%s: %w", l.argv[0], err)
		}
	}
	if a.json {
		return a.printJSON(map[string]string{"file": target})
	}
	return nil
}
