package sync

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite"

	"github.com/banjo/nota/internal/protocol"
)

// stateSchema is bumped on every change of the state tables. A state DB
// with another version is dropped: the engine then treats every file as
// new, which a sync pass resolves by content without losing anything.
const stateSchema = 1

// StateFile is the state database in the state directory.
const StateFile = "state.db"

// fileState is what the engine knows about one path.
type fileState struct {
	Path string
	// BaseHash is the hash of the last synced content: the merge base.
	BaseHash string
	// Version is the server version BaseHash corresponds to.
	Version int64
	// LocalHash is the hash the file had when the row was last written.
	LocalHash string
	// Deleted marks a path whose last synced state is a tombstone.
	Deleted bool
	Kind    protocol.Kind
}

// state is the engine's persistent state: .nota/state.db plus the merge
// bases in .nota/base/ (one file per content hash).
type state struct {
	db  *sql.DB
	dir string
}

func openState(dir string) (*state, error) {
	if err := os.MkdirAll(filepath.Join(dir, "base"), 0o755); err != nil {
		return nil, fmt.Errorf("sync: state: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, StateFile)+
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("sync: state: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &state{db: db, dir: dir}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *state) init() error {
	var v int
	err := s.db.QueryRow(`SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'schema'`).Scan(&v)
	if err == nil && v == stateSchema {
		return nil
	}
	for _, q := range []string{
		`DROP TABLE IF EXISTS meta`,
		`DROP TABLE IF EXISTS files`,
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE files (
			path       TEXT PRIMARY KEY,
			base_hash  TEXT NOT NULL,
			version    INTEGER NOT NULL,
			local_hash TEXT NOT NULL,
			deleted    INTEGER NOT NULL DEFAULT 0,
			kind       TEXT NOT NULL
		)`,
		`INSERT INTO meta (key, value) VALUES ('schema', '` + strconv.Itoa(stateSchema) + `')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("sync: state: %w", err)
		}
	}
	return nil
}

func (s *state) close() error { return s.db.Close() }

func (s *state) meta(key string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v
}

func (s *state) setMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *state) cursor() int64 {
	n, _ := strconv.ParseInt(s.meta("cursor"), 10, 64)
	return n
}

func (s *state) setCursor(c int64) error { return s.setMeta("cursor", strconv.FormatInt(c, 10)) }

// reset forgets every file and the cursor (another remote was configured).
func (s *state) reset(remote string) error {
	if _, err := s.db.Exec(`DELETE FROM files`); err != nil {
		return err
	}
	if err := s.setCursor(0); err != nil {
		return err
	}
	return s.setMeta("remote", remote)
}

func (s *state) all() (map[string]fileState, error) {
	rows, err := s.db.Query(`SELECT path, base_hash, version, local_hash, deleted, kind FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]fileState{}
	for rows.Next() {
		var f fileState
		var kind string
		if err := rows.Scan(&f.Path, &f.BaseHash, &f.Version, &f.LocalHash, &f.Deleted, &kind); err != nil {
			return nil, err
		}
		f.Kind = protocol.Kind(kind)
		out[f.Path] = f
	}
	return out, rows.Err()
}

func (s *state) get(p string) (fileState, bool, error) {
	f := fileState{Path: p}
	var kind string
	err := s.db.QueryRow(`SELECT base_hash, version, local_hash, deleted, kind FROM files WHERE path = ?`, p).
		Scan(&f.BaseHash, &f.Version, &f.LocalHash, &f.Deleted, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return fileState{}, false, nil
	}
	f.Kind = protocol.Kind(kind)
	return f, err == nil, err
}

func (s *state) put(f fileState) error {
	if f.Kind == "" {
		f.Kind = protocol.KindOf(f.Path)
	}
	_, err := s.db.Exec(`INSERT INTO files (path, base_hash, version, local_hash, deleted, kind) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET base_hash = excluded.base_hash, version = excluded.version,
		local_hash = excluded.local_hash, deleted = excluded.deleted, kind = excluded.kind`,
		f.Path, f.BaseHash, f.Version, f.LocalHash, f.Deleted, string(f.Kind))
	return err
}

// synced records that path holds the content with hash at version.
func (s *state) synced(p, hash string, version int64) error {
	return s.put(fileState{Path: p, BaseHash: hash, Version: version, LocalHash: hash})
}

// tombstone records that path is deleted at version.
func (s *state) tombstone(p, lastHash string, version int64) error {
	return s.put(fileState{Path: p, BaseHash: lastHash, Version: version, Deleted: true})
}

func (s *state) basePath(hash string) string { return filepath.Join(s.dir, "base", hash[:2], hash) }

// saveBase stores content as a merge base (notes only; blobs are never
// merged, so their content is not kept).
func (s *state) saveBase(p string, data []byte) error {
	if protocol.KindOf(p) != protocol.KindMD {
		return nil
	}
	hash := protocol.Hash(data)
	dst := s.basePath(hash)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// base returns the stored merge base with hash; ok is false when it is
// not there (an empty base is then the safe fallback: nothing merges
// silently).
func (s *state) base(hash string) ([]byte, bool) {
	if !protocol.ValidHash(hash) {
		return nil, false
	}
	data, err := os.ReadFile(s.basePath(hash))
	if err != nil || protocol.Hash(data) != hash {
		return nil, false
	}
	return data, true
}

// gcBases removes stored bases no live row refers to.
func (s *state) gcBases() error {
	rows, err := s.all()
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, r := range rows {
		if !r.Deleted {
			keep[r.BaseHash] = true
		}
	}
	root := filepath.Join(s.dir, "base")
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !keep[d.Name()] {
			os.Remove(p)
		}
		return nil
	})
}
