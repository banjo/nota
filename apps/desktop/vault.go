package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/banjo/nota/internal/edit"
	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/hooks"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/plugins"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/relink"
	"github.com/banjo/nota/internal/vault"
	"github.com/banjo/nota/internal/views"
)

// The shapes below mirror the web API (docs/api.md), so the frontend's
// two NotesClient implementations return the same data.

// NoteInfo is a note in lists, the tree and search results: its
// metadata without the content.
type NoteInfo struct {
	Path       string         `json:"path"`
	Folder     string         `json:"folder"`
	Title      string         `json:"title"`
	Tags       []string       `json:"tags"`
	Properties map[string]any `json:"properties"`
	Created    *time.Time     `json:"created,omitempty"`
	Updated    *time.Time     `json:"updated,omitempty"`
	Modified   time.Time      `json:"modified"`
	Size       int64          `json:"size"`
	// Snippet is the matching text, for search results.
	Snippet string `json:"snippet,omitempty"`
}

// Link is a link between two notes. To is "" when the target does not
// resolve to a note.
type Link struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Target  string `json:"target"`
	Heading string `json:"heading,omitempty"`
	Alias   string `json:"alias,omitempty"`
}

// Note is one note with its content and links.
type Note struct {
	Path       string         `json:"path"`
	Folder     string         `json:"folder"`
	Title      string         `json:"title"`
	Tags       []string       `json:"tags"`
	Properties map[string]any `json:"properties"`
	Created    *time.Time     `json:"created,omitempty"`
	Updated    *time.Time     `json:"updated,omitempty"`
	Modified   time.Time      `json:"modified"`
	Size       int64          `json:"size"`
	// Version is the server version on the web; always 0 on the desktop,
	// where Hash is what saves are checked against.
	Version int64 `json:"version"`
	// Hash is the SHA-256 of the whole file (hex).
	Hash string `json:"hash"`
	// Body is the markdown after the frontmatter; Content the whole file.
	Body      string `json:"body"`
	Content   string `json:"content"`
	Backlinks []Link `json:"backlinks"`
	Links     []Link `json:"links"`
}

// TreeFolder is a folder with its note count (subfolders included);
// empty folders are listed with 0.
type TreeFolder struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Notes int    `json:"notes"`
}

// Tree is every folder and every note of the vault.
type Tree struct {
	Inbox   string       `json:"inbox"`
	Folders []TreeFolder `json:"folders"`
	Notes   []NoteInfo   `json:"notes"`
	Cursor  int64        `json:"cursor"`
}

// ListQuery filters a note list. Where holds "key=value" or
// "key!=value" conditions; Sort is "field [asc|desc]".
type ListQuery struct {
	Folder string   `json:"folder,omitempty"`
	Tag    string   `json:"tag,omitempty"`
	Where  []string `json:"where,omitempty"`
	View   string   `json:"view,omitempty"`
	Sort   string   `json:"sort,omitempty"`
	Limit  int      `json:"limit,omitempty"`
}

// TagCount is a tag with the number of notes that have it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ViewInfo is a saved view (.nota/views/*.yaml) with its note count.
type ViewInfo struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Icon   string         `json:"icon,omitempty"`
	Where  map[string]any `json:"where,omitempty"`
	Tag    string         `json:"tag,omitempty"`
	Folder string         `json:"folder,omitempty"`
	Sort   string         `json:"sort,omitempty"`
	File   string         `json:"file"`
	Count  int            `json:"count"`
}

// CreateRequest makes a new note. Folder defaults to the inbox; "/" is
// the vault root. The file name comes from the title (or first line).
type CreateRequest struct {
	Title      string         `json:"title,omitempty"`
	Body       string         `json:"body,omitempty"`
	Folder     string         `json:"folder,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// TagEdit adds and removes tags.
type TagEdit struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// PatchRequest edits frontmatter: only the touched lines change.
type PatchRequest struct {
	Set   map[string]any `json:"set,omitempty"`
	Unset []string       `json:"unset,omitempty"`
	Tags  *TagEdit       `json:"tags,omitempty"`
}

// SaveResult is a write checked against the hash the editor started
// from. On a conflict nothing was written and Current is the note as it
// is now.
type SaveResult struct {
	Note     *Note `json:"note,omitempty"`
	Conflict bool  `json:"conflict"`
	Current  *Note `json:"current,omitempty"`
}

// MoveResult is where a moved note (or folder) went, and the links in
// other notes that were rewritten to follow it.
type MoveResult struct {
	From  string        `json:"from"`
	Path  string        `json:"path"`
	Links relink.Result `json:"links"`
}

// HistoryEntry is one server version of a note.
type HistoryEntry struct {
	Path       string    `json:"path"`
	Version    int64     `json:"version"`
	Hash       string    `json:"hash"`
	Size       int64     `json:"size"`
	Deleted    bool      `json:"deleted"`
	Device     string    `json:"device"`
	ModifiedAt time.Time `json:"modifiedAt"`
	MovedFrom  string    `json:"movedFrom,omitempty"`
}

// TrashItem is a deleted note in the vault's .trash/.
type TrashItem struct {
	// ID is its path in the trash, which the other trash calls take.
	ID string `json:"id"`
	// Path is where it was deleted from.
	Path      string    `json:"path"`
	DeletedAt time.Time `json:"deletedAt"`
	Size      int64     `json:"size"`
}

// VaultInfo describes the open vault.
type VaultInfo struct {
	// Name is the folder name, Dir its full path.
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Inbox string `json:"inbox"`
}

// ErrNoHistory is returned for history without a sync remote: versions
// live on the server.
var ErrNoHistory = errors.New("history needs sync: sign in to sync in Settings")

// ErrNoVault is returned while the first-run setup has not chosen a
// vault: the window is open, but nothing is bound to read from.
var ErrNoVault = errors.New("no vault is open yet: finish the setup first")

// Vault exposes the notes of one folder to the frontend. Every method
// goes through internal/vault and the index, as the CLI does.
type Vault struct {
	mu      sync.Mutex // bound methods run on their own goroutines
	v       *vault.Vault
	ix      *index.Index
	bus     *events.Bus
	hooks   *hooks.Runner          // nil without .nota/hooks.toml
	syn     atomic.Pointer[syncer] // background sync; "off" without .nota/sync.toml
	changes *notifier              // tells the frontend about changes
}

// openVault opens the vault and its persistent index (.nota/index.db),
// and keeps the index current on every write through the event bus.
func openVault(dir, inbox string) (*Vault, error) {
	bus := events.New()
	v, err := vault.Open(dir, vault.Options{Inbox: inbox, Create: true, Bus: bus, Now: clock()})
	if err != nil {
		return nil, err
	}
	ix, err := index.OpenVault(v)
	if err != nil {
		return nil, err
	}
	if _, err := ix.Sync(v); err != nil {
		return nil, err
	}
	s := &Vault{v: v, ix: ix, bus: bus, changes: newNotifier()}
	s.syn.Store(offSyncer())
	bus.Subscribe(func(e events.Event) {
		// Runs inside a locked method: the write that emitted the event.
		if err := updateIndex(ix, v, e); err != nil {
			warn("index update failed", "path", e.Path, "err", err)
		}
		s.changes.add(e.Path, e.OldPath)
	}, events.NoteCreated, events.NoteSaved, events.NoteMoved, events.NoteDeleted)
	// Hooks run after the index has seen a change. After hooks never
	// block the UI; transform hooks run inside the write, as in the CLI.
	if cfg, err := hooks.Load(v); err != nil {
		warn("hooks disabled", "err", err)
	} else if len(cfg.Hooks) > 0 {
		s.hooks = hooks.New(v, cfg, hooks.Options{})
		s.hooks.Attach(bus)
	}
	return s, nil
}

// clock is the time written into notes: $NOTA_NOW (RFC 3339) when set,
// as in the CLI, else the real clock.
func clock() func() time.Time {
	if t, err := time.Parse(time.RFC3339, os.Getenv("NOTA_NOW")); err == nil {
		return func() time.Time { return t }
	}
	return time.Now
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

func noteInfo(e index.Entry) NoteInfo {
	if e.Tags == nil {
		e.Tags = []string{}
	}
	if e.Props == nil {
		e.Props = map[string]any{}
	}
	return NoteInfo{Path: e.Path, Folder: e.Folder, Title: e.Title, Tags: e.Tags, Properties: e.Props,
		Created: optTime(e.Created), Updated: optTime(e.Updated), Modified: e.Modified.UTC(), Size: e.Size, Snippet: e.Snippet}
}

func noteInfos(es []index.Entry) []NoteInfo {
	out := make([]NoteInfo, len(es))
	for i, e := range es {
		out[i] = noteInfo(e)
	}
	return out
}

func links(ls []index.Link) []Link {
	out := make([]Link, 0, len(ls))
	for _, l := range ls {
		out = append(out, Link{From: l.From, To: l.To, Target: l.Target, Heading: l.Heading, Alias: l.Alias})
	}
	return out
}

// note builds the full Note of a path. The caller holds s.mu.
func (s *Vault) note(p string) (Note, error) {
	n, err := s.v.Read(p)
	if err != nil {
		return Note{}, err
	}
	e, err := s.ix.Get(n.Path)
	if err != nil { // ignored notes are not indexed
		e = index.Entry{Path: n.Path, Folder: n.Folder(), Title: n.Title, Props: n.Props, Modified: n.ModTime, Size: n.Size}
	}
	i := noteInfo(e)
	out := Note{Path: i.Path, Folder: i.Folder, Title: i.Title, Tags: i.Tags, Properties: i.Properties,
		Created: i.Created, Updated: i.Updated, Modified: i.Modified, Size: i.Size,
		Hash: protocol.Hash(n.Raw), Body: n.Body, Content: string(n.Raw)}
	bl, err := s.ix.Backlinks(n.Path)
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		return Note{}, err
	}
	out.Backlinks = links(bl)
	ls, err := s.ix.Links(n.Path)
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		return Note{}, err
	}
	out.Links = links(ls)
	return out, nil
}

// Info describes the open vault.
func (s *Vault) Info() VaultInfo {
	if s.v == nil {
		return VaultInfo{}
	}
	return VaultInfo{Name: filepath.Base(s.v.Root()), Dir: s.v.Root(), Inbox: s.v.Inbox()}
}

// Tree returns every folder (empty ones too) and every note, by path.
func (s *Vault) Tree() (Tree, error) {
	if s.v == nil {
		return Tree{}, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	es, err := s.ix.Query(index.Query{Sort: index.Sort{Field: "path"}})
	if err != nil {
		return Tree{}, err
	}
	counts := map[string]int{}
	for _, e := range es {
		for f := e.Folder; f != ""; f = vault.Folder(f) {
			counts[f]++
		}
	}
	dirs, err := s.v.Folders()
	if err != nil {
		return Tree{}, err
	}
	for _, d := range dirs {
		for f := d; f != ""; f = vault.Folder(f) {
			if _, ok := counts[f]; !ok {
				counts[f] = 0
			}
		}
	}
	out := Tree{Inbox: s.v.Inbox(), Folders: []TreeFolder{}, Notes: noteInfos(es), Cursor: s.changes.seq()}
	for f, n := range counts {
		out.Folders = append(out.Folders, TreeFolder{Path: f, Name: path.Base(f), Notes: n})
	}
	slices.SortFunc(out.Folders, func(a, b TreeFolder) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

// query turns a ListQuery into an index query. The caller holds s.mu.
func (s *Vault) query(lq ListQuery) (index.Query, error) {
	var q index.Query
	if lq.View != "" {
		vs, err := views.Load(s.v)
		if err != nil {
			return q, err
		}
		vw, ok := views.Find(vs, lq.View)
		if !ok {
			return q, fmt.Errorf("no view %q", lq.View)
		}
		if q, err = vw.Query(); err != nil {
			return q, fmt.Errorf("view %s: %w", lq.View, err)
		}
	}
	if lq.Folder != "" {
		c, err := vault.CleanPath(lq.Folder)
		if err != nil {
			return q, err
		}
		q.Folder = c
	}
	if lq.Tag != "" {
		q.Tag = lq.Tag
	}
	for _, w := range lq.Where {
		c, err := index.ParseCond(w)
		if err != nil {
			return q, err
		}
		q.Where = append(q.Where, c)
	}
	if lq.Sort != "" || q.Sort.Field == "" {
		so, err := index.ParseSort(lq.Sort)
		if err != nil {
			return q, err
		}
		q.Sort = so
	}
	if lq.Limit > 0 {
		q.Limit = lq.Limit
	}
	return q, nil
}

// List returns the notes matching a query: a folder (with subfolders),
// a tag, property conditions or a saved view.
func (s *Vault) List(q ListQuery) ([]NoteInfo, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iq, err := s.query(q)
	if err != nil {
		return nil, err
	}
	es, err := s.ix.Query(iq)
	if err != nil {
		return nil, err
	}
	return noteInfos(es), nil
}

// Inbox returns the notes in the inbox folder, newest first.
func (s *Vault) Inbox() ([]NoteInfo, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	es, err := s.ix.Query(index.Query{Folder: s.v.Inbox(), Sort: index.Sort{Field: "created", Desc: true}})
	if err != nil {
		return nil, err
	}
	return noteInfos(es), nil
}

// Search runs a full-text query, best match first, with snippets. All
// words must match, the last one as a prefix.
func (s *Vault) Search(q string, filter ListQuery) ([]NoteInfo, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iq, err := s.query(filter)
	if err != nil {
		return nil, err
	}
	if filter.Sort == "" {
		iq.Sort = index.Sort{}
	}
	es, err := s.ix.Search(q, iq)
	if errors.Is(err, index.ErrEmptyQuery) {
		return []NoteInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	return noteInfos(es), nil
}

// Tags returns every tag with its note count.
func (s *Vault) Tags() ([]TagCount, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ts, err := s.ix.Tags()
	if err != nil {
		return nil, err
	}
	out := make([]TagCount, 0, len(ts))
	for _, t := range ts {
		out = append(out, TagCount{Tag: t.Tag, Count: t.Count})
	}
	return out, nil
}

// Views returns the saved views of .nota/views with their note counts.
func (s *Vault) Views() ([]ViewInfo, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs, err := views.Load(s.v)
	if err != nil {
		return nil, err
	}
	out := []ViewInfo{}
	for _, vw := range vs {
		es, err := views.Eval(s.ix, vw)
		if err != nil {
			return nil, fmt.Errorf("view %s: %w", vw.ID, err)
		}
		out = append(out, ViewInfo{ID: vw.ID, Name: vw.Name, Icon: vw.Icon, Where: vw.Where, Tag: vw.Tag,
			Folder: vw.Folder, Sort: vw.Sort, File: vw.File, Count: len(es)})
	}
	return out, nil
}

// Plugins returns the plugins of .nota/plugins, broken ones with their
// error.
func (s *Vault) Plugins() ([]plugins.Plugin, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return plugins.Load(s.v)
}

// Get returns one note with its content, hash, links and backlinks.
func (s *Vault) Get(path string) (Note, error) {
	if s.v == nil {
		return Note{}, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.note(path)
}

// conflict reports whether the note at p no longer has the hash base.
// An empty base skips the check. The caller holds s.mu.
func (s *Vault) conflict(p, base string) (*Note, bool, error) {
	if base == "" {
		return nil, false, nil
	}
	cur, err := s.note(p)
	if err != nil {
		return nil, false, err
	}
	if cur.Hash != base {
		return &cur, true, nil
	}
	return nil, false, nil
}

// Save writes a note's body back, keeping its frontmatter byte for byte
// apart from "updated", which is set to now when the note has
// frontmatter. baseHash is the hash the editor started from: when the
// file changed since, nothing is written and the result is a conflict
// with the current note. An empty baseHash overwrites.
func (s *Vault) Save(path, body, baseHash string) (SaveResult, error) {
	if s.v == nil {
		return SaveResult{}, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, c, err := s.conflict(path, baseHash); err != nil || c {
		return SaveResult{Conflict: c, Current: cur}, err
	}
	n, err := s.v.Read(path)
	if err != nil {
		return SaveResult{}, err
	}
	if _, _, err := s.v.Save(n.Path, edit.ReplaceBody(n.Raw, body)); err != nil {
		return SaveResult{}, err
	}
	out, err := s.note(n.Path)
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Note: &out}, nil
}

// Patch edits a note's frontmatter (set and unset keys, add and remove
// tags); only the touched lines change. baseHash works as in Save.
func (s *Vault) Patch(path string, req PatchRequest, baseHash string) (SaveResult, error) {
	if s.v == nil {
		return SaveResult{}, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, c, err := s.conflict(path, baseHash); err != nil || c {
		return SaveResult{Conflict: c, Current: cur}, err
	}
	n, err := s.v.Read(path)
	if err != nil {
		return SaveResult{}, err
	}
	src := n.Raw
	if len(req.Set) > 0 || len(req.Unset) > 0 {
		if src, err = edit.Props(src, req.Set, req.Unset); err != nil {
			return SaveResult{}, err
		}
	}
	if req.Tags != nil && (len(req.Tags.Add) > 0 || len(req.Tags.Remove) > 0) {
		if src, _, err = edit.Tags(src, req.Tags.Add, req.Tags.Remove); err != nil {
			return SaveResult{}, err
		}
	}
	if _, _, err := s.v.Save(n.Path, src); err != nil {
		return SaveResult{}, err
	}
	out, err := s.note(n.Path)
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Note: &out}, nil
}

// Create writes a new note, by default into the inbox. Transform hooks
// may file it elsewhere: the result shows where it ended up.
func (s *Vault) Create(req CreateRequest) (Note, error) {
	if s.v == nil {
		return Note{}, ErrNoVault
	}
	title := strings.TrimSpace(req.Title)
	text := req.Body
	if title != "" {
		text = "# " + title + "\n"
		if req.Body != "" {
			text += "\n" + req.Body
		}
	}
	if strings.TrimSpace(text) == "" {
		return Note{}, errors.New("give a title or a body")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	content, err := edit.New(text, req.Tags, req.Properties, s.v.Now())
	if err != nil {
		return Note{}, err
	}
	folder := s.v.Inbox()
	switch req.Folder {
	case "":
	case "/":
		folder = ""
	default:
		folder = req.Folder
	}
	where, stop := s.bus.Follow()
	defer stop()
	n, err := s.v.Create(folder, content)
	if err != nil {
		return Note{}, err
	}
	return s.note(where(n.Path))
}

// CreateFolder makes an empty folder (and missing parents) and returns
// its path. It fails when the folder exists, differs from one only in
// letter case, or has a name that is not allowed. Empty folders are
// local until they hold a note (docs/sync.md).
func (s *Vault) CreateFolder(path string) (string, error) {
	if s.v == nil {
		return "", ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.v.Mkdir(path)
	if err != nil {
		return "", err
	}
	s.changes.add(p)
	return p, nil
}

// Move moves a note into folder, keeping its file name ("/" or "" is
// the vault root).
func (s *Vault) Move(path, folder string) (MoveResult, error) {
	if s.v == nil {
		return MoveResult{}, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if folder == "/" {
		folder = ""
	}
	clean, err := vault.CleanPath(folder)
	if err != nil {
		return MoveResult{}, err
	}
	from, err := vault.NotePath(path)
	if err != nil {
		return MoveResult{}, err
	}
	return s.move(from, intoFolder(clean, from))
}

// move renames a note and rewrites the links to it. The caller holds s.mu.
func (s *Vault) move(from, to string) (MoveResult, error) {
	pending, err := relink.Prepare(s.v, s.ix, map[string]string{from: to})
	if err != nil {
		return MoveResult{}, err
	}
	where, stop := s.bus.Follow()
	defer stop()
	p, err := s.v.Rename(from, to)
	if err != nil {
		return MoveResult{}, err
	}
	p = where(p)
	links, err := pending.Apply(map[string]string{from: p})
	if err != nil {
		return MoveResult{}, err
	}
	return MoveResult{From: from, Path: p, Links: links}, nil
}

// RenameFolder renames a folder, or moves it, to a new vault path: every
// file in it moves with it, one note.moved event per note.
func (s *Vault) RenameFolder(path, to string) (MoveResult, error) {
	if s.v == nil {
		return MoveResult{}, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var pending *relink.Pending
	from, err := vault.CleanPath(path)
	if err != nil {
		return MoveResult{}, err
	}
	if planned, err := vault.CleanPath(to); err == nil && from != "" {
		if pending, err = relink.PrepareFolder(s.v, s.ix, from, planned); err != nil {
			return MoveResult{}, err
		}
	}
	p, err := s.v.RenameFolder(path, to)
	if err != nil {
		return MoveResult{}, err
	}
	s.changes.add(path, p)
	res := MoveResult{From: path, Path: p, Links: relink.Result{Notes: []relink.Changed{}}}
	if pending != nil {
		if res.Links, err = pending.Apply(nil); err != nil {
			return MoveResult{}, err
		}
	}
	return res, nil
}

// Rename gives a note a new path (a new name, a new folder, or both).
func (s *Vault) Rename(path, to string) (MoveResult, error) {
	if s.v == nil {
		return MoveResult{}, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	from, err := vault.NotePath(path)
	if err != nil {
		return MoveResult{}, err
	}
	dest, err := vault.NotePath(to)
	if err != nil {
		return MoveResult{}, err
	}
	return s.move(from, dest)
}

// Delete moves a note to .trash/ and returns where it went.
func (s *Vault) Delete(path string) (string, error) {
	if s.v == nil {
		return "", ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.Delete(path)
}

// Trash lists the deleted notes, most recently deleted first.
func (s *Vault) Trash() ([]TrashItem, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.v.Trash()
	if err != nil {
		return nil, err
	}
	out := make([]TrashItem, 0, len(items))
	for _, it := range items {
		if vault.IsNote(it.Path) {
			out = append(out, TrashItem{ID: it.Path, Path: it.From, DeletedAt: it.DeletedAt, Size: it.Size})
		}
	}
	return out, nil
}

// TrashNote reads a deleted note.
func (s *Vault) TrashNote(id string) (string, error) {
	if s.v == nil {
		return "", ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.v.ReadTrash(id)
	return string(data), err
}

// RestoreTrash puts a deleted note back where it was deleted from, or
// into folder ("/" is the root), and returns its path. It refuses to
// overwrite a note that has taken the path.
func (s *Vault) RestoreTrash(id, folder string) (string, error) {
	if s.v == nil {
		return "", ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	to := ""
	if folder != "" {
		it, err := s.v.FindTrash(id)
		if err != nil {
			return "", err
		}
		f, err := vault.CleanPath(folder)
		if err != nil {
			return "", err
		}
		to = path.Join(f, path.Base(it.From))
	}
	return s.v.RestoreTrash(id, to)
}

// PurgeTrash deletes a note in the trash for good.
func (s *Vault) PurgeTrash(id string) error {
	if s.v == nil {
		return ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.PurgeTrash(id)
}

// EmptyTrash deletes everything in the trash for good and returns how
// many files it deleted.
func (s *Vault) EmptyTrash() (int, error) {
	if s.v == nil {
		return 0, ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.EmptyTrash(time.Time{})
}

// History lists the server versions of a note, newest first. Versions
// live on the sync server, so it needs sync.
func (s *Vault) History(ctx context.Context, path string) ([]HistoryEntry, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	c := s.syn.Load().client()
	if c == nil {
		return nil, ErrNoHistory
	}
	h, err := c.History(ctx, path)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryEntry, 0, len(h.Versions))
	for _, e := range h.Versions {
		out = append(out, HistoryEntry{Path: e.Path, Version: e.Version, Hash: e.Hash, Size: e.Size,
			Deleted: e.Deleted, Device: e.DeviceID, ModifiedAt: e.ModifiedAt, MovedFrom: e.MovedFrom})
	}
	return out, nil
}

// HistoryVersion returns the content of one server version of a note;
// nil when that version is a delete.
func (s *Vault) HistoryVersion(ctx context.Context, path string, version int64) (*string, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	c := s.syn.Load().client()
	if c == nil {
		return nil, ErrNoHistory
	}
	_, content, err := c.HistoryVersion(ctx, path, version)
	return content, err
}

// Restore makes an old server version of a note its newest one; the
// background sync brings it to this vault (the server nudges it).
func (s *Vault) Restore(ctx context.Context, path string, version int64) error {
	if s.v == nil {
		return ErrNoVault
	}
	c := s.syn.Load().client()
	if c == nil {
		return ErrNoHistory
	}
	_, err := c.Restore(ctx, path, version)
	return err
}

// SyncStatus returns the state of the background sync. Updates also
// arrive as "syncStatus" events after every pass.
func (s *Vault) SyncStatus() SyncStatus {
	if s.v == nil {
		return SyncStatus{State: "off", Conflicts: []string{}, Time: time.Now()}
	}
	return s.syn.Load().get()
}

// intoFolder is the path of the note p moved into folder.
func intoFolder(folder, p string) string { return path.Join(folder, path.Base(p)) }
