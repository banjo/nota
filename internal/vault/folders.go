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
