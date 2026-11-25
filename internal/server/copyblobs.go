package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
)

// CopyOptions configures CopyBlobs.
type CopyOptions struct {
	// Delete removes each blob from the source once the destination has
	// it.
	Delete bool
	// Workers is how many blobs are copied at once (default 8).
	Workers int
	// Progress, when set, is called after each blob (from one goroutine
	// at a time).
	Progress func(CopyResult)
}

// CopyResult counts what CopyBlobs did.
type CopyResult struct {
	// Copied blobs and their bytes.
	Copied int   `json:"copied"`
	Bytes  int64 `json:"bytes"`
	// Present is blobs the destination already had.
	Present int `json:"present"`
	// Deleted is blobs removed from the source.
	Deleted int `json:"deleted"`
}

// CopyBlobs copies every blob of src that dst lacks into dst, checking
// that the hash dst stored is the hash it came under, as "nota serve
// blobs migrate" does to move <data>/blobs into a bucket. It can run
// next to a server writing to src, and again: blobs dst has (same hash,
// same size) are skipped, so a second run copies only what came since.
func CopyBlobs(ctx context.Context, src, dst BlobStore, opt CopyOptions) (CopyResult, error) {
	if opt.Workers <= 0 {
		opt.Workers = 8
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu    sync.Mutex
		res   CopyResult
		first error
		wg    sync.WaitGroup
	)
	fail := func(err error) {
		mu.Lock()
		if first == nil {
			first = err
			cancel()
		}
		mu.Unlock()
	}
	jobs := make(chan BlobInfo)
	for range opt.Workers {
		wg.Go(func() {
			for b := range jobs {
				copied, err := copyBlob(ctx, src, dst, b)
				if err == nil && opt.Delete {
					err = src.Delete(ctx, b.Hash)
				}
				if err != nil {
					fail(fmt.Errorf("blob %s: %w", b.Hash, err))
					continue
				}
				mu.Lock()
				if copied {
					res.Copied++
					res.Bytes += b.Size
				} else {
					res.Present++
				}
				if opt.Delete {
					res.Deleted++
				}
				if opt.Progress != nil {
					opt.Progress(res)
				}
				mu.Unlock()
			}
		})
	}
	err := src.Walk(ctx, func(b BlobInfo) error {
		select {
		case jobs <- b:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(jobs)
	wg.Wait()
	if first != nil {
		return res, first
	}
	return res, err
}

func copyBlob(ctx context.Context, src, dst BlobStore, b BlobInfo) (bool, error) {
	have, err := dst.Stat(ctx, b.Hash)
	switch {
	case err == nil && have.Size == b.Size:
		return false, nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	rc, err := src.Open(ctx, b.Hash)
	if err != nil {
		return false, err
	}
	defer rc.Close()
	h, _, err := dst.Put(ctx, rc, 0)
	if err != nil {
		return false, err
	}
	if h != b.Hash {
		// The source's bytes do not match their name. What dst stored is
		// correct under its own hash, and garbage collection removes it
		// unless something references it.
		return false, fmt.Errorf("content hashes to %s", h)
	}
	return true, nil
}
