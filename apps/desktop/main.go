// Command desktop is the Nota desktop app. It serves the React frontend in
// the system webview and gives it the vault through bound Go services: the
// frontend never reads files itself. All vault IO goes through the same
// core packages as the nota CLI (internal/vault, internal/index).
package main

import (
	"log"
	"log/slog"
	"sync"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/hooks"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
	"github.com/egoist/mygo"
)

// Note is one markdown file in the vault.
type Note struct {
	Path  string   `json:"path"`
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
	Body  string   `json:"body"`
}

// Vault exposes the notes of one folder to the frontend.
type Vault struct {
	mu    sync.Mutex // bound methods run on their own goroutines
	v     *vault.Vault
	ix    *index.Index
	hooks *hooks.Runner // nil without .nota/hooks.toml
}

// openVault opens the vault and its persistent index (.nota/index.db),
// and keeps the index current on every write through the event bus.
func openVault(dir, inbox string) (*Vault, error) {
	bus := events.New()
	v, err := vault.Open(dir, vault.Options{Inbox: inbox, Create: true, Bus: bus})
	if err != nil {
		return nil, err
	}
	ix, err := index.OpenVault(v)
	if err != nil {
		return nil, err
	}
	s := &Vault{v: v, ix: ix}
	bus.Subscribe(func(e events.Event) {
		// Runs inside a locked method: the write that emitted the event.
		var err error
		switch e.Type {
		case events.NoteMoved:
			if err = ix.Remove(e.OldPath); err == nil {
				err = ix.Update(v, e.Path)
			}
		case events.NoteDeleted:
			err = ix.Remove(e.Path)
		default:
			err = ix.Update(v, e.Path)
		}
		if err != nil {
			slog.Warn("index update failed", "path", e.Path, "err", err)
		}
	}, events.NoteCreated, events.NoteSaved, events.NoteMoved, events.NoteDeleted)
	// Hooks run after the index has seen a change. After hooks never
	// block the UI; transform hooks run inside the write, as in the CLI.
	if cfg, err := hooks.Load(v); err != nil {
		slog.Warn("hooks disabled", "err", err)
	} else if len(cfg.Hooks) > 0 {
		s.hooks = hooks.New(v, cfg, hooks.Options{})
		s.hooks.Attach(bus)
	}
	return s, nil
}

// List returns every note in the vault, sorted by path. The index is
// brought up to date with the files first, so outside edits show up.
func (s *Vault) List() ([]Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.ix.Sync(s.v); err != nil {
		return nil, err
	}
	es, err := s.ix.Query(index.Query{})
	if err != nil {
		return nil, err
	}
	out := make([]Note, 0, len(es))
	for _, e := range es {
		n, err := s.v.Read(e.Path)
		if err != nil {
			continue // deleted since the sync
		}
		out = append(out, Note{Path: e.Path, Title: e.Title, Tags: e.Tags, Body: n.Body})
	}
	return out, nil
}

// Save writes a note's body back, keeping its frontmatter byte for byte
// apart from "updated", which the vault sets to now when the note has
// frontmatter.
func (s *Vault) Save(path, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.v.Read(path)
	if err != nil {
		return err
	}
	head := n.Raw[:len(n.Raw)-len(n.Body)]
	_, _, err = s.v.Save(n.Path, append(append([]byte{}, head...), body...))
	return err
}

// Search runs a full-text query over the vault and returns the paths of
// the matching notes, best match first. All words must match, the last
// one as a prefix.
func (s *Vault) Search(q string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.ix.Sync(s.v); err != nil {
		return nil, err
	}
	es, err := s.ix.Search(q, index.Query{})
	if err != nil {
		return nil, err
	}
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Path
	}
	return out, nil
}

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

func main() {
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
	mygo.Bind(s)
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:     "Nota",
			Width:     1200,
			Height:    780,
			MinWidth:  640,
			MinHeight: 420,
			StateKey:  "main",
			URL:       "/",
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
