package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// FS is the file system a vault lives on. Reads use the io/fs interfaces;
// the write methods are the few operations the vault needs on top.
//
// Names follow the io/fs rules: slash-separated, relative to the vault
// root, "." for the root itself, and as the file system stores them (so a
// name may be NFD on macOS; the vault maps them onto NFC paths).
type FS interface {
	fs.ReadDirFS
	fs.ReadFileFS
	fs.StatFS

	// WriteFile replaces name with data atomically: readers see the old
	// content or the new one, never a mix. An existing file's
	// permissions are kept. The parent directory must exist.
	WriteFile(name string, data []byte) error
	// Rename moves a file. The target's parent directory must exist.
	Rename(oldname, newname string) error
	// Remove deletes a file or an empty directory.
	Remove(name string) error
	// MkdirAll creates a directory and any missing parents.
	MkdirAll(name string) error
}

// OSPather is implemented by file systems backed by real OS paths, such
// as the one returned by DirFS.
type OSPather interface {
	// OSPath returns the absolute OS path of name.
	OSPath(name string) string
}

// DirFS returns the operating system's file system rooted at dir, which
// should be absolute.
func DirFS(dir string) FS { return osFS{root: dir, FS: os.DirFS(dir)} }

type osFS struct {
	root string
	fs.FS
}

func (o osFS) OSPath(name string) string {
	if name == "." || name == "" {
		return o.root
	}
	return filepath.Join(o.root, filepath.FromSlash(name))
}

func (o osFS) check(op, name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	return o.OSPath(name), nil
}

func (o osFS) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(o.FS, name) }

func (o osFS) ReadFile(name string) ([]byte, error) { return fs.ReadFile(o.FS, name) }

func (o osFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(o.FS, name) }

func (o osFS) WriteFile(name string, data []byte) error {
	p, err := o.check("write", name)
	if err != nil {
		return err
	}
	return writeAtomic(p, data)
}

func (o osFS) Rename(oldname, newname string) error {
	from, err := o.check("rename", oldname)
	if err != nil {
		return err
	}
	to, err := o.check("rename", newname)
	if err != nil {
		return err
	}
	return os.Rename(from, to)
}

func (o osFS) Remove(name string) error {
	p, err := o.check("remove", name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (o osFS) MkdirAll(name string) error {
	p, err := o.check("mkdir", name)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0o755)
}

// writeAtomic writes data to a temp file next to dst and renames it over
// dst, so readers never see a half-written note. An existing file's
// permissions are kept.
func writeAtomic(dst string, data []byte) error {
	perm := fs.FileMode(0o644)
	if st, err := os.Stat(dst); err == nil {
		perm = st.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	// Best effort: Windows only knows the read-only bit.
	_ = f.Chmod(perm)
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// osPath returns the OS path of a name on fsys, or "" when fsys is not
// backed by the OS.
func osPath(fsys FS, name string) string {
	if p, ok := fsys.(OSPather); ok {
		return p.OSPath(name)
	}
	return ""
}

// errNoOSPath is returned by DiskPath on file systems without OS paths.
var errNoOSPath = errors.New("vault: the file system has no OS paths")

// fsJoin joins names on an FS, keeping "." for the root.
func fsJoin(elem ...string) string {
	p := path.Join(elem...)
	if p == "" {
		return "."
	}
	return p
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("vault: %w", err)
}
