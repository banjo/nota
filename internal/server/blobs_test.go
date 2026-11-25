package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// blobStores are the BlobStore implementations every store test runs
// against: the disk, and S3Blobs on an in-memory S3. With
// $NOTA_TEST_S3_ENDPOINT (and $NOTA_TEST_S3_BUCKET, AWS_ACCESS_KEY_ID,
// AWS_SECRET_ACCESS_KEY) also on a real service, e.g. MinIO:
//
//	docker run -d -p 9000:9000 minio/minio server /data
func blobStores(t *testing.T) map[string]func(t *testing.T, dataDir string) BlobStore {
	stores := map[string]func(t *testing.T, dataDir string) BlobStore{
		"disk": func(t *testing.T, dataDir string) BlobStore {
			b, err := NewDiskBlobs(filepath.Join(dataDir, "blobs"))
			if err != nil {
				t.Fatal(err)
			}
			return b
		},
		"s3": func(t *testing.T, dataDir string) BlobStore {
			return newFakeS3(t).store(t, "nota")
		},
	}
	if ep := os.Getenv("NOTA_TEST_S3_ENDPOINT"); ep != "" {
		stores["s3-live"] = func(t *testing.T, dataDir string) BlobStore {
			b, err := NewS3Blobs(S3Config{
				Endpoint:  ep,
				Region:    os.Getenv("NOTA_TEST_S3_REGION"),
				Bucket:    os.Getenv("NOTA_TEST_S3_BUCKET"),
				Prefix:    "test-" + strconv.FormatInt(time.Now().UnixNano(), 36),
				AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
				SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
				TmpDir:    t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx := context.Background()
				_ = b.Walk(ctx, func(i BlobInfo) error { return b.Delete(ctx, i.Hash) })
			})
			return b
		}
	}
	return stores
}

func TestBlobStores(t *testing.T) {
	for name, open := range blobStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			b := open(t, t.TempDir())
			if r, ok := b.(interface{ Ready(context.Context) error }); ok {
				if err := r.Ready(ctx); err != nil {
					t.Fatal(err)
				}
			}
			put := func(s string) string {
				h, n, err := b.Put(ctx, strings.NewReader(s), 1<<20)
				if err != nil || n != int64(len(s)) || h != hashOf(s) {
					t.Fatalf("put %q: %s %d %v", s, h, n, err)
				}
				return h
			}
			a, c := put("alpha\n"), put("charlie\n")
			put("alpha\n") // again: still one
			big := strings.Repeat("x", 70<<10)
			bh := put(big)

			rc, err := b.Open(ctx, a)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(rc)
			rc.Close()
			if string(got) != "alpha\n" {
				t.Fatalf("open: %q", got)
			}
			if info, err := b.Stat(ctx, bh); err != nil || info.Size != int64(len(big)) || time.Since(info.Modified) > time.Hour {
				t.Fatalf("stat: %+v %v", info, err)
			}

			var seen []string
			if err := b.Walk(ctx, func(i BlobInfo) error { seen = append(seen, i.Hash); return nil }); err != nil {
				t.Fatal(err)
			}
			sort.Strings(seen)
			want := []string{a, c, bh}
			sort.Strings(want)
			if fmt.Sprint(seen) != fmt.Sprint(want) {
				t.Fatalf("walk: %v, want %v", seen, want)
			}

			if _, _, err := b.Put(ctx, strings.NewReader("too long"), 3); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("over the limit: %v", err)
			}
			if err := b.Delete(ctx, c); err != nil {
				t.Fatal(err)
			}
			if err := b.Delete(ctx, c); err != nil {
				t.Fatalf("delete twice: %v", err)
			}
			for _, h := range []string{c, hashOf("never stored"), "../../etc/passwd"} {
				if _, err := b.Open(ctx, h); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("open %s: %v", h, err)
				}
				if _, err := b.Stat(ctx, h); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("stat %s: %v", h, err)
				}
			}
		})
	}
}
