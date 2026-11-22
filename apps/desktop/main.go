// Command desktop is the Nota desktop app. It serves the React frontend
// (packages/app) in the system webview and gives it the vault through
// bound Go services: the frontend never reads files itself. All vault
// IO goes through the same core packages as the nota CLI
// (internal/vault, internal/index, internal/views, internal/edit).
package main

import (
	"context"
	"log"
	"log/slog"
	"runtime"
	"strings"
	"sync"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/vault"
)

func warn(msg string, args ...any) { slog.Warn(msg, args...) }

// Command is a menu item or global shortcut the frontend carries out,
// e.g. "note.new" or "palette.toggle".
type Command struct {
	ID string `json:"id"`
}

// commandEvent sends menu commands to the main window's page.
var commandEvent = mygo.NewEvent[Command]("command")

// Desktop is what only the desktop app has: its windows and the file
// manager.
type Desktop struct {
	mu      sync.Mutex
	main    *mygo.Window
	capture *mygo.Window
	dir     string
}

// HideCapture hides the quick-capture window (after saving, or on
// Escape). It keeps running, so it shows at once the next time.
func (d *Desktop) HideCapture() {
	d.mu.Lock()
	w := d.capture
	d.mu.Unlock()
	if w != nil {
		w.Hide()
	}
}

// ShowCapture shows the quick-capture window over other apps.
func (d *Desktop) ShowCapture() {
	d.mu.Lock()
	w := d.capture
	d.mu.Unlock()
	if w == nil {
		return
	}
	disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	width, height := w.Size()
	w.SetPosition(disp.WorkArea.X+(disp.WorkArea.Width-width)/2, disp.WorkArea.Y+(disp.WorkArea.Height-height)/3)
	w.Show()
	w.Focus()
	_ = commandEvent.Emit(w, Command{ID: "capture.focus"})
}

// ShowMain brings the main window to the front, e.g. to open a note
// captured in the quick-capture window.
func (d *Desktop) ShowMain() {
	d.mu.Lock()
	w := d.main
	d.mu.Unlock()
	if w != nil {
		w.Restore()
		w.Show()
		w.Focus()
	}
}

// RevealVault opens the vault folder in the file manager.
func (d *Desktop) RevealVault() error {
	return mygo.Shell.OpenPath(d.dir)
}

// Platform is the OS the app runs on ("darwin", "linux", "windows"),
// for shortcut labels.
func (d *Desktop) Platform() string { return runtime.GOOS }

// welcome is written into a vault that has no notes yet, so a fresh
// install (or a phone, which has no NOTA_VAULT) opens on something.
const welcome = "---\ntags: [nota]\n---\n# Välkommen till Nota\n\nAnteckningarna är vanliga .md-filer i den här mappen.\n"

func ensureWelcome(s *Vault) error {
	es, err := s.v.List()
	if err != nil || len(es) > 0 {
		return err
	}
	_, _, err = s.v.Write("Välkommen.md", []byte(welcome))
	return err
}

func send(id string) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) { _ = commandEvent.Broadcast(Command{ID: id}) }
}

func menu(d *Desktop) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New Note", Accelerator: "CmdOrCtrl+N", Click: send("note.new")},
			{Label: "Quick Capture", Click: func(*mygo.MenuItem, *mygo.Window) { d.ShowCapture() }},
			mygo.Separator(),
			{Label: "Open Inbox", Accelerator: "CmdOrCtrl+Shift+I", Click: send("inbox.open")},
			{Label: "Move Note To…", Accelerator: "CmdOrCtrl+Shift+M", Click: send("note.move")},
			mygo.Separator(),
			{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: send("settings.open")},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: "Command Palette", Accelerator: "CmdOrCtrl+K", Click: send("palette.toggle")},
			{Label: "Toggle Sidebar", Accelerator: "CmdOrCtrl+\\", Click: send("sidebar.toggle")},
			mygo.Separator(),
			{Role: mygo.RoleResetZoom},
			{Role: mygo.RoleZoomIn},
			{Role: mygo.RoleZoomOut},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance gets our deep link
	}
	loc, err := vault.Discover("")
	if err != nil {
		log.Fatal(err)
	}
	s, err := openVault(loc.Dir, loc.Config.Inbox)
	if err != nil {
		log.Fatal(err)
	}
	defer s.ix.Close()
	if s.hooks != nil {
		defer s.hooks.Wait()
	}
	if err := ensureWelcome(s); err != nil {
		log.Fatal(err)
	}
	// Sync runs in the background when the vault has a remote.
	s.syn.Store(startSync(s, loc.Dir, loc.Config.Inbox))
	defer func() { s.syn.Load().stop() }()
	acc := newAccount(s)
	acc.refreshEmail()
	d := &Desktop{dir: loc.Dir}
	mygo.Bind(s, acc, d)

	// Sign-in comes back as nota://auth?code=… (registered at install;
	// portable builds register it here).
	if runtime.GOOS != "darwin" {
		if err := mygo.App.RegisterURLScheme("nota"); err != nil {
			warn("registering nota:// failed", "err", err)
		}
	}
	mygo.App.OnOpenURL(func(u string) {
		if strings.HasPrefix(u, "nota://auth") {
			go func() { _ = acc.CompleteSignIn(u) }()
			d.ShowMain()
		}
	})
	mygo.App.OnSecondInstance(func([]string, string) { d.ShowMain() })
	mygo.App.SetMenu(menu(d))
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Nota",
			Width:           1280,
			Height:          820,
			MinWidth:        360,
			MinHeight:       480,
			StateKey:        "main",
			URL:             "/",
			BackgroundColor: "light-dark(#ffffff, #1c1c1e)",
		})
		w.OnClosed(func() {
			if runtime.GOOS != "darwin" {
				mygo.App.Quit()
			}
		})
		// Quick capture: a small panel above every app, summoned from
		// anywhere with ⌥⌘N. It hides rather than closes.
		c := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Quick Capture",
			URL:             "/?window=capture",
			Panel:           true,
			Hidden:          true,
			Frameless:       true,
			Width:           560,
			Height:          164,
			AlwaysOnTop:     true,
			BackgroundColor: "light-dark(#ffffff, #232325)",
		})
		c.OnClose(func(e *mygo.CloseEvent) {
			e.PreventDefault()
			c.Hide()
		})
		d.mu.Lock()
		d.main, d.capture = w, c
		d.mu.Unlock()
		if err := mygo.GlobalShortcut.Register("Alt+CmdOrCtrl+N", d.ShowCapture); err != nil {
			warn("quick capture shortcut unavailable", "err", err)
		}
		s.watch(ctx)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
