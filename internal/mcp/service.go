// Package mcp exposes a vault to AI agents over the Model Context
// Protocol. Service holds the tool logic as plain Go methods; NewServer
// registers them with the official SDK. The server does not know about
// its transport: `nota mcp` serves it over stdio, and `nota serve` can
// hand the same *sdk.Server to the SDK's streamable HTTP handler.
package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/banjo/nota/internal/edit"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
	"github.com/banjo/nota/internal/views"
)

// URIPrefix starts the URI of every note resource: nota://note/<path>,
// with the vault path percent-encoded segment by segment.
const URIPrefix = "nota://note/"

// Service runs the MCP tools against one vault and its index. It is safe
// for concurrent use; calls are serialized.
type Service struct {
	mu sync.Mutex
	v  *vault.Vault
	ix *index.Index
}

// NewService returns a service for v. ix is the vault's index; Service
// brings it up to date with the files before every call, so edits made
// outside (an editor, sync, the CLI) are seen.
func NewService(v *vault.Vault, ix *index.Index) *Service {
	return &Service{v: v, ix: ix}
}

// Errors the tools return. They reach the client as tool errors.
var (
	ErrAmbiguous = errors.New("ambiguous note reference")
	ErrConflict  = errors.New("note changed since it was read")
)

// NoteInfo describes a note without its content.
type NoteInfo struct {
	Path       string         `json:"path" jsonschema:"vault-relative path, the note's identity"`
	URI        string         `json:"uri" jsonschema:"resource URI of the note"`
	Folder     string         `json:"folder" jsonschema:"folder of the note, empty for the vault root"`
	Title      string         `json:"title"`
	Tags       []string       `json:"tags" jsonschema:"frontmatter and inline tags"`
	Properties map[string]any `json:"properties" jsonschema:"decoded frontmatter"`
	Created    string         `json:"created,omitempty" jsonschema:"RFC 3339"`
	Updated    string         `json:"updated,omitempty" jsonschema:"RFC 3339"`
	Modified   string         `json:"modified" jsonschema:"file modification time, RFC 3339"`
	Snippet    string         `json:"snippet,omitempty" jsonschema:"matching text, for search results"`
}

// Note is a note with its content.
type Note struct {
	NoteInfo
	Body    string `json:"body" jsonschema:"the markdown after the frontmatter"`
	Content string `json:"content" jsonschema:"the whole file"`
	Hash    string `json:"hash" jsonschema:"hash of the file; pass it to update_note as expected_hash"`
}

// URI returns the resource URI of a vault path.
func URI(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return URIPrefix + strings.Join(segs, "/")
}

// PathFromURI returns the vault path of a note resource URI.
func PathFromURI(uri string) (string, error) {
	rest, ok := strings.CutPrefix(uri, URIPrefix)
	if !ok {
		return "", fmt.Errorf("%w: not a note URI: %s", vault.ErrInvalidPath, uri)
	}
	p, err := url.PathUnescape(rest)
	if err != nil {
		return "", fmt.Errorf("%w: %v", vault.ErrInvalidPath, err)
	}
	return vault.NotePath(p)
}

// Hash is the content hash used for optimistic updates.
func Hash(raw []byte) string {
	h := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(h[:])
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func info(e index.Entry) NoteInfo {
	tags := e.Tags
	if tags == nil {
		tags = []string{}
	}
	props := e.Props
	if props == nil {
		props = map[string]any{}
	}
	return NoteInfo{
		Path: e.Path, URI: URI(e.Path), Folder: e.Folder, Title: e.Title, Tags: tags, Properties: props,
		Created: timeString(e.Created), Updated: timeString(e.Updated), Modified: timeString(e.Modified),
		Snippet: e.Snippet,
	}
}

func infos(es []index.Entry) []NoteInfo {
	out := make([]NoteInfo, len(es))
	for i, e := range es {
		out[i] = info(e)
	}
	return out
}

// sync brings the index up to date. The caller holds s.mu.
func (s *Service) sync() error {
	_, err := s.ix.Sync(s.v)
	return err
}

// resolve finds the note a reference points to: an existing path, else an
// exact title or file name, else a unique prefix. The index is in sync.
func (s *Service) resolve(ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("%w: give a note path or title", vault.ErrInvalidPath)
	}
	if strings.HasPrefix(ref, URIPrefix) {
		return PathFromURI(ref)
	}
	if p, err := vault.NotePath(ref); err == nil && s.v.Exists(p) {
		return p, nil
	}
	ms, err := s.ix.Lookup(ref)
	if err != nil {
		return "", err
	}
	switch len(ms) {
	case 0:
		return "", fmt.Errorf("no note matches %q: %w", ref, vault.ErrNotFound)
	case 1:
		return ms[0], nil
	}
	return "", fmt.Errorf("%w: %q matches %s; use the path", ErrAmbiguous, ref, strings.Join(ms, ", "))
}

// note reads a note with its indexed metadata. The index is in sync.
func (s *Service) note(p string) (Note, error) {
	n, err := s.v.Read(p)
	if err != nil {
		return Note{}, err
	}
	e, err := s.ix.Get(n.Path)
	if err != nil {
		// Ignored notes can be read by path but are not indexed.
		e = index.Entry{Path: n.Path, Folder: n.Folder(), Title: n.Title, Props: n.Props, Modified: n.ModTime, Size: n.Size}
	}
	return Note{NoteInfo: info(e), Body: n.Body, Content: string(n.Raw), Hash: Hash(n.Raw)}, nil
}

// SearchInput are the arguments of search_notes.
type SearchInput struct {
	Query  string `json:"query" jsonschema:"words to find; all must occur (the last as a prefix). \"quoted phrase\" matches exactly, -word excludes, OR between words means either"`
	Folder string `json:"folder,omitempty" jsonschema:"only notes in this folder and its subfolders"`
	Tag    string `json:"tag,omitempty" jsonschema:"only notes with this tag (nested tags included)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum number of results (default 20)"`
}

// NotesOutput is a list of notes.
type NotesOutput struct {
	Notes []NoteInfo `json:"notes"`
	Count int        `json:"count"`
}

// Search runs a full-text search.
func (s *Service) Search(in SearchInput) (NotesOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return NotesOutput{}, err
	}
	q := index.Query{Tag: in.Tag, Limit: in.Limit}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if in.Folder != "" {
		f, err := vault.CleanPath(in.Folder)
		if err != nil {
			return NotesOutput{}, err
		}
		q.Folder = f
	}
	es, err := s.ix.Search(in.Query, q)
	if err != nil {
		return NotesOutput{}, err
	}
	return NotesOutput{Notes: infos(es), Count: len(es)}, nil
}

// NoteInput names one note.
type NoteInput struct {
	Note string `json:"note" jsonschema:"the note: a vault path (Projekt/Plan.md; .md optional), a title, a unique prefix or a nota://note/ URI"`
}

// Read returns a note with its content.
func (s *Service) Read(in NoteInput) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return Note{}, err
	}
	p, err := s.resolve(in.Note)
	if err != nil {
		return Note{}, err
	}
	return s.note(p)
}

// ReadPath reads a note by exact vault path, for resources.
func (s *Service) ReadPath(p string) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return Note{}, err
	}
	return s.note(p)
}

// ListInput are the arguments of list_notes.
type ListInput struct {
	Folder string   `json:"folder,omitempty" jsonschema:"only notes in this folder and its subfolders"`
	Tag    string   `json:"tag,omitempty" jsonschema:"only notes with this tag (nested tags included)"`
	Where  []string `json:"where,omitempty" jsonschema:"frontmatter conditions, each key=value or key!=value (case-insensitive; list items match)"`
	View   string   `json:"view,omitempty" jsonschema:"start from a saved view (see list_views); the other filters narrow it"`
	Sort   string   `json:"sort,omitempty" jsonschema:"path, title, created, updated or modified, optionally followed by asc or desc"`
	Limit  int      `json:"limit,omitempty" jsonschema:"maximum number of notes (default 100)"`
}

// List lists notes by folder, tag, properties or view.
func (s *Service) List(in ListInput) (NotesOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return NotesOutput{}, err
	}
	var q index.Query
	if in.View != "" {
		vs, err := views.Load(s.v)
		if err != nil {
			return NotesOutput{}, err
		}
		vw, ok := views.Find(vs, in.View)
		if !ok {
			return NotesOutput{}, fmt.Errorf("no view %q; see list_views: %w", in.View, vault.ErrNotFound)
		}
		if q, err = vw.Query(); err != nil {
			return NotesOutput{}, err
		}
	}
	if in.Folder != "" {
		f, err := vault.CleanPath(in.Folder)
		if err != nil {
			return NotesOutput{}, err
		}
		q.Folder = f
	}
	if in.Tag != "" {
		q.Tag = in.Tag
	}
	for _, w := range in.Where {
		c, err := index.ParseCond(w)
		if err != nil {
			return NotesOutput{}, err
		}
		q.Where = append(q.Where, c)
	}
	if in.Sort != "" || q.Sort.Field == "" {
		srt, err := index.ParseSort(firstNonEmpty(in.Sort, "path"))
		if err != nil {
			return NotesOutput{}, err
		}
		q.Sort = srt
	}
	if in.Limit > 0 {
		q.Limit = in.Limit
	} else if q.Limit == 0 {
		q.Limit = 100
	}
	es, err := s.ix.Query(q)
	if err != nil {
		return NotesOutput{}, err
	}
	return NotesOutput{Notes: infos(es), Count: len(es)}, nil
}

// CreateInput are the arguments of create_note.
type CreateInput struct {
	Title      string         `json:"title,omitempty" jsonschema:"title; written as a '# Title' heading and used as the file name. Without it the first line of body is the title"`
	Body       string         `json:"body,omitempty" jsonschema:"markdown content"`
	Folder     string         `json:"folder,omitempty" jsonschema:"folder for the note; default the inbox, '/' for the vault root"`
	Tags       []string       `json:"tags,omitempty" jsonschema:"tags for the frontmatter, without #"`
	Properties map[string]any `json:"properties,omitempty" jsonschema:"extra frontmatter keys"`
}

// Create makes a new note and returns it. Hooks may have moved or edited
// it by the time it is returned; the result shows where it ended up.
func (s *Service) Create(in CreateInput) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title := strings.TrimSpace(in.Title)
	text := in.Body
	if title != "" {
		text = "# " + title + "\n"
		if in.Body != "" {
			text += "\n" + in.Body
		}
	}
	if strings.TrimSpace(text) == "" {
		return Note{}, fmt.Errorf("%w: give a title or a body", vault.ErrInvalidPath)
	}
	folder := s.v.Inbox()
	if in.Folder != "" {
		folder = in.Folder
	}
	content, err := edit.New(text, in.Tags, in.Properties, s.v.Now())
	if err != nil {
		return Note{}, err
	}
	where, stop := s.v.Bus().Follow()
	defer stop()
	n, err := s.v.Create(folder, content)
	if err != nil {
		return Note{}, err
	}
	p := where(n.Path)
	if err := s.sync(); err != nil {
		return Note{}, err
	}
	return s.note(p)
}

// AppendInput are the arguments of append_to_note.
type AppendInput struct {
	Note string `json:"note" jsonschema:"the note: a path, title, unique prefix or nota://note/ URI"`
	Text string `json:"text" jsonschema:"markdown to add at the end, on a line of its own; start with an empty line for a new paragraph"`
}

// Append adds text to the end of a note.
func (s *Service) Append(in AppendInput) (Note, error) {
	return s.modify(in.Note, "", func(raw []byte) ([]byte, error) {
		if in.Text == "" {
			return nil, fmt.Errorf("%w: text is empty", vault.ErrInvalidPath)
		}
		return edit.Append(raw, in.Text), nil
	})
}

// UpdateInput are the arguments of update_note.
type UpdateInput struct {
	Note         string `json:"note" jsonschema:"the note: a path, title, unique prefix or nota://note/ URI"`
	Body         string `json:"body" jsonschema:"the new markdown body; the frontmatter is kept"`
	ExpectedHash string `json:"expected_hash,omitempty" jsonschema:"hash from read_note; when given, the update fails if the note changed since"`
}

// Update replaces the body of a note, keeping its frontmatter.
func (s *Service) Update(in UpdateInput) (Note, error) {
	return s.modify(in.Note, in.ExpectedHash, func(raw []byte) ([]byte, error) {
		return edit.ReplaceBody(raw, in.Body), nil
	})
}

// FrontmatterInput are the arguments of update_frontmatter.
type FrontmatterInput struct {
	Note  string         `json:"note" jsonschema:"the note: a path, title, unique prefix or nota://note/ URI"`
	Set   map[string]any `json:"set,omitempty" jsonschema:"keys to set, with JSON values (strings, numbers, booleans, lists, objects)"`
	Unset []string       `json:"unset,omitempty" jsonschema:"keys to remove"`
}

// Frontmatter sets and removes frontmatter keys. Only those keys' lines
// (and updated) change.
func (s *Service) Frontmatter(in FrontmatterInput) (Note, error) {
	return s.modify(in.Note, "", func(raw []byte) ([]byte, error) {
		if len(in.Set) == 0 && len(in.Unset) == 0 {
			return nil, fmt.Errorf("%w: nothing to set or unset", vault.ErrInvalidPath)
		}
		return edit.Props(raw, in.Set, in.Unset)
	})
}

// modify applies fn to a note's content and saves the result.
func (s *Service) modify(ref, expected string, fn func([]byte) ([]byte, error)) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return Note{}, err
	}
	p, err := s.resolve(ref)
	if err != nil {
		return Note{}, err
	}
	n, err := s.v.Read(p)
	if err != nil {
		return Note{}, err
	}
	if expected != "" && expected != Hash(n.Raw) {
		return Note{}, fmt.Errorf("%w: %s has hash %s, not %s; read it again", ErrConflict, p, Hash(n.Raw), expected)
	}
	out, err := fn(n.Raw)
	if err != nil {
		return Note{}, err
	}
	if _, _, err := s.v.Save(p, out); err != nil {
		return Note{}, err
	}
	if err := s.sync(); err != nil {
		return Note{}, err
	}
	return s.note(p)
}

// MoveInput are the arguments of move_note.
type MoveInput struct {
	Note   string `json:"note" jsonschema:"the note: a path, title, unique prefix or nota://note/ URI"`
	Folder string `json:"folder" jsonschema:"target folder, created when missing; '/' for the vault root"`
}

// MoveOutput is the result of move_note.
type MoveOutput struct {
	From string `json:"from"`
	Path string `json:"path"`
	URI  string `json:"uri"`
}

// Move moves a note to another folder, keeping its file name.
func (s *Service) Move(in MoveInput) (MoveOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return MoveOutput{}, err
	}
	p, err := s.resolve(in.Note)
	if err != nil {
		return MoveOutput{}, err
	}
	where, stop := s.v.Bus().Follow()
	defer stop()
	to, err := s.v.Move(p, in.Folder)
	if err != nil {
		return MoveOutput{}, err
	}
	to = where(to)
	return MoveOutput{From: p, Path: to, URI: URI(to)}, nil
}

// TagsOutput is the result of list_tags.
type TagsOutput struct {
	Tags []index.TagCount `json:"tags"`
}

// Tags lists every tag with its note count.
func (s *Service) Tags() (TagsOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return TagsOutput{}, err
	}
	ts, err := s.ix.Tags()
	if err != nil {
		return TagsOutput{}, err
	}
	return TagsOutput{Tags: ts}, nil
}

// View is a saved view with its note count.
type View struct {
	ID     string         `json:"id" jsonschema:"pass as view to list_notes"`
	Name   string         `json:"name"`
	Where  map[string]any `json:"where,omitempty"`
	Tag    string         `json:"tag,omitempty"`
	Folder string         `json:"folder,omitempty"`
	Sort   string         `json:"sort,omitempty"`
	Count  int            `json:"count"`
}

// ViewsOutput is the result of list_views.
type ViewsOutput struct {
	Views []View `json:"views"`
}

// Views lists the saved views in .nota/views/.
func (s *Service) Views() (ViewsOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return ViewsOutput{}, err
	}
	vs, err := views.Load(s.v)
	if err != nil {
		return ViewsOutput{}, err
	}
	out := ViewsOutput{Views: []View{}}
	for _, vw := range vs {
		es, err := views.Eval(s.ix, vw)
		if err != nil {
			return ViewsOutput{}, err
		}
		out.Views = append(out.Views, View{ID: vw.ID, Name: vw.Name, Where: vw.Where, Tag: vw.Tag, Folder: vw.Folder, Sort: vw.Sort, Count: len(es)})
	}
	return out, nil
}

// BacklinksOutput is the result of get_backlinks.
type BacklinksOutput struct {
	Note      string       `json:"note"`
	Backlinks []index.Link `json:"backlinks"`
}

// Backlinks lists the notes linking to a note.
func (s *Service) Backlinks(in NoteInput) (BacklinksOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(); err != nil {
		return BacklinksOutput{}, err
	}
	p, err := s.resolve(in.Note)
	if err != nil {
		return BacklinksOutput{}, err
	}
	ls, err := s.ix.Backlinks(p)
	if errors.Is(err, vault.ErrNotFound) {
		ls, err = nil, nil // an ignored note has no indexed links
	}
	if err != nil {
		return BacklinksOutput{}, err
	}
	if ls == nil {
		ls = []index.Link{}
	}
	return BacklinksOutput{Note: p, Backlinks: ls}, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
