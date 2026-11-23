package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/vault"
)

// First-run setup, and "Switch vault…" / "New vault…" in Settings, which
// run the same flow (packages/app/src/onboarding).
//
// The vault is not hot-swapped. main() opens one vault and wires the
// index, the file watcher, the hooks and the background sync to it once;
// a vault switch writes the config and relaunches the app (App.Relaunch)
// instead of tearing all of that down and rebuilding it. On first run no
// vault is bound at all: the window opens in the setup, Vault answers
// ErrNoVault, and Account runs without a vault until the setup attaches
// the one it made.

// VaultResult is the vault the setup made or chose.
type VaultResult struct {
	// Path is the vault folder, Name its last element.
	Path string `json:"path"`
	Name string `json:"name"`
}

// welcomeNote is the starter note of a brand-new vault: where the notes
// live and the three things worth knowing. The shortcuts are the ones
// of the platform, and the inbox the one chosen in the setup.
func welcomeNote(goos, inbox string) string {
	search, capture := "Ctrl+K", "Ctrl+Alt+N"
	if goos == "darwin" {
		search, capture = "⌘K", "⌥⌘N"
	}
	return fmt.Sprintf(`---
tags: [welcome]
---
# Welcome to Nota

Every note here is a Markdown file in this folder. Edit them in Nota or in any other editor.

- Press %s to search and jump to any note.
- Press %s to capture a thought into %s.
- Link notes with `+"`[[double brackets]]`"+` and organise them with `+"`#tags`"+`.
`, search, capture, inbox)
}

// NeedsOnboarding reports that the app started without a vault, so the
// window shows the setup instead of the notes.
func (d *Desktop) NeedsOnboarding() bool { return d.onboarding }

// DefaultLocation is where a new vault goes unless the user picks
// another folder: ~/Documents when it exists (easy to find, and backed
// up by iCloud on macOS), else the home folder.
func (d *Desktop) DefaultLocation() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	docs := filepath.Join(home, "Documents")
	if fi, err := os.Stat(docs); err == nil && fi.IsDir() {
		return docs
	}
	return home
}

// VaultSource says what chose the open vault: "config" (the config
// file), "env" ($NOTA_VAULT) or "default" (~/Notes). With "env" a
// switch has no effect, because the variable wins over the config.
func (d *Desktop) VaultSource() string { return d.source }

// PickFolder shows the system's folder chooser, starting in start when
// given, and returns the chosen folder, or "" when the user cancelled.
func (d *Desktop) PickFolder(ctx context.Context, title, start string) (string, error) {
	if title == "" {
		title = "Choose a folder"
	}
	if fi, err := os.Stat(start); err != nil || !fi.IsDir() {
		start = d.DefaultLocation()
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            mygo.CallerWindow(ctx),
		Title:             title,
		DefaultPath:       start,
		Directory:         true,
		CreateDirectories: true,
		ButtonLabel:       "Choose",
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// InspectFolder looks at a folder the user typed or picked: whether it
// exists, is empty, how many Markdown notes it holds and whether Nota
// can write to it. It changes nothing.
func (d *Desktop) InspectFolder(path string) (vault.FolderInfo, error) {
	return vault.InspectFolder(path)
}

// CheckLocation says why a folder can't hold a new vault ("" when it
// can): not a full path, a drive root or system folder, or a file. The
// folder need not exist yet; creating the vault makes it.
func (d *Desktop) CheckLocation(location string) string {
	if _, err := vault.CheckLocation(location); err != nil {
		return err.Error()
	}
	return ""
}

// CreateVault makes the vault folder <location>/<name> and makes it
// the vault of the app, which then relaunches. A folder that exists and
// has files in it is refused (use UseVault for that). With welcome set,
// a new vault gets an English starter note.
func (d *Desktop) CreateVault(location, name, inbox string, welcome bool) (VaultResult, error) {
	inbox, err := cleanInbox(inbox)
	if err != nil {
		return VaultResult{}, err // before a folder is made for it
	}
	dir, err := vault.CreateVaultDir(location, name)
	if err != nil {
		return VaultResult{}, err
	}
	return d.activate(dir, inbox, welcome)
}

// UseVault makes an existing folder the vault of the app, which then
// relaunches. Nothing in the folder is moved or rewritten; Nota adds
// only its .nota folder. A starter note is added only when the folder
// is empty and welcome is set.
func (d *Desktop) UseVault(path, inbox string, welcome bool) (VaultResult, error) {
	info, err := vault.InspectFolder(path)
	switch {
	case err != nil:
		return VaultResult{}, err
	case !info.Exists:
		return VaultResult{}, fmt.Errorf("“%s” doesn’t exist.", info.Path)
	case !info.IsDir:
		return VaultResult{}, fmt.Errorf("“%s” is a file, not a folder.", info.Path)
	case !info.Writable:
		return VaultResult{}, fmt.Errorf("Nota can’t write to “%s”.", info.Path)
	}
	return d.activate(info.Path, inbox, welcome && info.Empty)
}

// activate records dir as the vault: the sync the user signed in to
// during setup is linked, the starter note written, the config saved
// (marking the setup done), and the app relaunched to open it.
func (d *Desktop) activate(dir, inbox string, welcome bool) (VaultResult, error) {
	inbox, err := cleanInbox(inbox)
	if err != nil {
		return VaultResult{}, err
	}
	// Only a vault that is empty now gets the note; linking sync puts
	// .nota/sync.toml in it, so ask before that.
	if es, err := os.ReadDir(dir); err != nil || len(es) > 0 {
		welcome = false
	}
	res := VaultResult{Path: dir, Name: filepath.Base(dir)}
	if d.acc != nil {
		if err := d.acc.attachVault(filepath.Join(dir, vault.MetaDirName), res.Name); err != nil {
			return VaultResult{}, fmt.Errorf("Couldn’t set up sync: %w", err)
		}
	}
	if welcome {
		f, err := os.OpenFile(filepath.Join(dir, "Welcome.md"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, err = f.WriteString(welcomeNote(runtime.GOOS, inbox))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil && !errors.Is(err, os.ErrExist) {
			return VaultResult{}, fmt.Errorf("Couldn’t write the welcome note: %w", err)
		}
	}
	cfg, err := d.configFile()
	if err != nil {
		return VaultResult{}, err
	}
	if err := vault.SaveConfig(cfg, vault.Config{Vault: dir, Inbox: inbox, Onboarded: true}); err != nil {
		return VaultResult{}, err
	}
	d.restart()
	return res, nil
}

// cleanInbox checks the inbox folder name; blank means the default.
func cleanInbox(inbox string) (string, error) {
	if strings.TrimSpace(inbox) == "" {
		return vault.DefaultInbox, nil
	}
	return vault.CheckName(inbox, vault.InboxName)
}

func (d *Desktop) configFile() (string, error) {
	if d.configPath != "" {
		return d.configPath, nil
	}
	return vault.ConfigPath()
}

func (d *Desktop) restart() {
	if d.relaunch != nil {
		d.relaunch()
		return
	}
	mygo.App.Relaunch()
}
