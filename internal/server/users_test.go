package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A user's deletion takes its tokens, vaults, version log and working
// copy with it, and only the blob files no one else references.
func TestDeleteUser(t *testing.T) {
	e := newEnv(t, Config{})
	ctx := context.Background()
	gone, keep := e.user("gone@example.com"), e.user("keep@example.com")
	const shared, own = "the same words\n", "only this user wrote this\n"
	for _, c := range []struct{ tok, path, body string }{
		{gone, "shared.md", shared}, {keep, "shared.md", shared}, {gone, "own.md", own},
	} {
		if code, _, perr := e.put(c.tok, c.path, 0, c.body); code != 200 {
			t.Fatalf("put %s: %d %v", c.path, code, perr)
		}
	}
	u, err := e.srv.Store().UserByEmail(ctx, "gone@example.com")
	if err != nil {
		t.Fatal(err)
	}
	vaultID, err := e.srv.Store().VaultID(ctx, u.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.srv.cfg.DataDir, "vaults", strconv.FormatInt(vaultID, 10), "shared.md")); err != nil {
		t.Fatalf("working copy not materialized: %v", err)
	}

	plan, err := e.srv.Store().PlanUserRemoval(ctx, "Gone@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tokens != 1 || len(plan.Vaults) != 1 || plan.Blobs != 2 {
		t.Fatalf("plan: %+v", plan)
	}
	if plan.BlobsReclaimed != 1 || plan.BytesReclaimed != int64(len(own)) {
		t.Fatalf("plan reclaims %d files/%d bytes, want 1/%d", plan.BlobsReclaimed, plan.BytesReclaimed, len(own))
	}

	rm, err := e.srv.Store().DeleteUser(ctx, "gone@example.com")
	if err != nil {
		t.Fatal(err)
	}
	files, bytes, err := RemoveUserData(ctx, e.srv.Store(), e.srv.cfg.DataDir, rm)
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 || bytes != int64(len(own)) {
		t.Fatalf("reclaimed %d files/%d bytes, want 1/%d", files, bytes, len(own))
	}
	if _, err := os.Stat(filepath.Join(e.srv.cfg.DataDir, "vaults", strconv.FormatInt(vaultID, 10))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("working copy still there: %v", err)
	}
	if res, _ := e.do(gone, "GET", "/v1/vaults", nil, ""); res.StatusCode != 401 {
		t.Fatalf("deleted user's token still works: %d", res.StatusCode)
	}
	if _, err := e.srv.Store().UserByEmail(ctx, "gone@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("user still there: %v", err)
	}
	if n, _ := countRows(ctx, e.srv.Store().db, `SELECT count(*) FROM vaults WHERE user_id = ?`, u.ID); n != 0 {
		t.Fatalf("%d vault rows left", n)
	}
	if n, _ := countRows(ctx, e.srv.Store().db, `SELECT count(*) FROM blobs WHERE hash = ?`, hashOf(own)); n != 0 {
		t.Fatalf("own blob row left: %d", n)
	}
	// The other user still reads the shared content.
	if res, body := e.do(keep, "GET", "/v1/vaults/main/files/shared.md", nil, ""); res.StatusCode != 200 || string(body) != shared {
		t.Fatalf("shared blob lost: %d %q", res.StatusCode, body)
	}
	if _, err := e.srv.Store().DeleteUser(ctx, "gone@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
