package vault

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode"
)

// Mkdir creates an empty folder (and any missing parents) at a
// vault-relative path and returns the canonical path. It fails with
// ErrExists when the folder is already there, ErrCaseCollision when a
// component differs from an existing entry only in letter case, and
// ErrInvalidPath for the vault root, hidden or ignored names (.nota,
// .trash, …), names with characters that are not portable, and names
// that look like notes. Empty folders are local: they are not synced
// until they hold a file (docs/sync.md).
func (v *Vault) Mkdir(rel string) (string, error) {
	rel, err := CleanPath(rel)
	if err != nil {
		return "", err
	}
	if err := checkFolderName(rel); err != nil {
		return "", err
	}
	if v.ignore.Match(rel, true) {
		return "", &PathError{Op: "mkdir", Path: rel, Err: fmt.Errorf("%w: ignored by the vault's ignore rules", ErrInvalidPath)}
	}
	r, err := v.resolve(rel)
	if err != nil {
		return "", err
	}
	if r.collision != "" {
		return "", &PathError{Op: "mkdir", Path: rel, Existing: r.collision, Err: ErrCaseCollision}
	}
	if r.exists {
		return "", &PathError{Op: "mkdir", Path: rel, Existing: rel, Err: ErrExists}
	}
	// A file in the way, such as "a" when making "a/b", is not a folder.
	for p := Folder(rel); p != ""; p = Folder(p) {
		pr, err := v.resolve(p)
		if err != nil {
			return "", err
		}
		if !pr.exists {
			continue
		}
		if st, err := v.fs.Stat(pr.disk); err == nil && !st.IsDir() {
			return "", &PathError{Op: "mkdir", Path: rel, Existing: p, Err: fmt.Errorf("%w: %s is a file", ErrExists, p)}
		}
		break
	}
	if _, err := v.mkdirs(rel); err != nil {
		return "", err
	}
	return rel, nil
}

// checkFolderName refuses names that are hidden, empty, reserved on
// Windows, contain characters other file systems reject, or end in .md.
func checkFolderName(rel string) error {
	bad := func(why string) error {
		return &PathError{Op: "mkdir", Path: rel, Err: fmt.Errorf("%w: %s", ErrInvalidPath, why)}
	}
	if rel == "" {
		return bad("a folder name is needed")
	}
	for _, seg := range strings.Split(rel, "/") {
		switch {
		case strings.HasPrefix(seg, "."):
			return bad(fmt.Sprintf("%q is hidden", seg))
		case strings.TrimRight(seg, ". ") != seg:
			return bad(fmt.Sprintf("%q ends in a dot or space", seg))
		case strings.TrimSpace(seg) == "":
			return bad("empty name")
		case isReservedWindowsName(seg):
			return bad(fmt.Sprintf("%q is a reserved name", seg))
		case IsNote(seg):
			return bad(fmt.Sprintf("%q looks like a note", seg))
		}
		for _, r := range seg {
			if strings.ContainsRune(`:*?"<>|`, r) || unicode.IsControl(r) {
				return bad(fmt.Sprintf("%q contains %q", seg, r))
			}
		}
	}
	return nil
}

// Folders lists every folder of the vault, empty ones included, as
// sorted NFC vault paths. Hidden folders (.git, .nota, .trash, …) and
// folders matched by the ignore rules are left out.
func (v *Vault) Folders() ([]string, error) {
	var out []string
	err := fs.WalkDir(v.fs, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil
		}
		if p == "." || !d.IsDir() {
			return nil
		}
		rel := NFC(p)
		if strings.HasPrefix(path.Base(rel), ".") || v.ignore.Match(rel, true) {
			return fs.SkipDir
		}
		out = append(out, rel)
		return nil
	})
	slices.Sort(out)
	return out, err
}

// RenameFolder renames a folder, or moves it under another one, by
// moving every file in it to its new path one by one (so each note emits
// note.moved, and sync sees plain path moves). Empty subfolders come
// along. Like Move it does not rewrite wikilinks: they name notes, not
// folders. It fails with ErrNotFound when the folder is not there,
// ErrExists when the target is, ErrCaseCollision when it differs from an
// existing folder only in letter case (a case-only rename of the folder
// itself is refused too), and ErrInvalidPath for the vault root, hidden
// or ignored names and a target inside the folder itself. Files the
// ignore rules hide stay behind, and so does the old folder then.
func (v *Vault) RenameFolder(from, to string) (string, error) {
	from, err := CleanPath(from)
	if err != nil {
		return "", err
	}
	to, err = CleanPath(to)
	if err != nil {
		return "", err
	}
	bad := func(why string) error {
		return &PathError{Op: "rename", Path: from, Err: fmt.Errorf("%w: %s", ErrInvalidPath, why)}
	}
	if from == "" {
		return "", bad("the vault root cannot be renamed")
	}
	if err := checkFolderName(from); err != nil {
		return "", err
	}
	if err := checkFolderName(to); err != nil {
		return "", err
	}
	if v.ignore.Match(to, true) {
		return "", bad("ignored by the vault's ignore rules")
	}
	if strings.HasPrefix(to+"/", from+"/") && to != from {
		return "", bad("a folder cannot move into itself")
	}
	src, err := v.resolve(from)
	if err != nil {
		return "", err
	}
	if st, err := v.fs.Stat(src.disk); !src.exists || err != nil || !st.IsDir() {
		return "", &PathError{Op: "rename", Path: from, Existing: src.collision, Err: ErrNotFound}
	}
	if to == from {
		return to, nil
	}
	dst, err := v.resolve(to)
	if err != nil {
		return "", err
	}
	if dst.collision != "" {
		return "", &PathError{Op: "rename", Path: to, Existing: dst.collision, Err: ErrCaseCollision}
	}
	if dst.exists {
		return "", &PathError{Op: "rename", Path: to, Existing: to, Err: ErrExists}
	}
	// A file in the way of a parent of the target is not a folder.
	for p := Folder(to); p != ""; p = Folder(p) {
		pr, err := v.resolve(p)
		if err != nil {
			return "", err
		}
		if !pr.exists {
			continue
		}
		if st, err := v.fs.Stat(pr.disk); err == nil && !st.IsDir() {
			return "", &PathError{Op: "rename", Path: to, Existing: p, Err: fmt.Errorf("%w: %s is a file", ErrExists, p)}
		}
		break
	}

	dirs, err := v.Folders()
	if err != nil {
		return "", err
	}
	var sub []string
	for _, d := range dirs {
		if d == from || strings.HasPrefix(d, from+"/") {
			sub = append(sub, d) // sorted: parents before children
		}
	}
	var files []string
	err = v.WalkFiles(func(e Entry) error {
		if strings.HasPrefix(e.Path, from+"/") {
			files = append(files, e.Path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	swap := func(p string) string { return to + strings.TrimPrefix(p, from) }
	for _, d := range sub {
		if _, err := v.mkdirs(swap(d)); err != nil {
			return "", err
		}
	}
	for _, f := range files {
		if _, err := v.rename(f, swap(f)); err != nil {
			return "", err
		}
	}
	// Drop what is left of the old tree, deepest first; a folder that
	// still holds hidden or ignored files is kept.
	for i := len(sub) - 1; i >= 0; i-- {
		if r, err := v.resolve(sub[i]); err == nil && r.exists {
			_ = v.fs.Remove(r.disk)
		}
	}
	// The icons follow; the folder has moved even when they can't.
	_ = v.moveFolderIcons(from, to)
	return to, nil
}
