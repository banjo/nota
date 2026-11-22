package main

import (
	"context"
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

// syncer runs the sync engine of the vault in the background.
type syncer struct {
	mu     sync.Mutex
	status SyncStatus
	cancel context.CancelFunc
	done   chan struct{}
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

// startSync starts the background sync when the vault has a remote
// (.nota/sync.toml, made by `nota remote add`). It uses a vault instance
// of its own, whose writes update the shared index under s.mu.
func startSync(s *Vault, dir, inbox string) *syncer {
	y := &syncer{status: SyncStatus{State: "off", Conflicts: []string{}}}
	r, ok, err := nsync.LoadRemote(s.v.MetaDir())
	if err != nil {
		y.set(SyncStatus{State: "error", Error: err.Error()})
		return y
	}
	if !ok {
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
	tok, err := nsync.LoadToken(r.Account(), r.TokenStore)
	if err != nil {
		return fail(err)
	}
	bus := events.New()
	sv, err := vault.Open(dir, vault.Options{Inbox: inbox, Bus: bus})
	if err != nil {
		return fail(err)
	}
	bus.Subscribe(func(e events.Event) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := updateIndex(s.ix, sv, e); err != nil {
			slog.Warn("index update failed", "path", e.Path, "err", err)
		}
	}, events.NoteCreated, events.NoteSaved, events.NoteMoved, events.NoteDeleted)
	c := &nsync.Client{BaseURL: r.URL, Vault: r.Vault, Token: tok, UserAgent: "nota-desktop"}
	e, err := nsync.Open(sv, c, nsync.Options{Device: r.Device})
	if err != nil {
		bus.Close()
		return fail(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	y.cancel, y.done = cancel, make(chan struct{})
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
	if y.cancel != nil {
		y.cancel()
		<-y.done
	}
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
