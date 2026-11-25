package server

import (
	"context"
	"time"
)

// Blob garbage collection. The blob store is content addressed and the
// bytes are shared by every user whose files have that hash, so a blob
// is live as long as one version of one vault references it: a version
// row keeps its old content alive, which is what "nota history" and
// "nota restore" read. What can go is what has no reference at all:
//
//   - rows of the blobs table whose hash appears in no version and no
//     file (a vault or an account was deleted, or a delete was undone
//     by a restored older version);
//   - blobs in the store that the database does not know: the
//     upload of a file that failed between writing the bytes and
//     committing the version, or the leftovers of a deleted vault.
//
// CollectBlobs removes both. It is safe next to a running server:
//
//   - the row work happens in one write transaction, the same serialised
//     path every other mutation uses (SQLite BEGIN IMMEDIATE, also across
//     processes), so a commit that references a hash cannot run between
//     reading the reference set and deleting the rows;
//   - a stored blob is only removed once no version and no row references
//     it;
//   - a file younger than MinAge (default DefaultBlobGrace) is left
//     alone even when unreferenced, because an upload writes its bytes
//     before it commits them and a file still being written must not be
//     swept. The same grace covers the one race that remains: a client
//     re-uploading content whose unreferenced file is exactly at the age
//     boundary.
//
// It does not run at startup on purpose: a boot that walks the whole
// blob store costs time proportional to the number of files, on every
// restart, to reclaim nothing most of the time. Run it from a timer or
// by hand when the store has grown.

// DefaultBlobGrace is how long an unreferenced blob file is left alone
// so an upload in flight is never swept.
const DefaultBlobGrace = time.Hour

// BlobGC configures one garbage collection pass.
type BlobGC struct {
	// DryRun reports what would go without removing anything.
	DryRun bool
	// MinAge protects unreferenced files younger than this
	// (DefaultBlobGrace when 0).
	MinAge time.Duration
	// Now is the clock (the store's clock when zero).
	Now time.Time
}

// GCResult is what one pass found and did.
type GCResult struct {
	DryRun bool `json:"dry_run"`
	// Rows is the number of blobs-table rows removed (or to remove).
	Rows int `json:"rows"`
	// Files is the number of blob files removed (or to remove).
	Files int `json:"files"`
	// Bytes is the size of those files.
	Bytes int64 `json:"bytes"`
	// Orphans is how many of the files had no row at all (a failed
	// upload or a leftover of a deleted vault); the rest lost their last
	// row in this pass.
	Orphans int `json:"orphans"`
	// Young is unreferenced files kept because they are younger than
	// MinAge.
	Young int `json:"young"`
}

// CollectBlobs runs one garbage collection pass over the database and
// the blob store. Call it only from one place at a time; CollectBlobs
// itself is safe next to the server's writes.
func (s *Store) CollectBlobs(ctx context.Context, blobs BlobStore, opt BlobGC) (GCResult, error) {
	res := GCResult{DryRun: opt.DryRun}
	if opt.Now.IsZero() {
		opt.Now = s.now()
	}
	if opt.MinAge <= 0 {
		opt.MinAge = DefaultBlobGrace
	}
	cutoff := opt.Now.Add(-opt.MinAge)

	// Snapshot the store first, outside the transaction: the walk can
	// take a while and holds no lock.
	disk := map[string]BlobInfo{}
	if err := blobs.Walk(ctx, func(b BlobInfo) error {
		disk[b.Hash] = b
		return nil
	}); err != nil {
		return res, err
	}

	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE: the server's write lock
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	const unreferenced = `NOT EXISTS (SELECT 1 FROM versions v WHERE v.hash = blobs.hash)
		AND NOT EXISTS (SELECT 1 FROM files f WHERE f.hash = blobs.hash)`
	rows, err := tx.QueryContext(ctx, `SELECT hash, max(size) FROM blobs WHERE `+unreferenced+` GROUP BY hash`)
	if err != nil {
		return res, err
	}
	stale := map[string]int64{}
	for rows.Next() {
		var h string
		var size int64
		if err := rows.Scan(&h, &size); err != nil {
			rows.Close()
			return res, err
		}
		stale[h] = size
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return res, err
	}
	if !opt.DryRun && len(stale) > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM blobs WHERE `+unreferenced); err != nil {
			return res, err
		}
	}
	// Live hashes after the row deletion: every version ever committed
	// plus every remaining row. A file with one of these stays.
	live := map[string]bool{}
	lrows, err := tx.QueryContext(ctx, `SELECT hash FROM versions UNION SELECT hash FROM blobs`)
	if err != nil {
		return res, err
	}
	for lrows.Next() {
		var h string
		if err := lrows.Scan(&h); err != nil {
			lrows.Close()
			return res, err
		}
		live[h] = true
	}
	err = lrows.Err()
	lrows.Close()
	if err != nil {
		return res, err
	}
	// In a dry run the stale rows are still there (and a hash can have
	// several, one per user): they are not live references.
	for h := range stale {
		delete(live, h)
	}
	res.Rows = len(stale)

	// Remove the files of the hashes that just lost their last row,
	// then the ones no row ever knew; both only when unreferenced and
	// older than the grace. Unlinking happens before the commit so a
	// failed commit leaves at most a harmless stale row behind, never a
	// live reference to a file that is gone.
	remove := func(h string, size int64) error {
		res.Files++
		res.Bytes += size
		if opt.DryRun {
			return nil
		}
		return blobs.Delete(ctx, h)
	}
	for h, size := range stale {
		if live[h] {
			continue
		}
		if _, ok := disk[h]; !ok {
			continue
		}
		if err := remove(h, size); err != nil {
			return res, err
		}
	}
	for h, f := range disk {
		if live[h] || stale[h] > 0 {
			continue
		}
		if f.Modified.After(cutoff) {
			res.Young++
			continue
		}
		res.Orphans++
		if err := remove(h, f.Size); err != nil {
			return res, err
		}
	}
	if opt.DryRun {
		return res, nil
	}
	return res, tx.Commit()
}
