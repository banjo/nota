package sync

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/vault"
)

// Status is the local view of a vault's sync state, computed without
// taking the lock (so it works next to a running `nota sync --watch`).
type Status struct {
	// Cursor is the last applied change-feed version.
	Cursor int64 `json:"cursor"`
	// LastSync is when the last successful pass ended (zero if never).
	LastSync time.Time `json:"last_sync,omitzero"`
	// Tracked counts the files synced at least once (not deleted).
	Tracked int `json:"tracked"`
	// Changed, Added and Deleted are local changes not yet pushed.
	Changed []string `json:"changed"`
	Added   []string `json:"added"`
	Deleted []string `json:"deleted"`
	// Running reports a live engine holding the vault's lock.
	Running bool      `json:"running"`
	Holder  *LockInfo `json:"holder,omitempty"`
}

// ReadStatus scans the vault against the sync state in stateDir (the
// vault's .nota directory when empty).
func ReadStatus(v *vault.Vault, stateDir string) (*Status, error) {
	if stateDir == "" {
		stateDir = v.MetaDir()
	}
	out := &Status{Changed: []string{}, Added: []string{}, Deleted: []string{}}
	if _, err := os.Stat(filepath.Join(stateDir, StateFile)); errors.Is(err, os.ErrNotExist) {
		// Never synced: everything is new.
		err := v.WalkFiles(func(e vault.Entry) error {
			out.Added = append(out.Added, e.Path)
			return nil
		})
		return out, err
	}
	st, err := openState(stateDir)
	if err != nil {
		return nil, err
	}
	defer st.close()
	out.Cursor = st.cursor()
	if t, err := time.Parse(time.RFC3339Nano, st.meta("last_sync")); err == nil {
		out.LastSync = t
	}
	rows, err := st.all()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	err = v.WalkFiles(func(e vault.Entry) error {
		data, err := v.ReadEntry(e)
		if err != nil {
			return nil
		}
		seen[e.Path] = true
		r, ok := rows[e.Path]
		switch {
		case !ok || r.Deleted:
			out.Added = append(out.Added, e.Path)
		case protocol.Hash(data) != r.BaseHash:
			out.Changed = append(out.Changed, e.Path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for p, r := range rows {
		if r.Deleted {
			continue
		}
		out.Tracked++
		if !seen[p] && !v.Ignored(p, false) {
			out.Deleted = append(out.Deleted, p)
		}
	}
	slices.Sort(out.Deleted)
	if li, age, err := readLock(filepath.Join(stateDir, LockFile)); err == nil && age < DefaultStaleLock {
		host, _ := os.Hostname()
		if li.Host != host || pidAlive(li.PID) {
			out.Running, out.Holder = true, &li
		}
	}
	return out, nil
}
