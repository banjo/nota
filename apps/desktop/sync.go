package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/index"
	nsync "github.com/banjo/nota/internal/sync"
	"github.com/banjo/nota/internal/vault"
)

// SyncStatus describes the background sync of the open vault.
type SyncStatus struct {
	// State is "off" (no .nota/sync.toml), "starting", "synced" or "error".
	State string `json:"state"`
	// Remote is the server URL and Vault the vault name on it.
	Remote string `json:"remote,omitempty"`
	Vault  string `json:"vault,omitempty"`
	// Cursor is the last applied server version.
	Cursor int64 `json:"cursor"`
	// Changed reports that the last pass changed files.
	Changed bool `json:"changed"`
	// Conflicts are the conflict copies the last pass made.
	Conflicts []string `json:"conflicts"`
	// Error is the last pass's error, or the reason sync is off.
	Error string `json:"error,omitempty"`
	// Time is when the status was taken.
	Time time.Time `json:"time"`
}

// syncStatusEvent tells the frontend about every sync pass.
var syncStatusEvent = mygo.NewEvent[SyncStatus]("syncStatus")

// storeApp marks a token kept in the app's credential store
// (mygo.Secrets: the Keychain on macOS) rather than the CLI's keyring
// entry or credentials file.
const storeApp = "app"

// loadToken reads a remote's device token from where sign-in put it.
func loadToken(r nsync.Remote) (string, error) {
	if r.TokenStore != storeApp {
		return nsync.LoadToken(r.Account(), r.TokenStore)
	}
	b, err := mygo.Secrets.Get(r.Account())
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return "", nsync.ErrNoToken
	}
	return string(b), err
}

// syncer runs the sync engine of the vault in the background.
type syncer struct {
	mu     sync.Mutex
	status SyncStatus
	c      *nsync.Client // nil while sync is off
	cancel context.CancelFunc
	done   chan struct{}
}

func offSyncer() *syncer {
	return &syncer{status: SyncStatus{State: "off", Conflicts: []string{}, Time: time.Now()}}
}

func (y *syncer) set(st SyncStatus) {
	st.Time = time.Now()
	if st.Conflicts == nil {
		st.Conflicts = []string{}
	}
	y.mu.Lock()
	y.status = st
	y.mu.Unlock()
	_ = syncStatusEvent.Broadcast(st) // no window yet is fine: SyncStatus() has it
}

func (y *syncer) get() SyncStatus {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.status
}

// client is the server client of a running sync, for history; nil when
// sync is off.
func (y *syncer) client() *nsync.Client {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.c
}

// startSync starts the background sync when the vault has a remote
// (.nota/sync.toml, made by signing in or by `nota remote add`). It uses
// a vault instance of its own, whose writes update the shared index
// under s.mu and reach the frontend as changes.
func startSync(s *Vault, dir, inbox string) *syncer {
	y := offSyncer()
	r, ok, err := nsync.LoadRemote(s.v.MetaDir())
	if err != nil {
		y.set(SyncStatus{State: "error", Error: err.Error()})
		return y
	}
	if !ok {
		y.set(SyncStatus{State: "off"})
		return y
	}
	base := SyncStatus{Remote: r.URL, Vault: r.Vault}
	fail := func(err error) *syncer {
		st := base
		st.State, st.Error = "error", err.Error()
		y.set(st)
		slog.Warn("sync disabled", "err", err)
		return y
	}
	tok, err := loadToken(r)
	if err != nil {
		return fail(err)
	}
	bus := events.New()
	sv, err := vault.Open(dir, vault.Options{Inbox: inbox, Bus: bus, Now: clock()})
	if err != nil {
		return fail(err)
	}
	bus.Subscribe(func(e events.Event) {
		s.mu.Lock()
		err := updateIndex(s.ix, sv, e)
		s.mu.Unlock()
		if err != nil {
			slog.Warn("index update failed", "path", e.Path, "err", err)
		}
		s.changes.add(e.Path, e.OldPath)
	}, events.NoteCreated, events.NoteSaved, events.NoteMoved, events.NoteDeleted)
	c := &nsync.Client{BaseURL: r.URL, Vault: r.Vault, Token: tok, UserAgent: "nota-desktop"}
	e, err := nsync.Open(sv, c, nsync.Options{Device: r.Device})
	if err != nil {
		bus.Close()
		return fail(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	y.mu.Lock()
	y.c, y.cancel, y.done = c, cancel, make(chan struct{})
	y.mu.Unlock()
	st := base
	st.State = "starting"
	y.set(st)
	go func() {
		defer close(y.done)
		defer bus.Close()
		defer e.Close()
		_ = e.Watch(ctx, nsync.WatchOptions{OnPass: func(res *nsync.Result, err error) {
			st := base
			st.Cursor = res.Cursor
			if err != nil {
				st.State, st.Error = "error", err.Error()
			} else {
				st.State, st.Changed = "synced", res.Changed()
				for _, c := range res.Conflicts {
					st.Conflicts = append(st.Conflicts, c.Copy)
				}
			}
			y.set(st)
		}})
	}()
	return y
}

// stop ends the background sync and waits for a running pass.
func (y *syncer) stop() {
	y.mu.Lock()
	cancel, done := y.cancel, y.done
	y.cancel, y.c = nil, nil
	y.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// restartSync stops the running sync, if any, and starts it again from
// .nota/sync.toml (after signing in or out).
func (s *Vault) restartSync() {
	s.syn.Load().stop()
	s.syn.Store(startSync(s, s.v.Root(), s.v.Inbox()))
}

// updateIndex applies one vault event to the index.
func updateIndex(ix *index.Index, v *vault.Vault, e events.Event) error {
	switch e.Type {
	case events.NoteMoved:
		if err := ix.Remove(e.OldPath); err != nil {
			return err
		}
		return ix.Update(v, e.Path)
	case events.NoteDeleted:
		return ix.Remove(e.Path)
	}
	return ix.Update(v, e.Path)
}
