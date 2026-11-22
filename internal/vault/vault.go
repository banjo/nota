// Package vault is the file layer of Nota: a vault is a folder of markdown
// notes, and the files on disk are the source of truth.
//
// A vault works on an FS: DirFS for a real directory, MemFS in tests.
//
// All paths in the API are vault-relative, slash-separated and in Unicode
// NFC, whatever the file system stores. Writes are atomic (temp file and
// rename), deletes move notes to .trash/, and a path that differs from an
// existing one only in letter case is refused, because it would overwrite
// that note on macOS and Windows.
package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/frontmatter"
)

// TrashDir is the vault-relative folder deleted notes are moved to.
const TrashDir = ".trash"

// Options configure Open.
type Options struct {
	// Inbox is the vault-relative folder for unsorted notes; DefaultInbox if empty.
	Inbox string
	// Create makes the vault directory when it does not exist yet.
	Create bool
	// Bus receives the vault's events. Nil drops them.
	Bus *events.Bus
}

// Vault is an open vault.
type Vault struct {
	fs     FS
	root   string
	inbox  string
	ignore *Ignore
	bus    *events.Bus
}

// Open opens the vault in the OS directory dir and loads its .notaignore.
func Open(dir string, opts Options) (*Vault, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	st, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) && opts.Create {
		if err = os.MkdirAll(root, 0o755); err == nil {
			st, err = os.Stat(root)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("vault: %s is not a directory", root)
	}
	return open(DirFS(root), root, opts)
}

// OpenFS opens a vault on any file system, such as a MemFS in tests.
// name identifies the vault in events and logs (Root returns it).
func OpenFS(fsys FS, name string, opts Options) (*Vault, error) {
	return open(fsys, name, opts)
}

func open(fsys FS, root string, opts Options) (*Vault, error) {
	inbox := opts.Inbox
	if inbox == "" {
		inbox = DefaultInbox
	}
	inbox, err := CleanPath(inbox)
	if err != nil {
		return nil, fmt.Errorf("vault: inbox: %w", err)
	}
	v := &Vault{fs: fsys, root: root, inbox: inbox, bus: opts.Bus}
	if err := v.ReloadIgnore(); err != nil {
		return nil, err
	}
	return v, nil
}

// Root returns the absolute vault directory (the name given to OpenFS
// for other file systems).
func (v *Vault) Root() string { return v.root }

// FS returns the file system the vault lives on.
func (v *Vault) FS() FS { return v.fs }

// Inbox returns the vault-relative inbox folder.
func (v *Vault) Inbox() string { return v.inbox }

// Bus returns the event bus the vault emits on (possibly nil).
func (v *Vault) Bus() *events.Bus { return v.bus }

// emit sends an event, plus inbox.added when a note arrives in the inbox.
func (v *Vault) emit(e events.Event, arrived bool) {
	v.bus.Emit(e)
	if arrived && Folder(e.Path) == v.inbox {
		v.bus.Emit(events.Event{Type: events.InboxAdded, Path: e.Path, Time: e.Time})
	}
}

// Ignore returns the vault's ignore matcher.
func (v *Vault) Ignore() *Ignore { return v.ignore }

// ReloadIgnore re-reads .notaignore from the vault root.
func (v *Vault) ReloadIgnore() error {
	data, err := v.fs.ReadFile(IgnoreFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("vault: %w", err)
	}
	v.ignore = ParseIgnoreFile(data)
	return nil
}

// Ignored reports whether a vault-relative path is excluded by the ignore rules.
func (v *Vault) Ignored(rel string, isDir bool) bool { return v.ignore.Match(rel, isDir) }

// MetaDirName is the vault-relative folder of Nota's own files.
const MetaDirName = ".nota"

// MetaDir returns the absolute path of the vault's .nota directory.
func (v *Vault) MetaDir() string { return filepath.Join(v.root, MetaDirName) }

// Entry is a note found on disk.
type Entry struct {
	// Path is the vault-relative NFC path.
	Path string
	// Disk is the name in the vault's FS, as the file system stores it
	// (possibly NFD). Read it with ReadEntry.
	Disk    string
	ModTime time.Time
	Size    int64
}

// ReadEntry reads the content of a note found by Walk.
func (v *Vault) ReadEntry(e Entry) ([]byte, error) { return v.fs.ReadFile(e.Disk) }

// Walk calls fn for every note in the vault that is not ignored, in
// lexical order of the on-disk names.
func (v *Vault) Walk(fn func(Entry) error) error {
	return fs.WalkDir(v.fs, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil // unreadable entries are skipped, not fatal
		}
		if p == "." {
			return nil
		}
		rel := NFC(p)
		if d.IsDir() {
			if v.ignore.Match(rel, true) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !IsNote(rel) || v.ignore.Match(rel, false) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		return fn(Entry{Path: rel, Disk: p, ModTime: info.ModTime(), Size: info.Size()})
	})
}

// List returns every note entry in the vault.
func (v *Vault) List() ([]Entry, error) {
	var out []Entry
	err := v.Walk(func(e Entry) error {
		out = append(out, e)
		return nil
	})
	return out, err
}

// lookup finds the entry in the directory disk whose NFC name is name.
// exact is the on-disk name of a match; folded is the on-disk name of an
// entry that differs only in letter case, when there is no exact match.
func (v *Vault) lookup(disk, name string) (exact, folded string, err error) {
	entries, err := v.fs.ReadDir(disk)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", nil
		}
		return "", "", err
	}
	key := foldKey(name)
	for _, e := range entries {
		n := NFC(e.Name())
		if n == name {
			return e.Name(), "", nil
		}
		if folded == "" && foldKey(n) == key {
			folded = e.Name()
		}
	}
	return "", folded, nil
}

// resolved is a vault path mapped onto the file system.
type resolved struct {
	rel  string // canonical NFC path
	disk string // FS name of the longest existing prefix plus the rest
	// exists reports whether every component exists with the exact NFC name.
	exists bool
	// collision is the vault path of an existing entry that differs from
	// rel only in case, in the first component where that happens.
	collision string
}

// resolve maps a clean vault-relative path onto disk names component by
// component, so NFD names on disk still match NFC input.
func (v *Vault) resolve(rel string) (resolved, error) {
	r := resolved{rel: rel, disk: ".", exists: true}
	if rel == "" {
		return r, nil
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if !r.exists {
			r.disk = fsJoin(r.disk, part)
			continue
		}
		exact, folded, err := v.lookup(r.disk, part)
		if err != nil {
			return r, fmt.Errorf("vault: %w", err)
		}
		switch {
		case exact != "":
			r.disk = fsJoin(r.disk, exact)
		case folded != "":
			prefix := append(append([]string{}, parts[:i]...), NFC(folded))
			r.collision = strings.Join(prefix, "/")
			r.exists = false
			r.disk = fsJoin(r.disk, part)
		default:
			r.exists = false
			r.disk = fsJoin(r.disk, part)
		}
	}
	return r, nil
}

// Exists reports whether the note path exists exactly (after NFC).
func (v *Vault) Exists(rel string) bool {
	rel, err := NotePath(rel)
	if err != nil {
		return false
	}
	r, err := v.resolve(rel)
	return err == nil && r.exists
}

// DiskPath returns the absolute OS path of an existing note, with the name
// as the file system stores it (which may be NFD on macOS). It fails on
// file systems that are not backed by the OS.
func (v *Vault) DiskPath(rel string) (string, error) {
	rel, err := NotePath(rel)
	if err != nil {
		return "", err
	}
	r, err := v.resolve(rel)
	if err != nil {
		return "", err
	}
	if !r.exists {
		return "", &PathError{Op: "open", Path: rel, Existing: r.collision, Err: ErrNotFound}
	}
	p := osPath(v.fs, r.disk)
	if p == "" {
		return "", errNoOSPath
	}
	return p, nil
}

// Read reads a note by vault-relative path. The ".md" may be left out.
func (v *Vault) Read(rel string) (*Note, error) {
	rel, err := NotePath(rel)
	if err != nil {
		return nil, err
	}
	r, err := v.resolve(rel)
	if err != nil {
		return nil, err
	}
	if !r.exists {
		return nil, &PathError{Op: "read", Path: rel, Existing: r.collision, Err: ErrNotFound}
	}
	return v.readDisk(rel, r.disk)
}

func (v *Vault) readDisk(rel, disk string) (*Note, error) {
	info, err := v.fs.Stat(disk)
	if err != nil {
		return nil, wrap(err)
	}
	if info.IsDir() {
		return nil, &PathError{Op: "read", Path: rel, Err: ErrNotNote}
	}
	raw, err := v.fs.ReadFile(disk)
	if err != nil {
		return nil, wrap(err)
	}
	n, _ := ParseNote(rel, raw) // broken YAML still yields a readable note
	n.ModTime = info.ModTime()
	return n, nil
}

// checkWritable refuses paths that are ignored, such as .nota/ or .trash/.
func (v *Vault) checkWritable(op, rel string) error {
	if v.ignore.Match(rel, false) {
		return &PathError{Op: op, Path: rel, Err: fmt.Errorf("%w: ignored by the vault's ignore rules", ErrInvalidPath)}
	}
	return nil
}

// Write replaces the content of a note, or creates it (and its folders)
// when it does not exist. It returns the canonical path and whether the
// note was created.
func (v *Vault) Write(rel string, data []byte) (string, bool, error) {
	rel, err := NotePath(rel)
	if err != nil {
		return "", false, err
	}
	if err := v.checkWritable("write", rel); err != nil {
		return "", false, err
	}
	r, err := v.resolve(rel)
	if err != nil {
		return "", false, err
	}
	if r.collision != "" {
		return "", false, &PathError{Op: "write", Path: rel, Existing: r.collision, Err: ErrCaseCollision}
	}
	if r.exists {
		if err := v.fs.WriteFile(r.disk, data); err != nil {
			return "", false, wrap(err)
		}
		v.emit(events.Event{Type: events.NoteSaved, Path: rel}, false)
		return rel, false, nil
	}
	disk, err := v.mkdirs(Folder(rel))
	if err != nil {
		return "", false, err
	}
	if err := v.fs.WriteFile(fsJoin(disk, path.Base(rel)), data); err != nil {
		return "", false, wrap(err)
	}
	v.emit(events.Event{Type: events.NoteCreated, Path: rel}, true)
	return rel, true, nil
}

// Create writes a new note into folder ("" for the vault root). The file
// name comes from the note's title; " 2", " 3" … are added until the name
// is free, comparing names case-insensitively.
func (v *Vault) Create(folder string, data []byte) (*Note, error) {
	folder, err := CleanPath(folder)
	if err != nil {
		return nil, err
	}
	stem := Filename(Title(frontmatter.Split(data).Body(), ""))
	if err := v.checkWritable("create", path.Join(folder, stem+Ext)); err != nil {
		return nil, err
	}
	dir, err := v.mkdirs(folder)
	if err != nil {
		return nil, err
	}
	name, err := v.freeName(dir, stem, Ext)
	if err != nil {
		return nil, err
	}
	rel := path.Join(folder, name)
	if err := v.fs.WriteFile(fsJoin(dir, name), data); err != nil {
		return nil, wrap(err)
	}
	v.emit(events.Event{Type: events.NoteCreated, Path: rel}, true)
	return v.Read(rel)
}

// freeName returns stem+ext, or "stem N"+ext for the lowest N >= 2, such
// that no entry in dir has the same name ignoring case and normalization.
func (v *Vault) freeName(dir, stem, ext string) (string, error) {
	entries, err := v.fs.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("vault: %w", err)
	}
	taken := make(map[string]bool, len(entries))
	for _, e := range entries {
		taken[foldKey(e.Name())] = true
	}
	name := stem + ext
	for i := 2; taken[foldKey(name)]; i++ {
		name = stem + " " + strconv.Itoa(i) + ext
	}
	return name, nil
}

// mkdirs makes sure the vault-relative folder exists and returns its FS
// name. Each missing component is checked for case collisions first.
func (v *Vault) mkdirs(folder string) (string, error) {
	r, err := v.resolve(folder)
	if err != nil {
		return "", err
	}
	if r.collision != "" {
		return "", &PathError{Op: "mkdir", Path: folder, Existing: r.collision, Err: ErrCaseCollision}
	}
	if r.exists {
		return r.disk, nil
	}
	if err := v.fs.MkdirAll(r.disk); err != nil {
		return "", wrap(err)
	}
	return r.disk, nil
}

// Move moves a note into another folder, keeping its file name, and
// returns the new path. It refuses to overwrite an existing note.
func (v *Vault) Move(rel, folder string) (string, error) {
	folder, err := CleanPath(folder)
	if err != nil {
		return "", err
	}
	src, err := NotePath(rel)
	if err != nil {
		return "", err
	}
	return v.Rename(src, path.Join(folder, path.Base(src)))
}

// Rename moves a note to a new vault-relative path and returns it. It
// refuses to overwrite an existing note or to create a case collision;
// renaming a note to a different case of its own name is allowed.
func (v *Vault) Rename(from, to string) (string, error) {
	from, err := NotePath(from)
	if err != nil {
		return "", err
	}
	to, err = NotePath(to)
	if err != nil {
		return "", err
	}
	src, err := v.resolve(from)
	if err != nil {
		return "", err
	}
	if !src.exists {
		return "", &PathError{Op: "move", Path: from, Existing: src.collision, Err: ErrNotFound}
	}
	if from == to {
		return to, nil
	}
	if err := v.checkWritable("move", to); err != nil {
		return "", err
	}
	dst, err := v.resolve(to)
	if err != nil {
		return "", err
	}
	if dst.exists {
		return "", &PathError{Op: "move", Path: to, Existing: to, Err: ErrExists}
	}
	if dst.collision != "" && dst.collision != from {
		return "", &PathError{Op: "move", Path: to, Existing: dst.collision, Err: ErrCaseCollision}
	}
	dir, err := v.mkdirs(Folder(to))
	if err != nil {
		return "", err
	}
	if err := v.fs.Rename(src.disk, fsJoin(dir, path.Base(to))); err != nil {
		return "", wrap(err)
	}
	v.emit(events.Event{Type: events.NoteMoved, Path: to, OldPath: from}, Folder(from) != Folder(to))
	return to, nil
}

// Delete moves a note to .trash/, keeping its folder structure, and
// returns its path inside the vault (".trash/…"). Nothing is removed.
func (v *Vault) Delete(rel string) (string, error) {
	rel, err := NotePath(rel)
	if err != nil {
		return "", err
	}
	src, err := v.resolve(rel)
	if err != nil {
		return "", err
	}
	if !src.exists {
		return "", &PathError{Op: "delete", Path: rel, Existing: src.collision, Err: ErrNotFound}
	}
	folder := path.Join(TrashDir, Folder(rel))
	if err := v.fs.MkdirAll(folder); err != nil {
		return "", wrap(err)
	}
	name, err := v.freeName(folder, Stem(rel), path.Ext(rel))
	if err != nil {
		return "", err
	}
	if err := v.fs.Rename(src.disk, path.Join(folder, name)); err != nil {
		return "", wrap(err)
	}
	trash := path.Join(folder, name)
	v.emit(events.Event{Type: events.NoteDeleted, Path: rel, Trash: trash}, false)
	return trash, nil
}
