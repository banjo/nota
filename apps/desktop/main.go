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
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/settings"
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

	appearance     Appearance // the last SetAppearance
	appearanceFile string     // where it is kept; "" for nowhere

	onboarding bool     // started without a vault: the window shows the setup
	source     string   // what chose the vault: "config", "env" or "default"
	acc        *Account // signs in to sync; linked to the vault at the end of setup
	configPath string   // the config file; "" for the default one
	// relaunch opens the new vault with the files to open again; nil
	// means mygo.App.Relaunch.
	relaunch func(files ...string)
	op       *Opener // files opened with the app; nil in tests
	// newMain makes the main window again after it was closed (macOS
	// keeps running without windows); nil before the app is ready.
	newMain func() *mygo.Window
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
// captured in the quick-capture window or a file opened with the app.
// On macOS, where closing the window leaves the app running, it opens
// the window again.
func (d *Desktop) ShowMain() {
	d.mu.Lock()
	w := d.main
	if (w == nil || w.IsDestroyed()) && d.newMain != nil {
		w = d.newMain()
		d.main = w
	}
	d.mu.Unlock()
	if w != nil {
		w.Restore()
		w.Show()
		w.Focus()
	}
}

// RevealVault opens the vault folder in the file manager.
func (d *Desktop) RevealVault() error {
	if d.dir == "" {
		return ErrNoVault
	}
	return mygo.Shell.OpenPath(d.dir)
}

// Platform is the OS the app runs on ("darwin", "linux", "windows"),
// for shortcut labels.
func (d *Desktop) Platform() string { return runtime.GOOS }

func send(id string) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) { _ = commandEvent.Broadcast(Command{ID: id}) }
}

func menu(d *Desktop) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New Note", Accelerator: "CmdOrCtrl+N", Click: send("note.new")},
			{Label: "Open…", Click: send("file.open")},
			{Label: "Quick Capture", Click: func(*mygo.MenuItem, *mygo.Window) { d.ShowCapture() }},
			mygo.Separator(),
			{Label: "Open Inbox", Accelerator: "CmdOrCtrl+Shift+I", Click: send("inbox.open")},
			{Label: "Add to Folder…", Accelerator: "CmdOrCtrl+Shift+M", Click: send("note.addToFolder")},
			{Label: "New Note in Folder…", Click: send("note.newInFolder")},
			{Label: "New Folder…", Accelerator: "CmdOrCtrl+Shift+N", Click: send("folder.add")},
			mygo.Separator(),
			{Label: "Switch Vault…", Click: send("vault.switch")},
			{Label: "New Vault…", Click: send("vault.new")},
			mygo.Separator(),
			{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: send("settings.open")},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: "Command Palette", Accelerator: "CmdOrCtrl+K", Click: send("palette.toggle")},
			{Label: "Toggle Sidebar", Accelerator: "CmdOrCtrl+\\", Click: send("sidebar.toggle")},
			{Label: "Toggle Reading Mode", Accelerator: "CmdOrCtrl+E", Click: send("view.mode.toggle")},
			{Label: "Toggle Dark Mode", Click: send("theme.toggleDark")},
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
	needsSetup, err := vault.NeedsOnboarding(loc)
	if err != nil {
		warn("could not tell whether this is a first run", "err", err)
	}
	d := &Desktop{appearanceFile: appearancePath(), onboarding: needsSetup, source: loc.Source}
	// Without a vault yet (first run) the services are bound empty: the
	// window shows the setup, and the setup relaunches the app once it
	// has made the vault. Otherwise the vault opens before the window.
	s := &Vault{}
	var acc *Account
	if needsSetup {
		acc = newAccountFor("", "", nil)
	} else {
		if s, err = openVault(loc.Dir, loc.Config.Inbox); err != nil {
			log.Fatal(err)
		}
		defer s.ix.Close()
		if s.hooks != nil {
			defer s.hooks.Wait()
		}
		// Sync runs in the background when the vault has a remote.
		s.syn.Store(startSync(s, loc.Dir, loc.Config.Inbox))
		defer func() { s.syn.Load().stop() }()
		acc = newAccount(s)
		d.dir = loc.Dir
	}
	d.acc = acc
	settingsPath, err := settings.Path()
	if err != nil {
		log.Fatal(err)
	}
	set := newSettings(settingsPath, acc.client)
	publish := acc.publish
	acc.publish = func(st AccountState) {
		publish(st)
		set.accountChanged(st.State)
	}
	acc.refreshEmail()
	home, _ := os.UserHomeDir()
	op := newOpener(openerEnv{
		locate: s.locateNote,
		imp:    s.importNote,
		nudge:  func(seq int64) { _ = openedEvent.Broadcast(OpenedNudge{Seq: seq}) },
		show:   d.ShowMain,
		trash:  mygo.Shell.TrashItem,
		reveal: mygo.Shell.ShowItemInFolder,
		watch:  watchFile,
		changed: func(id string) {
			_ = externalChangedEvent.Broadcast(ExternalChanged{ID: id})
		},
		home: home,
	})
	d.op = op
	mygo.Bind(s, acc, d, op, set)
	// Files opened with the app (Finder, the Dock, `open -a`, the command
	// line, a second instance). Registered before Run: a cold start
	// delivers them before any window exists, so Opener queues them and
	// the page takes them when it loads.
	mygo.App.OnOpenFile(op.receive)

	// Open in the last appearance, so neither the window chrome nor the
	// colour behind the page flashes the other mode before the page has
	// loaded and called SetAppearance.
	look := loadAppearance(d.appearanceFile)
	d.appearance = look
	mygo.Theme.SetSource(look.source())
	mainBackground, captureBackground := look.backgrounds()

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
	// macOS: clicking the Dock icon with no window open brings it back.
	mygo.App.OnActivate(func(visible bool) {
		if !visible {
			d.ShowMain()
		}
	})
	mygo.App.SetMenu(menu(d))
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})

	// The capture panel hides instead of closing, except while quitting.
	var quitting atomic.Bool
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting.Store(true) })
	mygo.App.OnQuitCanceled(func() { quitting.Store(false) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mygo.App.WhenReady(func() {
		newMain := func() *mygo.Window {
			w := mygo.NewWindow(mygo.WindowOptions{
				Title:           "Nota",
				Width:           1280,
				Height:          820,
				MinWidth:        360,
				MinHeight:       480,
				StateKey:        "main",
				URL:             "/",
				BackgroundColor: mainBackground,
			})
			w.OnClosed(func() {
				if runtime.GOOS != "darwin" {
					mygo.App.Quit()
				}
			})
			return w
		}
		w := newMain()
		d.mu.Lock()
		d.newMain = newMain
		d.mu.Unlock()
		set.run(ctx)
		if needsSetup {
			// Nothing to capture into yet; the setup relaunches the app.
			d.mu.Lock()
			d.main = w
			d.mu.Unlock()
			return
		}
		// Quick capture: a small panel above every app, summoned from
		// anywhere with ⌥⌘N. It hides rather than closes.
		c := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Quick Capture",
			URL:             "/?window=capture",
			Panel:           true,
			Hidden:          true,
			Frameless:       true,
			Width:           560,
			Height:          150,
			AlwaysOnTop:     true,
			BackgroundColor: captureBackground,
		})
		c.OnClose(func(e *mygo.CloseEvent) {
			if quitting.Load() {
				return // the app is quitting: let it close
			}
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
