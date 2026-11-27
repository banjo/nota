// Package index is the rebuildable SQLite index of a vault: notes, tags
// (nested a/b included), frontmatter properties, links and full-text
// search. The .md files stay the source of truth; the index can be deleted
// at any time and is rebuilt from disk.
//
// It uses modernc.org/sqlite, a pure-Go SQLite, so every target (desktop,
// server, phones) builds without cgo. There are no migrations: when the
// schema version changes the database is dropped and rebuilt.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/banjo/nota/internal/vault"
	_ "modernc.org/sqlite"
)

// SchemaVersion is bumped on every schema change; a database with another
// version is dropped and rebuilt.
const SchemaVersion = 2

// FileName is the index file inside the vault's .nota directory.
const FileName = "index.db"

// Index wraps the SQLite database.
type Index struct {
	db   *sql.DB
	path string
	// Recreated is true when Open found no index or an incompatible one.
	// The caller should Sync before trusting query results.
	Recreated bool
}

var schema = []string{
	`CREATE TABLE schema_version (version INTEGER NOT NULL)`,
	`CREATE TABLE notes (
		id        INTEGER PRIMARY KEY,
		path      TEXT NOT NULL UNIQUE,
		path_key  TEXT NOT NULL,  -- lower-case path without .md, for links
		folder    TEXT NOT NULL,
		name      TEXT NOT NULL,  -- lower-case file name without .md
		title     TEXT NOT NULL,
		title_key TEXT NOT NULL,
		mtime     INTEGER NOT NULL, -- unix nanoseconds
		size      INTEGER NOT NULL,
		hash      TEXT NOT NULL,
		created   INTEGER NOT NULL, -- unix milliseconds, frontmatter or mtime
		updated   INTEGER NOT NULL,
		props     TEXT NOT NULL     -- all frontmatter as a JSON object
	)`,
	`CREATE INDEX notes_path_key ON notes(path_key)`,
	`CREATE INDEX notes_folder ON notes(folder)`,
	`CREATE INDEX notes_name ON notes(name)`,
	`CREATE INDEX notes_title_key ON notes(title_key)`,
	`CREATE INDEX notes_updated ON notes(updated)`,
	`CREATE TABLE tags (
		note_id INTEGER NOT NULL,
		pos     INTEGER NOT NULL,
		tag     TEXT NOT NULL,
		tag_key TEXT NOT NULL,   -- lower case; "a/b" also matches tag "a"
		PRIMARY KEY (note_id, tag_key)
	) WITHOUT ROWID`,
	`CREATE INDEX tags_key ON tags(tag_key)`,
	`CREATE TABLE properties (
		note_id INTEGER NOT NULL,
		key     TEXT NOT NULL,
		value   TEXT NOT NULL,   -- JSON
		PRIMARY KEY (note_id, key)
	) WITHOUT ROWID`,
	`CREATE TABLE property_values (
		note_id INTEGER NOT NULL,
		key     TEXT NOT NULL,
		value   TEXT NOT NULL COLLATE NOCASE -- scalar as text; one row per list item
	)`,
	`CREATE INDEX property_values_kv ON property_values(key, value)`,
	`CREATE INDEX property_values_note ON property_values(note_id)`,
	`CREATE TABLE aliases (
		note_id   INTEGER NOT NULL,
		alias_key TEXT NOT NULL
	)`,
	`CREATE INDEX aliases_key ON aliases(alias_key)`,
	`CREATE INDEX aliases_note ON aliases(note_id)`,
	`CREATE TABLE links (
		note_id    INTEGER NOT NULL,
		target     TEXT NOT NULL,
		target_key TEXT NOT NULL,
		heading    TEXT NOT NULL,
		alias      TEXT NOT NULL,
		embed      INTEGER NOT NULL,
		wiki       INTEGER NOT NULL
	)`,
	`CREATE INDEX links_note ON links(note_id)`,
	`CREATE INDEX links_target ON links(target_key)`,
	// Markdown links to files that are not notes, by vault path (links
	// holds wikilinks and links to notes).
	`CREATE TABLE files (
		note_id INTEGER NOT NULL,
		path    TEXT NOT NULL
	)`,
	`CREATE INDEX files_note ON files(note_id)`,
	`CREATE VIRTUAL TABLE fts USING fts5(title, body, tokenize = 'unicode61 remove_diacritics 0')`,
}

// Path returns the index location for a vault: <vault>/.nota/index.db.
func Path(v *vault.Vault) string { return filepath.Join(v.MetaDir(), FileName) }

// OpenVault opens the index of a vault, creating .nota/ when needed.
func OpenVault(v *vault.Vault) (*Index, error) {
	if err := os.MkdirAll(v.MetaDir(), 0o755); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	return Open(Path(v))
}

// Open opens (or creates) the index at path. Use ":memory:" in tests. A
// database with another schema version, or one that is not readable as an
// index, is deleted and created again.
func Open(path string) (*Index, error) {
	ix, err := open(path)
	if err == nil {
		return ix, nil
	}
	if path == ":memory:" || !errors.Is(err, errStale) {
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("index: drop stale index: %w", err)
		}
	}
	ix, err = open(path)
	if err != nil {
		return nil, err
	}
	ix.Recreated = true
	return ix, nil
}

var errStale = errors.New("index: stale schema")

// corrupt reports SQLite errors that mean the file is not a usable
// database; only then is it safe to delete it. A locked database is not.
func corrupt(err error) bool {
	s := err.Error()
	return strings.Contains(s, "not a database") || strings.Contains(s, "malformed") || strings.Contains(s, "SQLITE_CORRUPT") || strings.Contains(s, "SQLITE_NOTADB")
}

func open(path string) (*Index, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1) // every connection would be its own database
	}
	ix := &Index{db: db, path: path}
	if err := ix.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return ix, nil
}

func (ix *Index) ensureSchema() error {
	var n int
	err := ix.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'`).Scan(&n)
	if err != nil {
		if corrupt(err) {
			return fmt.Errorf("%w: %v", errStale, err)
		}
		return fmt.Errorf("index: %w", err)
	}
	if n == 1 {
		var v int
		if err := ix.db.QueryRow(`SELECT version FROM schema_version`).Scan(&v); err == nil && v == SchemaVersion {
			return nil
		}
		return errStale
	}
	var tables int
	if err := ix.db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&tables); err != nil {
		return fmt.Errorf("index: %w", err)
	}
	if tables > 0 {
		return errStale // an older index without a version table
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, s := range schema {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("index: create schema: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, SchemaVersion); err != nil {
		return err
	}
	ix.Recreated = true
	return tx.Commit()
}

// Close closes the database.
func (ix *Index) Close() error { return ix.db.Close() }

// DB exposes the database for diagnostics and tests.
func (ix *Index) DB() *sql.DB { return ix.db }

// key is the case-insensitive form used for names, titles, paths and tags.
func key(s string) string { return strings.ToLower(vault.NFC(strings.TrimSpace(s))) }

// pathKey turns a note path or link target into its lookup key: lower
// case, no leading "./" or "/", no ".md".
func pathKey(p string) string {
	p = key(strings.ReplaceAll(p, `\`, "/"))
	p = strings.TrimPrefix(strings.TrimPrefix(p, "./"), "/")
	return strings.TrimSuffix(p, ".md")
}
