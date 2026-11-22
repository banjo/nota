package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/banjo/nota/internal/protocol"
)

// BlobStore keeps file contents by their hash (protocol.Hash). Writing
// the same content twice stores it once. Implementations must be safe for
// concurrent use. The local disk store is DiskBlobs; an S3 store can
// implement the same interface later.
type BlobStore interface {
	// Put reads r to the end, stores the bytes under their hash and
	// returns it with the size. It fails with ErrTooLarge when r has more
	// than limit bytes (no limit when limit <= 0); nothing is stored then.
	Put(ctx context.Context, r io.Reader, limit int64) (hash string, size int64, err error)
	// Open returns the content stored under hash, or an error wrapping
	// fs.ErrNotExist.
	Open(ctx context.Context, hash string) (io.ReadCloser, error)
}

// ErrTooLarge is returned by BlobStore.Put for content above the limit.
var ErrTooLarge = errors.New("content too large")

// DiskBlobs is a content-addressed BlobStore in a local directory:
// <dir>/ab/abcdef…, written to a temp file and renamed into place, so a
// blob is either complete or absent.
type DiskBlobs struct{ dir string }

// NewDiskBlobs returns a blob store in dir, creating it if needed.
func NewDiskBlobs(dir string) (*DiskBlobs, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o755); err != nil {
		return nil, fmt.Errorf("server: blob store: %w", err)
	}
	return &DiskBlobs{dir: dir}, nil
}

func (d *DiskBlobs) path(hash string) string { return filepath.Join(d.dir, hash[:2], hash) }

// Put implements BlobStore.
func (d *DiskBlobs) Put(_ context.Context, r io.Reader, limit int64) (string, int64, error) {
	f, err := os.CreateTemp(filepath.Join(d.dir, "tmp"), "put-*")
	if err != nil {
		return "", 0, err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once renamed
	h := sha256.New()
	src := r
	if limit > 0 {
		src = io.LimitReader(r, limit+1)
	}
	n, err := io.Copy(io.MultiWriter(f, h), src)
	if err == nil && limit > 0 && n > limit {
		err = ErrTooLarge
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	hash := hex.EncodeToString(h.Sum(nil))
	dst := d.path(hash)
	if _, err := os.Stat(dst); err == nil {
		return hash, n, nil // deduplicated
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", 0, err
	}
	return hash, n, nil
}

// Open implements BlobStore.
func (d *DiskBlobs) Open(_ context.Context, hash string) (io.ReadCloser, error) {
	if !protocol.ValidHash(hash) {
		return nil, &fs.PathError{Op: "open", Path: hash, Err: fs.ErrNotExist}
	}
	return os.Open(d.path(hash))
}
