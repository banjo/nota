package index

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/vault"
)

// Stats describes one Sync or Rebuild pass.
type Stats struct {
	Notes     int           `json:"notes"`
	Added     int           `json:"added"`
	Updated   int           `json:"updated"`
	Removed   int           `json:"removed"`
	Unchanged int           `json:"unchanged"`
	Duration  time.Duration `json:"durationNs"`
}

// Changed reports whether the pass wrote anything.
func (s Stats) Changed() bool { return s.Added+s.Updated+s.Removed > 0 }

// doc is a note parsed for the index.
type doc struct {
	path, folder, name, title string
	body                      string
	mtime                     time.Time
	size                      int64
	hash                      string
	created, updated          int64
	props                     map[string]any
	propsJSON                 string
	tags                      []string
	aliases                   []string
	links                     []markdown.Link
	files                     []string
}

func parseDoc(e vault.Entry, raw []byte) *doc {
	n, _ := vault.ParseNote(e.Path, raw) // broken YAML: index the note without properties
	sum := sha256.Sum256(raw)
	d := &doc{
		path:   e.Path,
		folder: vault.Folder(e.Path),
		name:   vault.Stem(e.Path),
		title:  n.Title,
		body:   n.Body,
		mtime:  e.ModTime,
		size:   int64(len(raw)),
		hash:   hex.EncodeToString(sum[:]),
		props:  normalize(n.Props).(map[string]any),
	}
	b, err := json.Marshal(d.props)
	if err != nil {
		b = []byte("{}")
	}
	d.propsJSON = string(b)
	d.created = timeProp(d.props["created"], e.ModTime)
	d.updated = timeProp(d.props["updated"], e.ModTime)
	parsed := markdown.Parse([]byte(n.Body), d.folder)
	d.tags = mergeTags(propStrings(d.props["tags"], true), propStrings(d.props["tag"], true), parsed.Tags)
	d.aliases = append(propStrings(d.props["aliases"], false), propStrings(d.props["alias"], false)...)
	d.links = parsed.Links
	d.files = parsed.Files
	return d
}

// normalize turns decoded YAML into JSON-friendly values: maps with
// non-string keys get string keys, times become RFC 3339 (or plain dates).
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalize(val)
		}
		return out
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 && x.Location() == time.UTC {
			return x.Format(time.DateOnly)
		}
		return x.Format(time.RFC3339)
	case nil:
		return nil
	default:
		return x
	}
}

// scalarText renders a scalar property value for --where matching.
func scalarText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(x), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	}
	return "", false
}

// propStrings reads a property that holds a list of strings, or one string
// with comma or (for tags) space separated items.
func propStrings(v any, tags bool) []string {
	var raw []string
	switch x := v.(type) {
	case string:
		sep := func(r rune) bool { return r == ',' || (tags && (r == ' ' || r == '\t')) }
		raw = strings.FieldsFunc(x, sep)
	case []any:
		for _, item := range x {
			if s, ok := scalarText(item); ok {
				raw = append(raw, s)
			}
		}
	}
	var out []string
	for _, s := range raw {
		if tags {
			s = markdown.CleanTag(s)
		} else {
			s = strings.TrimSpace(s)
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func mergeTags(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		for _, t := range l {
			if k := key(t); !seen[k] {
				seen[k] = true
				out = append(out, t)
			}
		}
	}
	return out
}

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", time.DateOnly}

// timeProp reads a created/updated property as unix milliseconds, falling
// back to the file's mtime.
func timeProp(v any, fallback time.Time) int64 {
	if s, ok := v.(string); ok {
		for _, l := range timeLayouts {
			if t, err := time.ParseInLocation(l, strings.TrimSpace(s), time.Local); err == nil {
				return t.UnixMilli()
			}
		}
	}
	return fallback.UnixMilli()
}

type stored struct {
	id    int64
	mtime int64
	size  int64
	hash  string
}

// Sync brings the index up to date with the vault: new and changed notes
// (by mtime and size, then content hash) are indexed, missing ones removed.
func (ix *Index) Sync(v *vault.Vault) (Stats, error) {
	start := time.Now()
	var st Stats
	have := map[string]stored{}
	rows, err := ix.db.Query(`SELECT id, path, mtime, size, hash FROM notes`)
	if err != nil {
		return st, fmt.Errorf("index: %w", err)
	}
	for rows.Next() {
		var s stored
		var p string
		if err := rows.Scan(&s.id, &p, &s.mtime, &s.size, &s.hash); err != nil {
			rows.Close()
			return st, err
		}
		have[p] = s
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}

	var todo []vault.Entry
	seen := map[string]bool{}
	err = v.Walk(func(e vault.Entry) error {
		seen[e.Path] = true
		if s, ok := have[e.Path]; ok && s.mtime == e.ModTime.UnixNano() && s.size == e.Size {
			st.Unchanged++
			return nil
		}
		todo = append(todo, e)
		return nil
	})
	if err != nil {
		return st, fmt.Errorf("index: walk vault: %w", err)
	}
	st.Notes = len(seen)

	docs := readDocs(v, todo)
	tx, err := ix.db.Begin()
	if err != nil {
		return st, err
	}
	defer tx.Rollback()
	w, err := newWriter(tx)
	if err != nil {
		return st, err
	}
	defer w.close()
	for i, d := range docs {
		if d == nil { // vanished between walk and read
			st.Notes--
			delete(seen, todo[i].Path)
			continue
		}
		s, ok := have[d.path]
		switch {
		case ok && s.hash == d.hash:
			if _, err := w.touch.Exec(d.mtime.UnixNano(), d.size, s.id); err != nil {
				return st, err
			}
			st.Unchanged++
		case ok:
			if err := w.put(s.id, d); err != nil {
				return st, err
			}
			st.Updated++
		default:
			if err := w.put(0, d); err != nil {
				return st, err
			}
			st.Added++
		}
	}
	for p, s := range have {
		if !seen[p] {
			if err := w.remove(s.id); err != nil {
				return st, err
			}
			st.Removed++
		}
	}
	if err := tx.Commit(); err != nil {
		return st, err
	}
	st.Duration = time.Since(start)
	return st, nil
}

// readDocs reads and parses entries on all CPUs. A nil result means the
// file could not be read (for example deleted meanwhile).
func readDocs(v *vault.Vault, entries []vault.Entry) []*doc {
	out := make([]*doc, len(entries))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), max(1, len(entries))) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				raw, err := v.ReadEntry(entries[i])
				if err != nil {
					continue
				}
				out[i] = parseDoc(entries[i], raw)
			}
		}()
	}
	for i := range entries {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}

// Rebuild drops everything and indexes the vault from scratch.
func (ix *Index) Rebuild(v *vault.Vault) (Stats, error) {
	start := time.Now()
	tx, err := ix.db.Begin()
	if err != nil {
		return Stats{}, err
	}
	defer tx.Rollback()
	for _, t := range []string{"notes", "tags", "properties", "property_values", "aliases", "links", "files", "fts"} {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return Stats{}, fmt.Errorf("index: clear %s: %w", t, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Stats{}, err
	}
	st, err := ix.Sync(v)
	st.Duration = time.Since(start)
	return st, err
}

// Update re-indexes one note after the vault changed it. A path that no
// longer exists is removed from the index.
func (ix *Index) Update(v *vault.Vault, rel string) error {
	var id int64
	err := ix.db.QueryRow(`SELECT id FROM notes WHERE path = ?`, rel).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	w, err := newWriter(tx)
	if err != nil {
		return err
	}
	defer w.close()
	n, err := v.Read(rel)
	switch {
	case errors.Is(err, vault.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
		if id != 0 {
			if err := w.remove(id); err != nil {
				return err
			}
		}
	case err != nil:
		return err
	case v.Ignored(n.Path, false):
		if id != 0 {
			if err := w.remove(id); err != nil {
				return err
			}
		}
	default:
		if err := w.put(id, parseDoc(vault.Entry{Path: n.Path, ModTime: n.ModTime, Size: n.Size}, n.Raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Remove drops one path from the index.
func (ix *Index) Remove(rel string) error {
	var id int64
	err := ix.db.QueryRow(`SELECT id FROM notes WHERE path = ?`, rel).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	w, err := newWriter(tx)
	if err != nil {
		return err
	}
	defer w.close()
	if err := w.remove(id); err != nil {
		return err
	}
	return tx.Commit()
}

// writer holds prepared statements for one write transaction.
type writer struct {
	tx                                      *sql.Tx
	insert, update, touch                   *sql.Stmt
	tag, prop, propValue, alias, link, text *sql.Stmt
	file                                    *sql.Stmt
	deletes                                 []*sql.Stmt
	all                                     []*sql.Stmt
}

func newWriter(tx *sql.Tx) (*writer, error) {
	w := &writer{tx: tx}
	prep := func(dst **sql.Stmt, q string) error {
		s, err := tx.Prepare(q)
		if err != nil {
			return fmt.Errorf("index: prepare %q: %w", q, err)
		}
		*dst = s
		w.all = append(w.all, s)
		return nil
	}
	cols := `path, path_key, folder, name, title, title_key, mtime, size, hash, created, updated, props`
	stmts := []struct {
		dst **sql.Stmt
		q   string
	}{
		{&w.insert, `INSERT INTO notes(` + cols + `) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`},
		{&w.update, `UPDATE notes SET (` + cols + `) = (?,?,?,?,?,?,?,?,?,?,?,?) WHERE id = ?`},
		{&w.touch, `UPDATE notes SET mtime = ?, size = ? WHERE id = ?`},
		{&w.tag, `INSERT OR IGNORE INTO tags(note_id, pos, tag, tag_key) VALUES (?,?,?,?)`},
		{&w.prop, `INSERT OR REPLACE INTO properties(note_id, key, value) VALUES (?,?,?)`},
		{&w.propValue, `INSERT INTO property_values(note_id, key, value) VALUES (?,?,?)`},
		{&w.alias, `INSERT INTO aliases(note_id, alias_key) VALUES (?,?)`},
		{&w.link, `INSERT INTO links(note_id, target, target_key, heading, alias, embed, wiki) VALUES (?,?,?,?,?,?,?)`},
		{&w.text, `INSERT INTO fts(rowid, title, body) VALUES (?,?,?)`},
		{&w.file, `INSERT INTO files(note_id, path) VALUES (?,?)`},
	}
	for _, s := range stmts {
		if err := prep(s.dst, s.q); err != nil {
			w.close()
			return nil, err
		}
	}
	for _, t := range []string{"tags", "properties", "property_values", "aliases", "links", "files"} {
		var s *sql.Stmt
		if err := prep(&s, `DELETE FROM `+t+` WHERE note_id = ?`); err != nil {
			w.close()
			return nil, err
		}
		w.deletes = append(w.deletes, s)
	}
	var s *sql.Stmt
	if err := prep(&s, `DELETE FROM fts WHERE rowid = ?`); err != nil {
		w.close()
		return nil, err
	}
	w.deletes = append(w.deletes, s)
	return w, nil
}

func (w *writer) close() {
	for _, s := range w.all {
		s.Close()
	}
}

// clear removes every row that belongs to a note, except the note itself.
func (w *writer) clear(id int64) error {
	for _, s := range w.deletes {
		if _, err := s.Exec(id); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) remove(id int64) error {
	if err := w.clear(id); err != nil {
		return err
	}
	_, err := w.tx.Exec(`DELETE FROM notes WHERE id = ?`, id)
	return err
}

// put inserts (id == 0) or replaces a note and all its rows.
func (w *writer) put(id int64, d *doc) error {
	args := []any{d.path, pathKey(d.path), d.folder, key(d.name), d.title, key(d.title),
		d.mtime.UnixNano(), d.size, d.hash, d.created, d.updated, d.propsJSON}
	if id == 0 {
		res, err := w.insert.Exec(args...)
		if err != nil {
			return fmt.Errorf("index: insert %s: %w", d.path, err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
	} else {
		if err := w.clear(id); err != nil {
			return err
		}
		if _, err := w.update.Exec(append(args, id)...); err != nil {
			return fmt.Errorf("index: update %s: %w", d.path, err)
		}
	}
	for i, t := range d.tags {
		if _, err := w.tag.Exec(id, i, t, key(t)); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(d.props))
	for k := range d.props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := d.props[k]
		b, err := json.Marshal(val)
		if err != nil {
			continue
		}
		if _, err := w.prop.Exec(id, k, string(b)); err != nil {
			return err
		}
		items := []any{val}
		if list, ok := val.([]any); ok {
			items = list
		}
		for _, item := range items {
			if s, ok := scalarText(item); ok {
				if _, err := w.propValue.Exec(id, k, s); err != nil {
					return err
				}
			}
		}
	}
	for _, a := range d.aliases {
		if _, err := w.alias.Exec(id, key(a)); err != nil {
			return err
		}
	}
	for _, l := range d.links {
		if _, err := w.link.Exec(id, l.Target, pathKey(l.Target), l.Heading, l.Alias, l.Embed, l.Wiki); err != nil {
			return err
		}
	}
	for _, f := range d.files {
		if _, err := w.file.Exec(id, f); err != nil {
			return err
		}
	}
	_, err := w.text.Exec(id, d.title, d.body)
	return err
}
