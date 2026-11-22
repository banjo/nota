// Package index is the rebuildable SQLite index of a vault: full-text search
// now, tags, properties and backlinks later. It uses modernc.org/sqlite, a
// pure-Go SQLite, so every target (desktop, server, phones) builds without cgo.
package index

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Index wraps the SQLite database.
type Index struct{ db *sql.DB }

// Open opens (or creates) the index at path. Use ":memory:" in tests.
func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS notes USING fts5(path UNINDEXED, title, body, tokenize = 'unicode61 remove_diacritics 0')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, fmt.Errorf("index: %s: %w", s, err)
		}
	}
	return &Index{db: db}, nil
}

// Close closes the database.
func (ix *Index) Close() error { return ix.db.Close() }

// Put adds or replaces one note.
func (ix *Index) Put(path, title, body string) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM notes WHERE path = ?`, path); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO notes(path, title, body) VALUES (?, ?, ?)`, path, title, body); err != nil {
		return err
	}
	return tx.Commit()
}

// Search returns the paths of notes matching an FTS5 query, best match first.
func (ix *Index) Search(q string) ([]string, error) {
	rows, err := ix.db.Query(`SELECT path FROM notes WHERE notes MATCH ? ORDER BY bm25(notes, 0, 10, 1)`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
