package server

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// Blob GC removes what nothing references and keeps everything a
// version (or a file) still points at, including the content of a
// deleted path, whose tombstone keeps its history readable.
func TestCollectBlobs(t *testing.T) {
	e := newEnv(t, Config{})
	ctx := context.Background()
	tok := e.user("gc@example.com")
	blobDir := filepath.Join(e.srv.cfg.DataDir, "blobs")
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
	// A whole vault that goes away: its versions go, the blob row stays.
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
	if err := e.srv.Store().DeleteVault(ctx, u.ID, "scratch"); err != nil {
		t.Fatal(err)
	}
	// A file nothing knows about, as a crashed upload leaves it.
	const orphan = "orphan bytes\n"
	op := BlobPath(blobDir, hashOf(orphan))
	if err := os.MkdirAll(filepath.Dir(op), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(op, []byte(orphan), 0o644); err != nil {
		t.Fatal(err)
	}

	wantBytes := int64(len(scratch) + len(orphan))
	future := time.Now().Add(2 * time.Hour) // older than the grace
	dry, err := e.srv.Store().CollectBlobs(ctx, blobDir, BlobGC{DryRun: true, Now: future})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Rows != 1 || dry.Files != 2 || dry.Orphans != 1 || dry.Bytes != wantBytes {
		t.Fatalf("dry run: %+v, want rows 1 files 2 orphans 1 bytes %d", dry, wantBytes)
	}
	for _, h := range []string{hashOf(scratch), hashOf(orphan)} {
		if _, err := os.Stat(BlobPath(blobDir, h)); err != nil {
			t.Fatalf("dry run removed %s: %v", h, err)
		}
	}
	if n, _ := countRows(ctx, e.srv.Store().db, `SELECT count(*) FROM blobs WHERE hash = ?`, hashOf(scratch)); n != 1 {
		t.Fatalf("dry run changed rows: %d", n)
	}

	res, err := e.srv.Store().CollectBlobs(ctx, blobDir, BlobGC{Now: future})
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 1 || res.Files != 2 || res.Bytes != wantBytes {
		t.Fatalf("gc: %+v, want rows 1 files 2 bytes %d", res, wantBytes)
	}
	for _, h := range []string{hashOf(scratch), hashOf(orphan)} {
		if _, err := os.Stat(BlobPath(blobDir, h)); !os.IsNotExist(err) {
			t.Fatalf("%s still there: %v", h, err)
		}
	}
	if n, _ := countRows(ctx, e.srv.Store().db, `SELECT count(*) FROM blobs WHERE hash = ?`, hashOf(scratch)); n != 0 {
		t.Fatalf("stale row left: %d", n)
	}
	// Live content survives: the current file and the history of the
	// deleted path.
	for h := range map[string]bool{hashOf(keep): true, hashOf(hist): true} {
		if _, err := os.Stat(BlobPath(blobDir, h)); err != nil {
			t.Fatalf("live blob %s gone: %v", h, err)
		}
	}
	if res, body := e.do(tok, "GET", protocol.FilePath("main", "keep.md"), nil, ""); res.StatusCode != 200 || string(body) != keep {
		t.Fatalf("read after gc: %d %q", res.StatusCode, body)
	}
	if res, body := e.do(tok, "GET", protocol.HistoryPath("main", "hist.md"), nil, ""); res.StatusCode != 200 {
		t.Fatalf("history after gc: %d %s", res.StatusCode, body)
	}

	// A second pass finds nothing; a young orphan is left alone.
	fresh := BlobPath(blobDir, hashOf("just uploaded\n"))
	if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("just uploaded\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := e.srv.Store().CollectBlobs(ctx, blobDir, BlobGC{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Rows != 0 || again.Files != 0 || again.Young != 1 {
		t.Fatalf("second pass: %+v", again)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("young orphan swept: %v", err)
	}
}
