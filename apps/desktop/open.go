package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/edit"
	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/vault"
)

// Files opened with the app (Finder's Open With, a file dropped on the
// Dock icon, `open -a Nota x.md`, `nota open --app`, a path on the
// command line, File ▸ Open…) arrive at Opener. A note of the vault opens
// as that note; any other Markdown file opens as a read-only preview the
// frontend may turn into a draft. Nothing is written, to the original or
// to the vault, until the user saves it to the vault, moves it in, or
// overwrites the original with an explicit command.
//
// The frontend can only read files the OS handed the app (or that a
// relative link in one of them names): they are known by opaque ids that
// live as long as the process.

// MaxOpenSize is the largest file the app previews.
const MaxOpenSize = 5 << 20

// sniffSize is how much of a file is checked for NUL bytes.
const sniffSize = 8 << 10

// openedEvent nudges the main window's page: files were opened, take them.
var openedEvent = mygo.NewEvent[OpenedNudge]("opened")

// externalChangedEvent says an outside file changed on disk: read it again.
var externalChangedEvent = mygo.NewEvent[ExternalChanged]("externalChanged")

// ExternalChanged names the outside file that changed on disk.
type ExternalChanged struct {
	ID string `json:"id"`
}

// OpenedNudge says there are opened files to take (Opener.Take).
type OpenedNudge struct {
	Seq int64 `json:"seq"`
}

// Opened is a file the OS opened with the app.
type Opened struct {
	// ID names an outside file for Read, Save and the rest; "" for notes.
	ID string `json:"id"`
	// Kind is "note" (a note of the vault, see NotePath), "external" (a
	// preview) or "error" (see Error).
	Kind     string `json:"kind"`
	NotePath string `json:"notePath,omitempty"`
	// Name is the file name, Dir its folder for display (~ for home).
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Error string `json:"error,omitempty"`
}

// ExternalDoc is the content of an outside file.
type ExternalDoc struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Title string `json:"title"`
	// Content is the whole file, Body what follows its frontmatter.
	Content    string         `json:"content"`
	Body       string         `json:"body"`
	Properties map[string]any `json:"properties"`
	Modified   time.Time      `json:"modified"`
	Size       int64          `json:"size"`
	// Hash is the SHA-256 of the file as read; saves check it.
	Hash string `json:"hash"`
	// UTF8 is false when the file is not valid UTF-8: it is shown with
	// replacement characters and cannot be saved.
	UTF8 bool `json:"utf8"`
}

// SaveRequest saves an outside file into the vault.
type SaveRequest struct {
	// Folder is the vault folder; "" for the inbox, "/" for the root.
	Folder string `json:"folder"`
	// Edited says Body is a draft that replaces the file's body (its
	// frontmatter stays byte for byte). Otherwise the file's bytes are
	// copied exactly, and only if they still have Hash.
	Edited bool   `json:"edited"`
	Body   string `json:"body"`
	Hash   string `json:"hash"`
	// Move sends the original to the Trash once the note is written. It
	// needs Hash to match the file, edited or not.
	Move bool `json:"move"`
}

// SaveOutcome is where a saved file went.
type SaveOutcome struct {
	// Conflict says the file changed since Hash: nothing was written.
	Conflict bool `json:"conflict"`
	// Path is the new note's vault path.
	Path string `json:"path,omitempty"`
	// TrashError says the note was written but the original could not
	// go to the Trash.
	TrashError string `json:"trashError,omitempty"`
}

// openDoc is an outside file the app knows.
type openDoc struct {
	id   string
	path string // absolute, symlinks resolved
	name string
	dir  string
	raw  []byte // as last read
	stop func() // ends the watch on the file
}

// openerEnv is what Opener needs from the app; tests replace it.
type openerEnv struct {
	// locate maps a file onto a vault note ("" when it is none, or no
	// vault is open).
	locate func(file string) string
	// imp writes a new note (folder as in SaveRequest).
	imp    func(folder, name string, data []byte) (string, error)
	nudge  func(seq int64)
	show   func()
	trash  func(file string) error
	reveal func(file string)
	// watch calls changed whenever file changes on disk, until ctx ends.
	watch func(ctx context.Context, file string, changed func())
	// changed tells the page an outside file changed (externalChanged).
	changed func(id string)
	home    string
}

// Opener receives files opened with the app and serves them to the
// frontend.
type Opener struct {
	env openerEnv

	mu    sync.Mutex
	queue []Opened
	docs  map[string]*openDoc
	order []string // ids, oldest first
	seq   int64
	// noted keeps the files of vault notes still in the queue, for a
	// relaunch.
	noted map[string]string

	in chan string
}

func newOpener(env openerEnv) *Opener {
	// Opened files are compared by their resolved path (check), so the
	// home folder must be too: on macOS /var is /private/var.
	if real, err := filepath.EvalSymlinks(env.home); env.home != "" && err == nil {
		env.home = real
	}
	o := &Opener{env: env, docs: map[string]*openDoc{}, noted: map[string]string{}, in: make(chan string, 256)}
	go func() {
		for p := range o.in {
			o.add(p)
		}
	}()
	return o
}

// receive is App.OnOpenFile's target: it runs on the main thread, so the
// file is looked at in the background, in the order files came.
func (o *Opener) receive(file string) {
	select {
	case o.in <- file:
	default:
		go func() { o.in <- file }()
	}
}

// add classifies one file, queues it and tells the page.
func (o *Opener) add(file string) {
	op := o.classify(file)
	slog.Info("opened file", "kind", op.Kind)
	slog.Debug("opened file", "path", file, "kind", op.Kind, "error", op.Error)
	o.mu.Lock()
	o.queue = append(o.queue, op)
	if op.Kind == "note" {
		o.noted[op.NotePath] = file
	}
	o.seq++
	seq := o.seq
	o.mu.Unlock()
	if o.env.show != nil {
		o.env.show()
	}
	if o.env.nudge != nil {
		o.env.nudge(seq)
	}
}

// display shortens a folder for the banner: ~ for the home folder.
func (o *Opener) display(dir string) string {
	if h := o.env.home; h != "" {
		if dir == h {
			return "~"
		}
		if r, err := filepath.Rel(h, dir); err == nil && !strings.HasPrefix(r, "..") {
			return "~" + string(filepath.Separator) + r
		}
	}
	return dir
}

// check opens a file for reading under the limits: Markdown, regular,
// not too large, not binary. It returns the resolved path.
func check(file string) (string, os.FileInfo, error) {
	if !vault.IsMarkdown(file) {
		return "", nil, errors.New("Nota opens Markdown files (.md, .markdown) only.")
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, errors.New("The file isn't there anymore.")
		}
		return "", nil, fmt.Errorf("Couldn't open the file: %w", err)
	}
	info, err := os.Stat(real)
	switch {
	case err != nil:
		return "", nil, fmt.Errorf("Couldn't open the file: %w", err)
	case !info.Mode().IsRegular():
		return "", nil, errors.New("That isn't a regular file.")
	case info.Size() > MaxOpenSize:
		return "", nil, fmt.Errorf("The file is too large to preview (%.1f MB; the limit is %d MB).",
			float64(info.Size())/(1<<20), MaxOpenSize>>20)
	}
	return real, info, nil
}

// readLimited reads a checked file, refusing binary content.
func readLimited(real string) ([]byte, error) {
	f, err := os.Open(real)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("The file isn't there anymore.")
		}
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxOpenSize+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxOpenSize {
		return nil, fmt.Errorf("The file is too large to preview (the limit is %d MB).", MaxOpenSize>>20)
	}
	if bytes.IndexByte(raw[:min(len(raw), sniffSize)], 0) >= 0 {
		return nil, errors.New("This doesn't look like a text file.")
	}
	return raw, nil
}

// classify turns a file into an Opened, registering outside files.
func (o *Opener) classify(file string) Opened {
	name := vault.NFC(filepath.Base(file))
	real, info, err := check(file)
	if err != nil {
		return Opened{Kind: "error", Name: name, Dir: o.display(filepath.Dir(file)), Error: err.Error()}
	}
	if _, err := readLimited(real); err != nil {
		return Opened{Kind: "error", Name: name, Dir: o.display(filepath.Dir(real)), Error: err.Error()}
	}
	if o.env.locate != nil {
		if rel := o.env.locate(real); rel != "" {
			return Opened{Kind: "note", NotePath: rel, Name: name, Dir: o.display(filepath.Dir(real))}
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, id := range o.order {
		d := o.docs[id]
		if st, err := os.Stat(d.path); err == nil && os.SameFile(st, info) {
			return Opened{ID: d.id, Kind: "external", Name: d.name, Dir: d.dir}
		}
	}
	d := &openDoc{id: newID(), path: real, name: name, dir: o.display(filepath.Dir(real)), stop: func() {}}
	if o.env.watch != nil && o.env.changed != nil {
		ctx, cancel := context.WithCancel(context.Background())
		d.stop = cancel
		id := d.id
		go o.env.watch(ctx, real, func() { o.env.changed(id) })
	}
	o.docs[d.id] = d
	o.order = append(o.order, d.id)
	return Opened{ID: d.id, Kind: "external", Name: d.name, Dir: d.dir}
}

func newID() string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (o *Opener) doc(id string) (*openDoc, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	d, ok := o.docs[id]
	if !ok {
		return nil, errors.New("This preview was closed. Open the file again.")
	}
	return d, nil
}

// Take returns the files opened since the last call, oldest first. The
// page calls it when it loads and on every "opened" event.
func (o *Opener) Take() []Opened {
	o.mu.Lock()
	defer o.mu.Unlock()
	q := o.queue
	o.queue = nil
	for _, op := range q {
		if op.Kind == "note" {
			delete(o.noted, op.NotePath)
		}
	}
	if q == nil {
		q = []Opened{}
	}
	return q
}

// Previews lists the outside files open now, oldest first, so a page
// that loads again finds them.
func (o *Opener) Previews() []Opened {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Opened, 0, len(o.order))
	for _, id := range o.order {
		d := o.docs[id]
		out = append(out, Opened{ID: d.id, Kind: "external", Name: d.name, Dir: d.dir})
	}
	return out
}

// Read returns the content of an outside file as it is now.
func (o *Opener) Read(id string) (ExternalDoc, error) {
	d, err := o.doc(id)
	if err != nil {
		return ExternalDoc{}, err
	}
	if _, _, err := check(d.path); err != nil {
		return ExternalDoc{}, err
	}
	raw, err := readLimited(d.path)
	if err != nil {
		return ExternalDoc{}, err
	}
	info, err := os.Stat(d.path)
	if err != nil {
		return ExternalDoc{}, err
	}
	o.mu.Lock()
	d.raw = raw
	o.mu.Unlock()
	return externalDoc(d, raw, info), nil
}

func externalDoc(d *openDoc, raw []byte, info os.FileInfo) ExternalDoc {
	ok := utf8.Valid(raw)
	text := raw
	if !ok {
		text = []byte(strings.ToValidUTF8(string(raw), "\uFFFD"))
	}
	fm := frontmatter.Split(text)
	body := strings.TrimPrefix(string(fm.Body()), "\uFEFF")
	props, err := fm.Properties()
	if err != nil || props == nil {
		props = map[string]any{}
	}
	stem := strings.TrimSuffix(d.name, path.Ext(d.name))
	return ExternalDoc{
		ID: d.id, Name: d.name, Dir: d.dir,
		Title:   vault.Title([]byte(body), stem),
		Content: string(text), Body: body, Properties: props,
		Modified: info.ModTime().UTC(), Size: info.Size(),
		Hash: protocol.Hash(raw), UTF8: ok,
	}
}

// Save copies an outside file (or the draft made from it) into the vault
// as a new note named after the file. With Move set, the original then
// goes to the Trash. The original is never written.
func (o *Opener) Save(id string, req SaveRequest) (SaveOutcome, error) {
	if o.env.imp == nil {
		return SaveOutcome{}, ErrNoVault
	}
	d, err := o.doc(id)
	if err != nil {
		return SaveOutcome{}, err
	}
	raw, err := readLimited(d.path)
	if err != nil {
		// The original is gone (or unreadable) but the draft made from it
		// is here: it can still be saved, just not moved.
		o.mu.Lock()
		base := d.raw
		o.mu.Unlock()
		if !req.Edited || req.Move || base == nil {
			return SaveOutcome{}, err
		}
		raw = base
	}
	changed := req.Hash != "" && protocol.Hash(raw) != req.Hash
	if (!req.Edited || req.Move) && (changed || (req.Move && req.Hash == "")) {
		return SaveOutcome{Conflict: true}, nil
	}
	data := raw
	if req.Edited {
		o.mu.Lock()
		base := d.raw
		o.mu.Unlock()
		if base == nil {
			base = raw
		}
		data = edit.ReplaceBody(base, req.Body)
	}
	if !utf8.Valid(data) {
		return SaveOutcome{}, errors.New("The file isn't UTF-8 text, so Nota can't save it to the vault.")
	}
	p, err := o.env.imp(req.Folder, d.name, data)
	if err != nil {
		return SaveOutcome{}, err
	}
	out := SaveOutcome{Path: p}
	if req.Move {
		if err := o.env.trash(d.path); err != nil {
			out.TrashError = err.Error()
		}
	}
	o.Dismiss(id)
	return out, nil
}

// OverwriteOriginal writes the draft's body back into the original file,
// keeping its frontmatter byte for byte. It is the one way the app
// writes a file outside the vault, and only when the file still has
// hash; otherwise nothing is written and the result is a conflict.
func (o *Opener) OverwriteOriginal(id, body, hash string) (SaveOutcome, error) {
	d, err := o.doc(id)
	if err != nil {
		return SaveOutcome{}, err
	}
	raw, err := readLimited(d.path)
	if err != nil {
		return SaveOutcome{}, err
	}
	if hash == "" || protocol.Hash(raw) != hash {
		return SaveOutcome{Conflict: true}, nil
	}
	if !utf8.Valid(raw) {
		return SaveOutcome{}, errors.New("The file isn't UTF-8 text, so Nota won't write to it.")
	}
	data := edit.ReplaceBody(raw, body)
	if err := writeInPlace(d.path, data); err != nil {
		return SaveOutcome{}, err
	}
	o.mu.Lock()
	d.raw = data
	o.mu.Unlock()
	return SaveOutcome{}, nil
}

// writeInPlace replaces a file's content through a temp file and a
// rename, keeping its permissions.
func writeInPlace(file string, data []byte) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".nota-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	_ = f.Chmod(info.Mode().Perm())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, file)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// Reveal shows the original file in the file manager.
func (o *Opener) Reveal(id string) error {
	d, err := o.doc(id)
	if err != nil {
		return err
	}
	if o.env.reveal != nil {
		o.env.reveal(d.path)
	}
	return nil
}

// Dismiss forgets an outside file.
func (o *Opener) Dismiss(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if d, ok := o.docs[id]; ok {
		d.stop()
	}
	delete(o.docs, id)
	for i, x := range o.order {
		if x == id {
			o.order = append(o.order[:i], o.order[i+1:]...)
			break
		}
	}
}

// Follow opens the Markdown file a relative link in an outside file
// names, as Finder would: a note of the vault, or another preview.
// Anything else (other schemes, absolute paths, other file types) is
// refused.
func (o *Opener) Follow(id, href string) (Opened, error) {
	d, err := o.doc(id)
	if err != nil {
		return Opened{}, err
	}
	target, err := linkTarget(href)
	if err != nil {
		return Opened{}, err
	}
	op := o.classify(filepath.Join(filepath.Dir(d.path), filepath.FromSlash(target)))
	if op.Kind == "error" {
		return Opened{}, errors.New(op.Error)
	}
	return op, nil
}

// linkTarget is the relative file path a link names.
func linkTarget(href string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" {
		return "", errors.New("Only relative links to Markdown files open from a preview.")
	}
	p := u.Path
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || filepath.IsAbs(p) {
		return "", errors.New("Only relative links to Markdown files open from a preview.")
	}
	if !vault.IsMarkdown(p) {
		return "", errors.New("Only links to Markdown files open from a preview.")
	}
	return p, nil
}

// PickFile shows the system's file chooser for Markdown files and opens
// the chosen ones as if the OS had (File ▸ Open…).
func (o *Opener) PickFile(ctx context.Context) error {
	start, _ := os.UserHomeDir()
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "Open a Markdown file",
		DefaultPath: start,
		ButtonLabel: "Open",
		Multiple:    true,
		Filters:     []mygo.FileFilter{{Name: "Markdown", Extensions: []string{"md", "markdown"}}},
	})
	if err != nil {
		return err
	}
	for _, p := range paths {
		o.receive(p)
	}
	return nil
}

// pending returns the files a relaunch should open again: the previews
// and the vault notes not taken yet.
func (o *Opener) pending() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []string
	for _, id := range o.order {
		out = append(out, o.docs[id].path)
	}
	for _, f := range o.noted {
		out = append(out, f)
	}
	return out
}

// watchFile follows one outside file for live previews (WatchFiles
// watches a file through its directory, so a save by rename counts).
func watchFile(ctx context.Context, file string, changed func()) {
	w, err := mygo.WatchFiles(ctx, file, mygo.FileWatchOptions{})
	if err != nil {
		slog.Debug("watching an opened file failed", "err", err)
		return
	}
	defer w.Close()
	for {
		if _, err := w.Next(ctx); err != nil {
			// The file's folder went away: the preview says the file is gone.
			if ctx.Err() == nil {
				changed()
			}
			return
		}
		changed()
	}
}

// locateNote returns the vault path of the note at file, or "" when the
// file is not a note of the vault or not indexed (such as a note
// reached only through a symlink, which the vault's walk skips).
func (s *Vault) locateNote(file string) string {
	if s.v == nil {
		return ""
	}
	rel, ok, err := s.v.Locate(file)
	if err != nil || !ok {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.ix.Get(rel); err != nil {
		// Written by another app a moment ago: the watcher has not
		// caught up yet.
		if _, err := s.ix.Sync(s.v); err != nil {
			return ""
		}
		if _, err := s.ix.Get(rel); err != nil {
			return ""
		}
	}
	return rel
}

// importNote writes data as a new note named after name into folder
// ("" for the inbox, "/" for the root) and returns where it ended up
// (transform hooks may file it elsewhere).
func (s *Vault) importNote(folder, name string, data []byte) (string, error) {
	if s.v == nil {
		return "", ErrNoVault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.v.Inbox()
	switch folder {
	case "":
	case "/":
		f = ""
	default:
		f = folder
	}
	where, stop := s.bus.Follow()
	defer stop()
	n, err := s.v.Import(f, name, data)
	if err != nil {
		return "", err
	}
	return where(n.Path), nil
}
