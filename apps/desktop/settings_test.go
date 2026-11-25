package main

import (
	"context"
	"io"
	"log/slog"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/server"
	nsync "github.com/banjo/nota/internal/sync"
)

// fakeWatch records the files watched and lets the test fire changes.
type fakeWatch struct {
	mu    sync.Mutex
	files map[string]func()
}

func (f *fakeWatch) watch(_ context.Context, file string, changed func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[file] = changed
}

func (f *fakeWatch) fire(file string) {
	f.mu.Lock()
	fn := f.files[file]
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// watched waits for n files to be watched (watching starts in the
// background) and returns them.
func (f *fakeWatch) watched(n int) []string {
	for range 200 {
		f.mu.Lock()
		got := slices.Sorted(maps.Keys(f.files))
		f.mu.Unlock()
		if len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// published collects the "settings" events.
type published struct {
	mu  sync.Mutex
	got []SettingsState
	ch  chan SettingsState
}

func newPublished() *published { return &published{ch: make(chan SettingsState, 16)} }

func (p *published) publish(st SettingsState) {
	p.mu.Lock()
	p.got = append(p.got, st)
	p.mu.Unlock()
	p.ch <- st
}

func (p *published) next(t *testing.T) SettingsState {
	t.Helper()
	select {
	case st := <-p.ch:
		return st
	case <-time.After(5 * time.Second):
		t.Fatal("no settings event")
		return SettingsState{}
	}
}

func TestSettingsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nota", "settings.toml")
	s := newSettings(path, func() *nsync.Client { return nil })
	fw := &fakeWatch{files: map[string]func(){}}
	pub := newPublished()
	s.watch, s.publish = fw.watch, pub.publish

	st := s.State(context.Background())
	if len(st.File) != 0 || st.Account != nil || st.SignedIn || st.Path != path || len(st.Warnings) != 0 {
		t.Fatalf("no file: %+v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.watchFile(ctx)
	if got := fw.watched(1); !slices.Equal(got, []string{path}) {
		t.Fatalf("watched %v", got)
	}

	if err := os.WriteFile(path, []byte("theme = \"dark\"\nfont = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.fire(path)
	st = pub.next(t)
	if st.File["theme"] != "dark" || len(st.File) != 1 || !slices.Equal(st.Warnings, []string{`settings.toml: unknown setting "font" is ignored`}) {
		t.Fatalf("after write: %+v", st)
	}
	// The same content again is not news.
	fw.fire(path)
	if err := os.WriteFile(path, []byte("theme = \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.fire(path)
	st = pub.next(t)
	if len(st.File) != 0 || len(st.Warnings) != 1 {
		t.Fatalf("broken file: %+v", st)
	}
	if n := len(pub.got); n != 2 {
		t.Fatalf("%d events, want 2", n)
	}
}

func TestSettingsFileSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir := t.TempDir()
	dotfiles := filepath.Join(dir, "dotfiles", "nota.toml")
	if err := os.MkdirAll(filepath.Dir(dotfiles), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dotfiles, []byte("palette = \"paper\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config", "nota", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, path); err != nil {
		t.Fatal(err)
	}
	s := newSettings(path, func() *nsync.Client { return nil })
	fw := &fakeWatch{files: map[string]func(){}}
	pub := newPublished()
	s.watch, s.publish = fw.watch, pub.publish
	if s.state().File["palette"] != "paper" {
		t.Fatalf("through the link: %+v", s.state())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.watchFile(ctx)
	real, _ := filepath.EvalSymlinks(dotfiles)
	if got := fw.watched(2); !slices.Contains(got, path) || !slices.Contains(got, real) {
		t.Fatalf("watched %v, want the link and its target", got)
	}
	// Editing the dotfiles copy is a change.
	if err := os.WriteFile(dotfiles, []byte("palette = \"forest\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.fire(real)
	if st := pub.next(t); st.File["palette"] != "forest" {
		t.Fatalf("after editing the target: %+v", st)
	}
}

// settingsServer is a real sync server with one user and two tokens.
func settingsServer(t *testing.T) (laptop, phone *nsync.Client) {
	t.Helper()
	ctx := context.Background()
	srv, err := server.New(ctx, server.Config{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		KeepAlive: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
		srv.Close()
	})
	u, err := srv.Store().AddUser(ctx, "anton@example.com")
	if err != nil {
		t.Fatal(err)
	}
	tok := func(device string) *nsync.Client {
		s, _, err := srv.Store().CreateToken(ctx, u.ID, device)
		if err != nil {
			t.Fatal(err)
		}
		return &nsync.Client{BaseURL: ts.URL, Token: s}
	}
	return tok("laptop"), tok("phone")
}

func TestSettingsAccount(t *testing.T) {
	laptop, phone := settingsServer(t)
	var signedIn atomic.Bool
	signedIn.Store(true)
	s := newSettings(filepath.Join(t.TempDir(), "settings.toml"), func() *nsync.Client {
		if signedIn.Load() {
			return laptop
		}
		return nil
	})
	pub := newPublished()
	s.publish, s.retry = pub.publish, 50*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st := s.State(ctx)
	if !st.SignedIn || st.Account == nil || len(st.Account) != 0 {
		t.Fatalf("new account: %+v", st)
	}
	if st, err := s.Update(ctx, SettingsPatch{Set: map[string]string{"theme": "dark"}}); err != nil || st.Account["theme"] != "dark" {
		t.Fatalf("Update = %+v, %v", st, err)
	}
	if st := pub.next(t); st.Account["theme"] != "dark" {
		t.Fatalf("event after Update: %+v", st)
	}
	for _, bad := range []map[string]string{{"theme": "sepia"}, {"font": "x"}} {
		if _, err := s.Update(ctx, SettingsPatch{Set: bad}); err == nil {
			t.Errorf("Update(%v) accepted it", bad)
		}
	}

	// Another device changes a setting: the stream brings it here.
	go s.followAccount(ctx)
	if _, err := phone.UpdateSettings(ctx, protocol.SettingsPatch{Set: map[string]string{"palette": "paper"}}); err != nil {
		t.Fatal(err)
	}
	for {
		st := pub.next(t)
		if st.Account["palette"] == "paper" {
			if st.Account["theme"] != "dark" || !st.SignedIn {
				t.Fatalf("after the phone's change: %+v", st)
			}
			break
		}
	}

	// Signing out forgets the account; changes stay on the device.
	signedIn.Store(false)
	s.accountChanged("signed-out")
	if st := s.State(ctx); st.SignedIn || st.Account != nil {
		t.Fatalf("signed out: %+v", st)
	}
	if _, err := s.Update(ctx, SettingsPatch{Set: map[string]string{"theme": "light"}}); err != ErrNotSignedIn {
		t.Fatalf("Update signed out: %v", err)
	}
	// Signing in again fetches the account's settings once more.
	signedIn.Store(true)
	s.accountChanged("signed-in")
	if st := s.State(ctx); st.Account["palette"] != "paper" {
		t.Fatalf("signed in again: %+v", st)
	}
}
