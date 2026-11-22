package vault

import (
	"errors"
	"path"
	"strings"
)

// The methods in this file work on any file in the vault, notes and
// attachments alike, for the sync engine. They take paths as given
// (cleaned and NFC, but no ".md" is added). Notes still emit their
// events; other files emit none.

// filePath cleans a path for the file methods.
func filePath(rel string) (string, error) {
	c, err := CleanPath(rel)
	if err != nil {
		return "", err
	}
	if c == "" {
		return "", &PathError{Op: "clean", Path: rel, Err: ErrInvalidPath}
	}
	return c, nil
}

// WalkFiles calls fn for every regular file in the vault that the ignore
// rules do not exclude, notes and attachments alike.
func (v *Vault) WalkFiles(fn func(Entry) error) error { return v.walk(false, fn) }

// ReadFile reads any file by vault path. A path that does not exist
// exactly fails with ErrNotFound (with Existing set when another case of
// it exists).
func (v *Vault) ReadFile(rel string) ([]byte, error) {
	rel, err := filePath(rel)
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
	data, err := v.fs.ReadFile(r.disk)
	return data, wrap(err)
}

// FileExists reports whether any file exists at exactly rel.
func (v *Vault) FileExists(rel string) bool {
	_, err := v.ReadFile(rel)
	return err == nil
}

// WriteFile writes any file raw, like Write does for notes: no
// timestamps are touched. It creates missing folders and refuses ignored
// paths and case collisions.
func (v *Vault) WriteFile(rel string, data []byte) (string, bool, error) {
	rel, err := filePath(rel)
	if err != nil {
		return "", false, err
	}
	return v.write(rel, data)
}

// RenameFile moves any file to a new path, refusing to overwrite or to
// create a case collision.
func (v *Vault) RenameFile(from, to string) (string, error) {
	from, err := filePath(from)
	if err != nil {
		return "", err
	}
	if to, err = filePath(to); err != nil {
		return "", err
	}
	return v.rename(from, to)
}

// DeleteFile moves any file to .trash/ and returns where it went.
func (v *Vault) DeleteFile(rel string) (string, error) {
	rel, err := filePath(rel)
	if err != nil {
		return "", err
	}
	return v.trash(rel)
}

// FreeName returns a path in the folder of rel that no file uses, case
// insensitively: rel itself, or rel with " 2", " 3" … before the
// extension.
func (v *Vault) FreeName(rel string) (string, error) {
	rel, err := filePath(rel)
	if err != nil {
		return "", err
	}
	dir, err := v.resolve(Folder(rel))
	if err != nil {
		return "", err
	}
	if !dir.exists {
		if dir.collision != "" {
			return "", &PathError{Op: "name", Path: rel, Existing: dir.collision, Err: ErrCaseCollision}
		}
		return rel, nil
	}
	ext := path.Ext(rel)
	name, err := v.freeName(dir.disk, strings.TrimSuffix(path.Base(rel), ext), ext)
	if err != nil {
		return "", err
	}
	if f := Folder(rel); f != "" {
		return f + "/" + name, nil
	}
	return name, nil
}

// IsNotFound reports whether err means a vault path does not exist.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
