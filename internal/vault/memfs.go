package vault

import (
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"testing/fstest"
	"time"
)

// MemFS is an in-memory FS for tests. It is case-sensitive and stores
// names byte for byte, like ext4. It is safe for concurrent use.
type MemFS struct {
	mu sync.RWMutex
	m  fstest.MapFS
	// Now stamps modification times; time.Now when nil.
	Now func() time.Time
}

// NewMemFS returns an empty in-memory file system. files maps names to
// contents, for setting up fixtures.
func NewMemFS(files map[string]string) *MemFS {
	m := &MemFS{m: fstest.MapFS{}}
	for name, data := range files {
		if err := m.MkdirAll(path.Dir(name)); err != nil {
			panic(err)
		}
		if err := m.WriteFile(name, []byte(data)); err != nil {
			panic(err)
		}
	}
	return m
}

func (m *MemFS) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Open implements fs.FS.
func (m *MemFS) Open(name string) (fs.File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.m.Open(name)
}

// ReadDir implements fs.ReadDirFS.
func (m *MemFS) ReadDir(name string) ([]fs.DirEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.m.ReadDir(name)
}

// ReadFile implements fs.ReadFileFS.
func (m *MemFS) ReadFile(name string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.m.ReadFile(name)
}

// Stat implements fs.StatFS.
func (m *MemFS) Stat(name string) (fs.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.m.Stat(name)
}

// isDir reports whether name is a directory, explicit or implied by a
// file below it. The caller holds the lock.
func (m *MemFS) isDir(name string) bool {
	if name == "." {
		return true
	}
	if f, ok := m.m[name]; ok {
		return f.Mode.IsDir()
	}
	prefix := name + "/"
	for k := range m.m {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// parentOK checks that the parent directory of name exists.
func (m *MemFS) parentOK(op, name string) error {
	if !fs.ValidPath(name) || name == "." {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	if !m.isDir(path.Dir(name)) {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	return nil
}

// WriteFile implements FS.
func (m *MemFS) WriteFile(name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.parentOK("write", name); err != nil {
		return err
	}
	perm := fs.FileMode(0o644)
	if f, ok := m.m[name]; ok {
		if f.Mode.IsDir() {
			return &fs.PathError{Op: "write", Path: name, Err: fs.ErrInvalid}
		}
		perm = f.Mode.Perm()
	}
	m.m[name] = &fstest.MapFile{Data: slices.Clone(data), Mode: perm, ModTime: m.now()}
	return nil
}

// Rename implements FS. Like rename(2) it replaces an existing target file.
func (m *MemFS) Rename(oldname, newname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.m[oldname]
	if !ok || f.Mode.IsDir() {
		// Directories are not renamed by the vault; keep MemFS small.
		return &fs.PathError{Op: "rename", Path: oldname, Err: fs.ErrNotExist}
	}
	if err := m.parentOK("rename", newname); err != nil {
		return err
	}
	if t, ok := m.m[newname]; ok && t.Mode.IsDir() {
		return &fs.PathError{Op: "rename", Path: newname, Err: fs.ErrExist}
	}
	delete(m.m, oldname)
	m.m[newname] = f
	return nil
}

// Remove implements FS.
func (m *MemFS) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isDir(name) {
		if _, ok := m.m[name]; !ok {
			return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
		}
		delete(m.m, name)
		return nil
	}
	prefix := name + "/"
	for k := range m.m {
		if strings.HasPrefix(k, prefix) {
			return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrExist}
		}
	}
	delete(m.m, name)
	return nil
}

// MkdirAll implements FS.
func (m *MemFS) MkdirAll(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrInvalid}
	}
	for p := name; p != "."; p = path.Dir(p) {
		if f, ok := m.m[p]; ok {
			if !f.Mode.IsDir() {
				return &fs.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
			}
			continue
		}
		m.m[p] = &fstest.MapFile{Mode: fs.ModeDir | 0o755, ModTime: m.now()}
	}
	return nil
}
