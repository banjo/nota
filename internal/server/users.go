package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Deleting a user ("nota serve user rm") removes the account and
// everything reachable from it: device tokens, the browser sessions of
// the web app, passkeys, recovery codes, invites bound to the address,
// every vault with its files, version log and blob references, and the
// vaults' working copies on disk. The bytes in the blob store are shared
// by hash between users, so a blob file is only removed when no other
// user's row references it.

// RemovedVault is a vault that goes with a user.
type RemovedVault struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Cursor int64  `json:"cursor"`
}

// UserRemoval is what deleting a user removes, or would remove.
type UserRemoval struct {
	User           User           `json:"user"`
	Vaults         []RemovedVault `json:"vaults"`
	Tokens         int            `json:"tokens"`
	Passkeys       int            `json:"passkeys"`
	Sessions       int            `json:"sessions"`
	RecoveryCodes  int            `json:"recovery_codes"`
	Invites        int            `json:"invites"`
	Blobs          int            `json:"blobs"`
	BlobBytes      int64          `json:"blob_bytes"`
	BlobsReclaimed int            `json:"blobs_reclaimed"`
	BytesReclaimed int64          `json:"bytes_reclaimed"`

	// blobHashes are the user's blobs; which of them are shared with
	// another user is only known once the rows are gone.
	blobHashes []string
}

func countRows(ctx context.Context, q querier, query string, args ...any) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

// planUserRemoval collects what belongs to a user. It runs inside the
// deleting transaction or on the database directly (for the preview).
func planUserRemoval(ctx context.Context, q querier, email string) (UserRemoval, error) {
	e, err := NormalizeEmail(email)
	if err != nil {
		return UserRemoval{}, err
	}
	var rm UserRemoval
	rm.User = User{Email: e}
	var created int64
	err = q.QueryRowContext(ctx, `SELECT id, created_at FROM users WHERE email = ?`, e).Scan(&rm.User.ID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return UserRemoval{}, fmt.Errorf("user %s: %w", e, ErrNotFound)
	}
	if err != nil {
		return UserRemoval{}, err
	}
	rm.User.CreatedAt = fromMS(created)

	rows, err := q.QueryContext(ctx, `SELECT id, name, seq FROM vaults WHERE user_id = ? ORDER BY name`, rm.User.ID)
	if err != nil {
		return UserRemoval{}, err
	}
	for rows.Next() {
		var v RemovedVault
		if err := rows.Scan(&v.ID, &v.Name, &v.Cursor); err != nil {
			rows.Close()
			return UserRemoval{}, err
		}
		rm.Vaults = append(rm.Vaults, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return UserRemoval{}, err
	}

	brows, err := q.QueryContext(ctx, `SELECT hash, size FROM blobs WHERE user_id = ?`, rm.User.ID)
	if err != nil {
		return UserRemoval{}, err
	}
	seen := map[string]bool{}
	for brows.Next() {
		var h string
		var size int64
		if err := brows.Scan(&h, &size); err != nil {
			brows.Close()
			return UserRemoval{}, err
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		rm.Blobs++
		rm.BlobBytes += size
		rm.blobHashes = append(rm.blobHashes, h)
	}
	err = brows.Err()
	brows.Close()
	if err != nil {
		return UserRemoval{}, err
	}

	for _, c := range []struct {
		n   *int
		sql string
	}{
		{&rm.Tokens, `SELECT count(*) FROM tokens WHERE user_id = ?`},
		{&rm.Passkeys, `SELECT count(*) FROM credentials WHERE user_id = ?`},
		{&rm.Sessions, `SELECT count(*) FROM sessions WHERE user_id = ?`},
		{&rm.RecoveryCodes, `SELECT count(*) FROM recovery_codes WHERE user_id = ?`},
	} {
		if *c.n, err = countRows(ctx, q, c.sql, rm.User.ID); err != nil {
			return UserRemoval{}, err
		}
	}
	// Invites name the address, not a user id.
	if rm.Invites, err = countRows(ctx, q, `SELECT count(*) FROM invites WHERE email = ?`, e); err != nil {
		return UserRemoval{}, err
	}
	return rm, nil
}

// PlanUserRemoval reports what deleting a user would remove, including
// the blob files only this user references. It changes nothing.
func (s *Store) PlanUserRemoval(ctx context.Context, email string) (UserRemoval, error) {
	rm, err := planUserRemoval(ctx, s.db, email)
	if err != nil {
		return UserRemoval{}, err
	}
	for _, h := range rm.blobHashes {
		var one int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM blobs WHERE hash = ? AND user_id <> ? LIMIT 1`, h, rm.User.ID).Scan(&one)
		switch {
		case err == nil: // shared with another user: the file stays
		case errors.Is(err, sql.ErrNoRows):
			rm.BlobsReclaimed++
			var size int64
			_ = s.db.QueryRowContext(ctx, `SELECT max(size) FROM blobs WHERE hash = ?`, h).Scan(&size)
			rm.BytesReclaimed += size
		default:
			return UserRemoval{}, err
		}
	}
	return rm, nil
}

// DeleteUser deletes a user and everything owned by it in one write
// transaction: the tables cascade from users (tokens, vaults with their
// files and versions, blobs, credentials, sessions, recovery codes,
// ceremonies, auth requests), and invites bound to the address go first.
// The disk is left alone: the caller removes the working copies and the
// unreferenced blob files with RemoveUserData, so the database stays the
// truth if that fails.
func (s *Store) DeleteUser(ctx context.Context, email string) (UserRemoval, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserRemoval{}, err
	}
	defer tx.Rollback()
	rm, err := planUserRemoval(ctx, tx, email)
	if err != nil {
		return UserRemoval{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM invites WHERE email = ?`, rm.User.Email); err != nil {
		return UserRemoval{}, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, rm.User.ID)
	if err != nil {
		return UserRemoval{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return UserRemoval{}, fmt.Errorf("user %s: %w", rm.User.Email, ErrNotFound)
	}
	// Content-addressed blobs are shared: keep the file while any row
	// still points at the hash, hand the rest to the disk pass.
	unshared := rm.blobHashes[:0]
	for _, h := range rm.blobHashes {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM blobs WHERE hash = ? LIMIT 1`, h).Scan(&one)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			unshared = append(unshared, h)
		default:
			return UserRemoval{}, err
		}
	}
	rm.blobHashes = unshared
	if err := tx.Commit(); err != nil {
		return UserRemoval{}, err
	}
	return rm, nil
}

// BlobPath is where DiskBlobs keeps a blob: <dir>/ab/abcdef…
func BlobPath(dir, hash string) string {
	if len(hash) < 3 {
		return ""
	}
	return filepath.Join(dir, hash[:2], hash)
}

// RemoveUserData removes what DeleteUser left on disk: the working copy
// and index of each of the user's vaults, and the blob files no one
// references any more. It returns how many blob files went and their
// bytes.
func RemoveUserData(ctx context.Context, st *Store, dataDir string, rm UserRemoval) (files int, bytes int64, err error) {
	var errs []error
	for _, v := range rm.Vaults {
		if err := os.RemoveAll(filepath.Join(dataDir, "vaults", fmt.Sprint(v.ID))); err != nil {
			errs = append(errs, err)
		}
	}
	for _, h := range rm.blobHashes {
		// Another user may have referenced the hash since the commit (a
		// concurrent upload of the same content).
		var one int
		if err := st.db.QueryRowContext(ctx, `SELECT 1 FROM blobs WHERE hash = ? LIMIT 1`, h).Scan(&one); err == nil {
			continue
		}
		p := BlobPath(filepath.Join(dataDir, "blobs"), h)
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		if err := os.Remove(p); err != nil {
			errs = append(errs, err)
			continue
		}
		files++
		bytes += fi.Size()
	}
	return files, bytes, errors.Join(errs...)
}
