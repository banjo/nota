package sync

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/banjo/nota/internal/vault"
)

// WatchOptions configure Engine.Watch.
type WatchOptions struct {
	// Debounce is how long the vault must be quiet after a local change
	// before a pass runs; 500 ms when 0.
	Debounce time.Duration
	// Poll is the fallback interval for a pass when nothing nudged one;
	// one minute when 0.
	Poll time.Duration
	// OnPass is called after every pass with its result or error.
	OnPass func(*Result, error)
	// NoEvents turns off the server's SSE nudges (polling still runs).
	NoEvents bool
}

// Watch syncs until ctx is done: once at the start, then after local
// file changes (fsnotify, debounced), on the server's change nudges
// (SSE) and every Poll interval. Errors of a pass are reported to OnPass
// and retried on the next trigger. It returns nil when ctx is cancelled.
func (e *Engine) Watch(ctx context.Context, o WatchOptions) error {
	if o.Debounce <= 0 {
		o.Debounce = 500 * time.Millisecond
	}
	if o.Poll <= 0 {
		o.Poll = time.Minute
	}
	trigger := make(chan struct{}, 1)
	nudge := func() {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}
	local := make(chan struct{}, 1)

	if osfs, ok := e.v.FS().(vault.OSPather); ok {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return err
		}
		defer w.Close()
		root := osfs.OSPath(".")
		e.watchTree(w, root, root)
		go e.watchLoop(ctx, w, root, local)
	}
	if !o.NoEvents {
		go e.events(ctx, nudge)
	}

	run := func() {
		res, err := e.Sync(ctx)
		if ctx.Err() != nil {
			return
		}
		if o.OnPass != nil {
			o.OnPass(res, err)
		}
	}
	run()
	poll := time.NewTicker(o.Poll)
	defer poll.Stop()
	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-local:
			debounce = time.After(o.Debounce) // restart on every change
		case <-debounce:
			debounce = nil
			run()
		case <-trigger:
			run()
		case <-poll.C:
			run()
		}
	}
}

// watchTree adds dir and every folder below it that is not ignored.
func (e *Engine) watchTree(w *fsnotify.Watcher, root, dir string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if rel, ok := relPath(root, p); ok && rel != "" && e.v.Ignored(rel, true) {
			return filepath.SkipDir
		}
		_ = w.Add(p)
		return nil
	})
}

func relPath(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return "", true
	}
	return vault.NFC(filepath.ToSlash(rel)), true
}

func (e *Engine) watchLoop(ctx context.Context, w *fsnotify.Watcher, root string, local chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			rel, ok := relPath(root, ev.Name)
			if !ok || rel == "" {
				continue
			}
			st, err := os.Stat(ev.Name)
			isDir := err == nil && st.IsDir()
			if e.v.Ignored(rel, isDir) {
				continue
			}
			if isDir && ev.Has(fsnotify.Create) {
				e.watchTree(w, root, ev.Name)
			}
			select {
			case local <- struct{}{}:
			default:
			}
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

// events follows the server's SSE stream and nudges a pass whenever the
// vault's cursor moves past the local one. It reconnects with backoff.
func (e *Engine) events(ctx context.Context, nudge func()) {
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := e.c.Events(ctx, func(cursor int64) {
			if cursor > e.cursor() {
				nudge()
			}
		})
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second // it was a healthy stream
		}
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 404 {
			backoff = 5 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Minute)
	}
}

// cursor reads the stored cursor without waiting for a running pass.
func (e *Engine) cursor() int64 { return e.st.cursor() }
