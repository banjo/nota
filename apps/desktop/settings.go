package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/settings"
	nsync "github.com/banjo/nota/internal/sync"
)

// The user's settings (internal/settings) as the frontend sees them:
// the file layer (settings.toml, read and watched here, never written)
// and the account layer (the sync server, when this vault is signed in).
// The device layer is the frontend's localStorage; the frontend merges
// the three.

// SettingsState is the layers of the settings that live outside the page.
type SettingsState struct {
	// File is what settings.toml sets: those keys are locked.
	File map[string]string `json:"file"`
	// Account is what the account sets; null when not signed in, or
	// signed in but not fetched yet (offline).
	Account map[string]string `json:"account"`
	// SignedIn says whether changes go to the account.
	SignedIn bool `json:"signedIn"`
	// Path is where settings.toml is.
	Path string `json:"path"`
	// Warnings are problems with the file or the account, for Settings.
	Warnings []string `json:"warnings"`
}

// SettingsPatch changes the account's settings key by key.
type SettingsPatch struct {
	Set   map[string]string `json:"set"`
	Unset []string          `json:"unset"`
}

// settingsEvent tells every window that a layer changed.
var settingsEvent = mygo.NewEvent[SettingsState]("settings")

// ErrNotSignedIn is returned when a change needs the account.
var ErrNotSignedIn = errors.New("not signed in: the setting is kept on this device")

// Settings reads settings.toml and talks to the account's settings.
type Settings struct {
	mu          sync.Mutex
	path        string
	file        settings.Values
	fileErr     string
	fileWarns   []string
	account     settings.Values // nil: unknown (signed out, or not fetched yet)
	accountRev  int64
	accountWarn string

	// client is the account's server client; nil when not signed in.
	client  func() *nsync.Client
	publish func(SettingsState)
	watch   func(ctx context.Context, file string, changed func())
	kick    chan struct{}
	retry   time.Duration
}

func newSettings(path string, client func() *nsync.Client) *Settings {
	s := &Settings{
		path:    path,
		client:  client,
		publish: func(st SettingsState) { _ = settingsEvent.Broadcast(st) },
		watch:   watchFile,
		kick:    make(chan struct{}, 1),
		retry:   5 * time.Second,
	}
	s.loadFile()
	return s
}

// loadFile reads settings.toml and reports whether what it sets changed.
func (s *Settings) loadFile() bool {
	v, warns, err := settings.Load(s.path)
	msg := ""
	if err != nil {
		v, msg = settings.Values{}, err.Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := !maps.Equal(v, s.file) || msg != s.fileErr || !slices.Equal(warns, s.fileWarns)
	s.file, s.fileErr, s.fileWarns = v, msg, warns
	return changed
}

func (s *Settings) state() SettingsState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := SettingsState{File: maps.Clone(s.file), Path: s.path, Warnings: []string{}}
	if st.File == nil {
		st.File = map[string]string{}
	}
	if s.fileErr != "" {
		st.Warnings = append(st.Warnings, s.fileErr+" (the file is ignored)")
	}
	for _, w := range s.fileWarns {
		st.Warnings = append(st.Warnings, settings.File+": "+w)
	}
	if s.account != nil {
		st.Account = maps.Clone(s.account)
	}
	if s.accountWarn != "" {
		st.Warnings = append(st.Warnings, s.accountWarn)
	}
	return st
}

// State returns the layers, fetching the account's settings the first
// time. Changes also arrive as "settings" events.
func (s *Settings) State(ctx context.Context) SettingsState {
	c := s.client()
	s.mu.Lock()
	unknown := s.account == nil
	s.mu.Unlock()
	if c != nil && unknown {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		s.fetch(ctx, c)
	}
	st := s.state()
	st.SignedIn = c != nil
	return st
}

// fetch loads the account's settings; it reports whether they changed.
func (s *Settings) fetch(ctx context.Context, c *nsync.Client) bool {
	as, err := c.Settings(ctx)
	if err != nil {
		s.mu.Lock()
		s.accountWarn = "Couldn't load your account's settings: " + err.Error()
		s.mu.Unlock()
		return false
	}
	return s.setAccount(as)
}

func (s *Settings) setAccount(as protocol.AccountSettings) bool {
	v := settings.Values(as.Settings)
	if v == nil {
		v = settings.Values{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.account == nil || !maps.Equal(v, s.account) || s.accountWarn != ""
	s.account, s.accountRev, s.accountWarn = v, as.Rev, ""
	return changed
}

// Update changes the account's settings. Values the app does not know
// are refused; the file's locked keys are the frontend's business.
func (s *Settings) Update(ctx context.Context, p SettingsPatch) (SettingsState, error) {
	c := s.client()
	if c == nil {
		return s.state(), ErrNotSignedIn
	}
	for k, v := range p.Set {
		key, ok := settings.Lookup(k)
		if !ok {
			return s.state(), fmt.Errorf("unknown setting %q", k)
		}
		if !key.Valid(v) {
			return s.state(), fmt.Errorf("%q is not a valid %s", v, k)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	as, err := c.UpdateSettings(ctx, protocol.SettingsPatch{Set: p.Set, Unset: p.Unset})
	if err != nil {
		return s.state(), err
	}
	s.setAccount(as)
	st := s.state()
	st.SignedIn = true
	s.publish(st)
	return st, nil
}

// accountChanged forgets the account's settings after signing in or out,
// and has run reconnect with the new account.
func (s *Settings) accountChanged(state string) {
	if state != "signed-in" && state != "signed-out" {
		return
	}
	s.mu.Lock()
	s.account, s.accountRev, s.accountWarn = nil, 0, ""
	s.mu.Unlock()
	select {
	case s.kick <- struct{}{}:
	default:
	}
	s.emit() // signed in: the account's values follow once fetched
}

func (s *Settings) emit() {
	st := s.state()
	st.SignedIn = s.client() != nil
	s.publish(st)
}

// run follows settings.toml and the account's settings stream until ctx
// is done.
func (s *Settings) run(ctx context.Context) {
	s.watchFile(ctx)
	go s.followAccount(ctx)
}

// watchFile watches settings.toml, and the file it links to when it is
// a symlink into a dotfiles repository: editing the target changes the
// settings too.
func (s *Settings) watchFile(ctx context.Context) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		slog.Warn("watching settings.toml failed", "err", err)
		return
	}
	var mu sync.Mutex
	var target string
	var stopTarget context.CancelFunc
	var changed func()
	follow := func() {
		t, err := filepath.EvalSymlinks(s.path)
		if err != nil || t == s.path {
			t = ""
		}
		if t == target {
			return
		}
		if stopTarget != nil {
			stopTarget()
			stopTarget = nil
		}
		target = t
		if t != "" {
			tctx, cancel := context.WithCancel(ctx)
			stopTarget = cancel
			go s.watch(tctx, t, changed)
		}
	}
	changed = func() {
		mu.Lock()
		follow()
		mu.Unlock()
		if s.loadFile() {
			s.emit()
		}
	}
	mu.Lock()
	follow()
	mu.Unlock()
	go s.watch(ctx, s.path, changed)
}

// followAccount keeps the account's settings current while signed in:
// the server's stream sends the revision at the start and after every
// change, and a new revision is fetched. It reconnects after errors and
// starts over when the account changes.
func (s *Settings) followAccount(ctx context.Context) {
	for ctx.Err() == nil {
		c := s.client()
		if c == nil {
			select {
			case <-ctx.Done():
			case <-s.kick:
			}
			continue
		}
		sctx, cancel := context.WithCancel(ctx)
		go func() {
			select {
			case <-s.kick:
				cancel()
			case <-sctx.Done():
			}
		}()
		err := c.SettingsEvents(sctx, func(rev int64) {
			s.mu.Lock()
			known := s.account != nil && rev == s.accountRev
			s.mu.Unlock()
			if !known && s.fetch(sctx, c) {
				s.emit()
			}
		})
		kicked := sctx.Err() != nil && ctx.Err() == nil
		cancel()
		if kicked || ctx.Err() != nil {
			continue
		}
		if err != nil {
			slog.Debug("settings stream broke", "err", err)
		}
		select {
		case <-ctx.Done():
		case <-s.kick:
		case <-time.After(s.retry):
		}
	}
}
