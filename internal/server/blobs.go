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
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// BlobStore keeps file contents by their hash (protocol.Hash). Writing
// the same content twice stores it once. Implementations must be safe for
// concurrent use: DiskBlobs keeps them in a local directory, S3Blobs in
// an S3-compatible bucket.
type BlobStore interface {
	// Put reads r to the end, stores the bytes under their hash and
	// returns it with the size. It fails with ErrTooLarge when r has more
	// than limit bytes (no limit when limit <= 0); nothing is stored then.
	Put(ctx context.Context, r io.Reader, limit int64) (hash string, size int64, err error)
	// Open returns the content stored under hash, or an error wrapping
	// fs.ErrNotExist.
	Open(ctx context.Context, hash string) (io.ReadCloser, error)
	// Stat returns the size and time of the content stored under hash,
	// or an error wrapping fs.ErrNotExist.
	Stat(ctx context.Context, hash string) (BlobInfo, error)
	// Delete removes the content stored under hash; it is not an error
	// when there is none.
	Delete(ctx context.Context, hash string) error
	// Walk calls fn for every blob stored, in no particular order, and
	// stops at the first error fn returns.
	Walk(ctx context.Context, fn func(BlobInfo) error) error
}

// BlobInfo describes a stored blob.
type BlobInfo struct {
	Hash     string
	Size     int64
	Modified time.Time
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

// Stat implements BlobStore.
func (d *DiskBlobs) Stat(_ context.Context, hash string) (BlobInfo, error) {
	if !protocol.ValidHash(hash) {
		return BlobInfo{}, &fs.PathError{Op: "stat", Path: hash, Err: fs.ErrNotExist}
	}
	fi, err := os.Stat(d.path(hash))
	if err != nil {
		return BlobInfo{}, err
	}
	return BlobInfo{Hash: hash, Size: fi.Size(), Modified: fi.ModTime()}, nil
}

// Delete implements BlobStore.
func (d *DiskBlobs) Delete(_ context.Context, hash string) error {
	if !protocol.ValidHash(hash) {
		return nil
	}
	if err := os.Remove(d.path(hash)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Walk implements BlobStore. Files outside the <ab>/ directories (tmp/)
// and names that are not hashes are skipped.
func (d *DiskBlobs) Walk(ctx context.Context, fn func(BlobInfo) error) error {
	return filepath.WalkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if p == d.dir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if errors.Is(err, fs.ErrNotExist) {
				return nil // removed while walking
			}
			return err
		}
		if e.IsDir() {
			if p != d.dir && filepath.Dir(p) != d.dir {
				return filepath.SkipDir
			}
			return ctx.Err()
		}
		name := e.Name()
		if filepath.Dir(p) == d.dir || !protocol.ValidHash(name) || filepath.Base(filepath.Dir(p)) != name[:2] {
			return nil
		}
		fi, err := e.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		return fn(BlobInfo{Hash: name, Size: fi.Size(), Modified: fi.ModTime()})
	})
}
