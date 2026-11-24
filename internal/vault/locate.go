package vault

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/banjo/nota/internal/events"
)

// Locate maps a path of the operating system onto the vault: the
// vault-relative NFC path of the same file, or ok=false when the file is
// outside the vault, ignored by the vault (.nota/, .trash/, .notaignore),
// not a note (".md"), or not a regular file.
//
// Symbolic links are resolved on both sides (the vault root itself may be
// one), so a link elsewhere that points into the vault finds the note,
// and a link inside the vault that points out does not. A name that
// differs from the note on disk only in Unicode normalization or, on a
// case-insensitive file system, in letter case finds the note too: the
// final check is that both paths are the same file (os.SameFile).
//
// It fails only when the file cannot be looked at (missing, unreadable),
// or the vault is not backed by the OS.
func (v *Vault) Locate(file string) (rel string, ok bool, err error) {
	if file == "" {
		return "", false, &PathError{Op: "locate", Path: file, Err: ErrInvalidPath}
	}
	if !isOSFS(v.fs) {
		return "", false, errNoOSPath
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", false, fmt.Errorf("vault: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false, wrap(err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", false, wrap(err)
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	root, err := filepath.EvalSymlinks(v.root)
	if err != nil {
		return "", false, wrap(err)
	}
	if !within(root, real) || real == root {
		return "", false, nil
	}
	r, err := filepath.Rel(root, real)
	if err != nil {
		return "", false, nil
	}
	rel = NFC(filepath.ToSlash(r))
	if !IsNote(rel) || v.Ignored(rel, false) {
		return "", false, nil
	}
	// The name as given may differ in case (a CLI argument on APFS) or
	// normalization: find the entry on disk, then make sure it is the
	// same file.
	disk, ok, err := v.resolveFold(rel)
	if err != nil || !ok {
		return "", false, err
	}
	canon := NFC(disk)
	if !IsNote(canon) || v.Ignored(canon, false) {
		return "", false, nil
	}
	st, err := os.Stat(osPath(v.fs, disk))
	if err != nil || !os.SameFile(st, info) {
		return "", false, nil
	}
	return canon, true, nil
}

// resolveFold maps a clean vault path onto the FS name of an existing
// entry, component by component, preferring an exact (NFC) match and
// else one that differs only in case.
func (v *Vault) resolveFold(rel string) (string, bool, error) {
	disk := "."
	for _, part := range strings.Split(rel, "/") {
		exact, folded, err := v.lookup(disk, part)
		if err != nil {
			return "", false, fmt.Errorf("vault: %w", err)
		}
		switch {
		case exact != "":
			disk = fsJoin(disk, exact)
		case folded != "":
			disk = fsJoin(disk, folded)
		default:
			return "", false, nil
		}
	}
	return disk, true, nil
}

func isOSFS(fsys FS) bool {
	_, ok := fsys.(OSPather)
	return ok
}

// MarkdownExts are the file extensions Nota opens as Markdown from the
// file manager. Only ".md" files are notes in the vault.
var MarkdownExts = []string{".md", ".markdown"}

// IsMarkdown reports whether a file name has one of MarkdownExts.
func IsMarkdown(name string) bool {
	ext := path.Ext(strings.ReplaceAll(name, `\`, "/"))
	for _, e := range MarkdownExts {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// Import writes data as a new note in folder ("" is the vault root),
// named after name: its extension is dropped (".markdown" becomes ".md")
// and the stem made safe as Filename does. " 2", " 3" … are added when a
// note of that name exists, comparing case-insensitively. The bytes are
// written exactly as given: no frontmatter or timestamp is added. It
// emits note.created (so hooks see it), and returns the note as written;
// event handlers may have moved it since (see events.Bus.Follow).
func (v *Vault) Import(folder, name string, data []byte) (*Note, error) {
	folder, err := CleanPath(folder)
	if err != nil {
		return nil, err
	}
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	if IsMarkdown(base) {
		base = strings.TrimSuffix(base, path.Ext(base))
	}
	stem := Filename(base)
	if err := v.checkWritable("import", path.Join(folder, stem+Ext)); err != nil {
		return nil, err
	}
	if folder != "" && v.Ignored(folder, true) {
		return nil, &PathError{Op: "import", Path: folder, Err: fmt.Errorf("%w: ignored by the vault's ignore rules", ErrInvalidPath)}
	}
	dir, err := v.mkdirs(folder)
	if err != nil {
		return nil, err
	}
	fname, err := v.freeName(dir, stem, Ext)
	if err != nil {
		return nil, err
	}
	rel := path.Join(folder, fname)
	disk := fsJoin(dir, fname)
	if err := v.fs.WriteFile(disk, data); err != nil {
		return nil, wrap(err)
	}
	n, err := v.readDisk(rel, disk)
	if err != nil {
		return nil, err
	}
	v.emit(events.Event{Type: events.NoteCreated, Path: rel}, true)
	return n, nil
}
