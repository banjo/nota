package server

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// Blob GC removes what nothing references and keeps everything a
// version (or a file) still points at, including the content of a
// deleted path, whose tombstone keeps its history readable.
func TestCollectBlobs(t *testing.T) {
	for name, blobs := range blobStores(t) {
		t.Run(name, func(t *testing.T) { testCollectBlobs(t, blobs) })
	}
}

func testCollectBlobs(t *testing.T, blobs func(t *testing.T, dataDir string) BlobStore) {
	dataDir := t.TempDir()
	e := newEnv(t, Config{DataDir: dataDir, Blobs: blobs(t, dataDir)})
	ctx := context.Background()
	tok := e.user("gc@example.com")
	exists := func(h string) bool {
		_, err := e.srv.blobs.Stat(ctx, h)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		return err == nil
	}
	store := func(content string) {
		if _, _, err := e.srv.blobs.Put(ctx, strings.NewReader(content), 0); err != nil {
			t.Fatal(err)
		}
	}
	const keep, hist = "keep me\n", "deleted but in history\n"
	if code, _, perr := e.put(tok, "keep.md", 0, keep); code != 200 {
		t.Fatalf("put keep: %d %v", code, perr)
	}
	if code, ent, perr := e.put(tok, "hist.md", 0, hist); code != 200 {
		t.Fatalf("put hist: %d %v", code, perr)
	} else if res, body := e.do(tok, "DELETE", protocol.FilePath("main", "hist.md"),
		map[string]string{protocol.HeaderBaseVersion: strconv.FormatInt(ent.Version, 10)}, ""); res.StatusCode != 200 {
		t.Fatalf("delete hist: %d %s", res.StatusCode, body)
	}
	// A whole vault that went away while its blob row stayed (what
	// servers before DeleteVault reclaimed rows left behind).
	const scratch = "only in a deleted vault\n"
	if res, body := e.do(tok, "POST", protocol.VaultsPath(), nil, `{"name":"scratch"}`); res.StatusCode != 201 {
		t.Fatalf("create vault: %d %s", res.StatusCode, body)
	}
	if res, body := e.do(tok, "PUT", protocol.FilePath("scratch", "x.md"), map[string]string{protocol.HeaderBaseVersion: "0"}, scratch); res.StatusCode != 200 {
		t.Fatalf("put scratch: %d %s", res.StatusCode, body)
	}
	u, err := e.srv.Store().UserByEmail(ctx, "gc@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.Store().db.ExecContext(ctx, `DELETE FROM vaults WHERE user_id = ? AND name = 'scratch'`, u.ID); err != nil {
		t.Fatal(err)
	}
	// A file nothing knows about, as a crashed upload leaves it.
	const orphan = "orphan bytes\n"
	store(orphan)

	wantBytes := int64(len(scratch) + len(orphan))
	future := time.Now().Add(2 * time.Hour) // older than the grace
	dry, err := e.srv.Store().CollectBlobs(ctx, e.srv.blobs, BlobGC{DryRun: true, Now: future})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Rows != 1 || dry.Files != 2 || dry.Orphans != 1 || dry.Bytes != wantBytes {
		t.Fatalf("dry run: %+v, want rows 1 files 2 orphans 1 bytes %d", dry, wantBytes)
	}
	for _, h := range []string{hashOf(scratch), hashOf(orphan)} {
		if !exists(h) {
			t.Fatalf("dry run removed %s", h)
		}
	}
	if n, _ := countRows(ctx, e.srv.Store().db, `SELECT count(*) FROM blobs WHERE hash = ?`, hashOf(scratch)); n != 1 {
		t.Fatalf("dry run changed rows: %d", n)
	}

	res, err := e.srv.Store().CollectBlobs(ctx, e.srv.blobs, BlobGC{Now: future})
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 1 || res.Files != 2 || res.Bytes != wantBytes {
		t.Fatalf("gc: %+v, want rows 1 files 2 bytes %d", res, wantBytes)
	}
	for _, h := range []string{hashOf(scratch), hashOf(orphan)} {
		if exists(h) {
			t.Fatalf("%s still there", h)
		}
	}
	if n, _ := countRows(ctx, e.srv.Store().db, `SELECT count(*) FROM blobs WHERE hash = ?`, hashOf(scratch)); n != 0 {
		t.Fatalf("stale row left: %d", n)
	}
	// Live content survives: the current file and the history of the
	// deleted path.
	for h := range map[string]bool{hashOf(keep): true, hashOf(hist): true} {
		if !exists(h) {
			t.Fatalf("live blob %s gone", h)
		}
	}
	if res, body := e.do(tok, "GET", protocol.FilePath("main", "keep.md"), nil, ""); res.StatusCode != 200 || string(body) != keep {
		t.Fatalf("read after gc: %d %q", res.StatusCode, body)
	}
	if res, body := e.do(tok, "GET", protocol.HistoryPath("main", "hist.md"), nil, ""); res.StatusCode != 200 {
		t.Fatalf("history after gc: %d %s", res.StatusCode, body)
	}

	// A second pass finds nothing; a young orphan is left alone.
	store("just uploaded\n")
	again, err := e.srv.Store().CollectBlobs(ctx, e.srv.blobs, BlobGC{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Rows != 0 || again.Files != 0 || again.Young != 1 {
		t.Fatalf("second pass: %+v", again)
	}
	if !exists(hashOf("just uploaded\n")) {
		t.Fatal("young orphan swept")
	}
}
