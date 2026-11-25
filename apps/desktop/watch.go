package main

import (
	"context"
	"log/slog"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/plugins"
	"github.com/banjo/nota/internal/vault"
)

// Changes tells the frontend which notes changed: by the app itself,
// the sync engine, a hook, or another program editing the files.
type Changes struct {
	// Paths are the vault paths that changed (old and new paths of a
	// move). ".nota/views" stands for the saved views, ".nota/plugins"
	// for the plugins.
	Paths []string `json:"paths"`
	// Seq counts the change batches since the app started.
	Seq int64 `json:"seq"`
}

// changedEvent is sent after notes change, debounced to one batch per
// burst of writes. The frontend refetches what it shows.
var changedEvent = mygo.NewEvent[Changes]("changed")

// notifier collects changed paths and sends them in batches.
type notifier struct {
	mu      sync.Mutex
	paths   []string
	n       int64
	timer   *time.Timer
	delay   time.Duration
	publish func(Changes)
}

func newNotifier() *notifier {
	return &notifier{delay: 60 * time.Millisecond, publish: func(c Changes) {
		_ = changedEvent.Broadcast(c) // no window yet is fine
	}}
}

// add queues paths ("" is skipped) and schedules a batch.
func (c *notifier) add(paths ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range paths {
		if p != "" && !slices.Contains(c.paths, p) {
			c.paths = append(c.paths, p)
		}
	}
	if c.timer == nil {
		c.timer = time.AfterFunc(c.delay, c.flush)
	}
}

func (c *notifier) flush() {
	c.mu.Lock()
	ps := c.paths
	c.paths, c.timer = nil, nil
	c.n++
	batch := Changes{Paths: ps, Seq: c.n}
	c.mu.Unlock()
	if batch.Paths == nil {
		batch.Paths = []string{}
	}
	c.publish(batch)
}

func (c *notifier) seq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// viewsDir holds the saved views and pluginsDir the plugins, the parts
// of .nota/ the UI shows.
const (
	viewsDir   = vault.MetaDirName + "/views"
	pluginsDir = plugins.Dir
)

// under reports whether p is dir or inside it.
func under(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

// watchIgnored leaves out what never shows in the UI: .git, the trash,
// and .nota/ apart from the views and plugins (the index and sync state
// change on every write).
func watchIgnored(p string, isDir bool) bool {
	switch base := path.Base(p); {
	case isDir && (base == ".git" || base == ".trash" || base == ".obsidian" || base == "node_modules"):
		return true
	case p == vault.MetaDirName:
		return false
	case strings.HasPrefix(p, vault.MetaDirName+"/"):
		return !under(p, viewsDir) && !under(p, pluginsDir)
	}
	return false
}

// watch follows the vault's files, so edits made by other programs (an
// editor, git, the CLI, an agent) reach the index and the UI. It runs
// until ctx is done.
func (s *Vault) watch(ctx context.Context) {
	w, err := mygo.WatchFiles(ctx, s.v.Root(), mygo.FileWatchOptions{Recursive: true, Ignore: watchIgnored})
	if err != nil {
		slog.Warn("watching the vault failed; outside edits show after a restart", "err", err)
		return
	}
	go func() {
		defer w.Close()
		for {
			evs, err := w.Next(ctx)
			if err != nil {
				return
			}
			s.applyFileEvents(evs)
		}
	}()
}

// applyFileEvents brings the index up to date after outside changes and
// tells the frontend which notes changed. The app's own writes have
// updated the index already, so they find nothing new here.
func (s *Vault) applyFileEvents(evs []mygo.FileEvent) {
	var paths []string
	views, plugs := false, false
	var dirs, images []string
	for _, e := range evs {
		for _, p := range []string{e.Path, e.OldPath} {
			switch {
			case p == "":
			case under(p, viewsDir):
				views = true
			case under(p, pluginsDir):
				plugs = true
			case vault.IsNote(p):
				paths = append(paths, vault.NFC(p))
			case e.IsDir && p != ".":
				dirs = append(dirs, vault.NFC(p))
			case !e.IsDir && vault.ImageType(p) != "":
				images = append(images, vault.NFC(p))
			}
		}
	}
	// Folders made, removed or renamed by other programs change the tree
	// even though no note (and so nothing in the index) did.
	s.changes.add(dirs...)
	// Images aren't indexed, but notes show them: the frontend reloads
	// them (synced in, edited or replaced by another program).
	s.changes.add(images...)
	// Plugins don't touch the index: the frontend reads them again.
	if plugs {
		s.changes.add(pluginsDir)
	}
	if len(paths) == 0 && !views {
		return
	}
	s.mu.Lock()
	st, err := s.ix.Sync(s.v)
	s.mu.Unlock()
	if err != nil {
		slog.Warn("index sync failed", "err", err)
		return
	}
	if views {
		paths = append(paths, viewsDir)
	}
	if st.Changed() || views {
		s.changes.add(paths...)
	}
}
