package index

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/banjo/nota/internal/vault"
)

// Entry is one note as the index knows it.
type Entry struct {
	Path     string         `json:"path"`
	Folder   string         `json:"folder"`
	Title    string         `json:"title"`
	Tags     []string       `json:"tags"`
	Props    map[string]any `json:"properties"`
	Created  time.Time      `json:"created"`
	Updated  time.Time      `json:"updated"`
	Modified time.Time      `json:"modified"`
	Size     int64          `json:"size"`
	// Snippet is the matching text, for search results.
	Snippet string `json:"snippet,omitempty"`
}

// Cond is one property condition of a query.
type Cond struct {
	Key   string
	Value string
	// Not inverts the condition: the key is missing or has another value.
	Not bool
}

// ParseCond parses "key=value" or "key!=value".
func ParseCond(s string) (Cond, error) {
	if i := strings.Index(s, "!="); i > 0 {
		return Cond{Key: strings.TrimSpace(s[:i]), Value: strings.TrimSpace(s[i+2:]), Not: true}, nil
	}
	if i := strings.IndexByte(s, '='); i > 0 {
		return Cond{Key: strings.TrimSpace(s[:i]), Value: strings.TrimSpace(s[i+1:])}, nil
	}
	return Cond{}, fmt.Errorf("index: condition %q is not key=value or key!=value", s)
}

// Sort orders query results.
type Sort struct {
	// Field is one of "path", "title", "created", "updated", "modified".
	Field string
	Desc  bool
}

var sortColumns = map[string]string{
	"path":     "n.path",
	"title":    "n.title_key",
	"created":  "n.created",
	"updated":  "n.updated",
	"modified": "n.mtime",
}

// ParseSort parses "updated desc", "title", "created asc" and so on. The
// empty string sorts by path.
func ParseSort(s string) (Sort, error) {
	f := strings.Fields(strings.ToLower(s))
	if len(f) == 0 {
		return Sort{Field: "path"}, nil
	}
	if _, ok := sortColumns[f[0]]; !ok || len(f) > 2 {
		return Sort{}, fmt.Errorf("index: sort %q: want <path|title|created|updated|modified> [asc|desc]", s)
	}
	srt := Sort{Field: f[0]}
	if len(f) == 2 {
		switch f[1] {
		case "desc":
			srt.Desc = true
		case "asc":
		default:
			return Sort{}, fmt.Errorf("index: sort %q: direction must be asc or desc", s)
		}
	}
	return srt, nil
}

// Query selects notes. Empty fields do not filter.
type Query struct {
	// Folder limits results to a folder and its subfolders.
	Folder string
	// Tag matches the tag and its nested tags: "a" matches "a" and "a/b".
	Tag   string
	Where []Cond
	Sort  Sort
	Limit int
}

// filter renders the WHERE clause shared by Query and Search.
func (q Query) filter() (string, []any) {
	var where []string
	var args []any
	if f := strings.Trim(vault.NFC(q.Folder), "/"); f != "" {
		where = append(where, `(n.folder = ? OR (n.folder > ? AND n.folder < ?))`)
		args = append(args, f, f+"/", f+"0") // '0' sorts right after '/'
	}
	if t := key(strings.TrimPrefix(q.Tag, "#")); t != "" {
		where = append(where, `EXISTS (SELECT 1 FROM tags t WHERE t.note_id = n.id AND (t.tag_key = ? OR (t.tag_key > ? AND t.tag_key < ?)))`)
		args = append(args, t, t+"/", t+"0")
	}
	for _, c := range q.Where {
		sub := `EXISTS (SELECT 1 FROM property_values pv WHERE pv.note_id = n.id AND pv.key = ? AND pv.value = ?)`
		if c.Not {
			sub = "NOT " + sub
		}
		where = append(where, sub)
		args = append(args, c.Key, c.Value)
	}
	if len(where) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

const entryCols = `n.path, n.folder, n.title, n.props, n.created, n.updated, n.mtime, n.size,
	(SELECT group_concat(t.tag, char(31) ORDER BY t.pos) FROM tags t WHERE t.note_id = n.id)`

func scanEntries(rows *sql.Rows, snippet bool) ([]Entry, error) {
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var props string
		var created, updated, mtime int64
		var tags sql.NullString
		dst := []any{&e.Path, &e.Folder, &e.Title, &props, &created, &updated, &mtime, &e.Size, &tags}
		if snippet {
			dst = append(dst, &e.Snippet)
		}
		if err := rows.Scan(dst...); err != nil {
			return nil, err
		}
		e.Props = map[string]any{}
		_ = json.Unmarshal([]byte(props), &e.Props)
		e.Created = time.UnixMilli(created)
		e.Updated = time.UnixMilli(updated)
		e.Modified = time.Unix(0, mtime)
		e.Tags = []string{}
		if tags.Valid && tags.String != "" {
			e.Tags = strings.Split(tags.String, "\x1f")
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Query returns the notes matching q.
func (ix *Index) Query(q Query) ([]Entry, error) {
	where, args := q.filter()
	col, ok := sortColumns[q.Sort.Field]
	if !ok {
		col = "n.path"
	}
	order := col
	if q.Sort.Desc {
		order += " DESC"
	}
	s := `SELECT ` + entryCols + ` FROM notes n` + where + ` ORDER BY ` + order + `, n.path`
	if q.Limit > 0 {
		s += fmt.Sprintf(" LIMIT %d", q.Limit)
	}
	rows, err := ix.db.Query(s, args...)
	if err != nil {
		return nil, fmt.Errorf("index: query: %w", err)
	}
	return scanEntries(rows, false)
}

// Get returns one note by its exact path.
func (ix *Index) Get(p string) (Entry, error) {
	rows, err := ix.db.Query(`SELECT `+entryCols+` FROM notes n WHERE n.path = ?`, p)
	if err != nil {
		return Entry{}, err
	}
	es, err := scanEntries(rows, false)
	if err != nil {
		return Entry{}, err
	}
	if len(es) == 0 {
		return Entry{}, &vault.PathError{Op: "index", Path: p, Err: vault.ErrNotFound}
	}
	return es[0], nil
}

// Count returns the number of indexed notes.
func (ix *Index) Count() (int, error) {
	var n int
	err := ix.db.QueryRow(`SELECT count(*) FROM notes`).Scan(&n)
	return n, err
}

// ErrEmptyQuery is returned by Search for a query without search terms.
var ErrEmptyQuery = errors.New("index: empty search query")

// Search runs a full-text search over titles and bodies, best match
// first; title matches weigh more. q is plain text: words must all occur
// (the last one as a prefix), "quoted phrases" match as phrases, -word
// excludes and OR between words means either. The filters in filter
// (folder, tag, where) apply as in Query; its Sort is ignored.
func (ix *Index) Search(q string, filter Query) ([]Entry, error) {
	match, err := ftsQuery(q)
	if err != nil {
		return nil, err
	}
	where, args := filter.filter()
	if where == "" {
		where = " WHERE "
	} else {
		where += " AND "
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	s := `SELECT ` + entryCols + `, snippet(fts, 1, '', '', '…', 12)
		FROM fts JOIN notes n ON n.id = fts.rowid` + where + `fts MATCH ?
		ORDER BY bm25(fts, 10.0, 1.0), n.path LIMIT ?`
	rows, err := ix.db.Query(s, append(args, match, limit)...)
	if err != nil {
		return nil, fmt.Errorf("index: search: %w", err)
	}
	return scanEntries(rows, true)
}

// ftsQuery turns user input into an FTS5 query with every term quoted, so
// characters such as "-" or ":" in words never cause syntax errors.
func ftsQuery(q string) (string, error) {
	type term struct {
		text          string
		phrase, neg   bool
		or            bool
		prefixAllowed bool
	}
	var terms []term
	for i := 0; i < len(q); {
		r := rune(q[i])
		if unicode.IsSpace(r) {
			i++
			continue
		}
		t := term{}
		if q[i] == '-' && i+1 < len(q) && !unicode.IsSpace(rune(q[i+1])) {
			t.neg = true
			i++
		}
		if i < len(q) && q[i] == '"' {
			end := strings.IndexByte(q[i+1:], '"')
			if end < 0 {
				end = len(q) - i - 1
			}
			t.text, t.phrase = q[i+1:i+1+end], true
			i += end + 2
		} else {
			end := strings.IndexFunc(q[i:], unicode.IsSpace)
			if end < 0 {
				end = len(q) - i
			}
			t.text = q[i : i+end]
			i += end
			if t.text == "OR" && !t.neg {
				t.or = true
			}
			t.prefixAllowed = true
		}
		if strings.TrimSpace(t.text) != "" || t.or {
			terms = append(terms, t)
		}
	}
	var pos, neg []string
	lastPos := -1
	for i, t := range terms {
		if !t.neg && !t.or {
			lastPos = i
		}
	}
	for i, t := range terms {
		quoted := `"` + strings.ReplaceAll(t.text, `"`, `""`) + `"`
		switch {
		case t.or:
			if len(pos) > 0 && i < len(terms)-1 {
				pos = append(pos, "OR")
			}
		case t.neg:
			neg = append(neg, quoted)
		default:
			if i == lastPos && t.prefixAllowed {
				quoted += "*"
			}
			pos = append(pos, quoted)
		}
	}
	if len(pos) == 0 || pos[len(pos)-1] == "OR" {
		if len(pos) == 0 {
			return "", ErrEmptyQuery
		}
		pos = pos[:len(pos)-1]
	}
	out := strings.Join(pos, " ")
	for _, n := range neg {
		out = "(" + out + ") NOT " + n
	}
	return out, nil
}

// Link is a resolved link between two notes.
type Link struct {
	// From is the linking note, To the linked one ("" when unresolved).
	From    string `json:"from"`
	To      string `json:"to"`
	Target  string `json:"target"`
	Heading string `json:"heading,omitempty"`
	Alias   string `json:"alias,omitempty"`
	Embed   bool   `json:"embed,omitempty"`
}

// Links returns the outgoing links of a note, resolved like Obsidian does.
func (ix *Index) Links(p string) ([]Link, error) {
	rows, err := ix.db.Query(`SELECT l.target, l.target_key, l.heading, l.alias, l.embed, l.wiki
		FROM links l JOIN notes n ON n.id = l.note_id WHERE n.path = ? ORDER BY l.rowid`, p)
	if err != nil {
		return nil, err
	}
	type raw struct {
		Link
		key  string
		wiki bool
	}
	var rs []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.Target, &r.key, &r.Heading, &r.Alias, &r.Embed, &r.wiki); err != nil {
			rows.Close()
			return nil, err
		}
		r.From = p
		rs = append(rs, r)
	}
	rows.Close()
	out := []Link{}
	for _, r := range rs {
		r.To, err = ix.resolveLink(r.key, p, r.wiki)
		if err != nil {
			return nil, err
		}
		out = append(out, r.Link)
	}
	return out, rows.Err()
}

// Backlinks returns the links from other notes (or the note itself) that
// resolve to p.
func (ix *Index) Backlinks(p string) ([]Link, error) {
	e, err := ix.Get(p)
	if err != nil {
		return nil, err
	}
	// Every key a link to p could use: path suffixes, title, aliases.
	keys := map[string]bool{key(e.Title): true}
	pk := pathKey(p)
	for {
		keys[pk] = true
		i := strings.IndexByte(pk, '/')
		if i < 0 {
			break
		}
		pk = pk[i+1:]
	}
	arows, err := ix.db.Query(`SELECT a.alias_key FROM aliases a JOIN notes n ON n.id = a.note_id WHERE n.path = ?`, p)
	if err != nil {
		return nil, err
	}
	for arows.Next() {
		var a string
		if err := arows.Scan(&a); err == nil {
			keys[a] = true
		}
	}
	arows.Close()
	ph := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys))
	for k := range keys {
		ph = append(ph, "?")
		args = append(args, k)
	}
	rows, err := ix.db.Query(`SELECT n.path, l.target, l.target_key, l.heading, l.alias, l.embed, l.wiki
		FROM links l JOIN notes n ON n.id = l.note_id
		WHERE l.target_key IN (`+strings.Join(ph, ",")+`) ORDER BY n.path, l.rowid`, args...)
	if err != nil {
		return nil, err
	}
	type cand struct {
		Link
		key  string
		wiki bool
	}
	var cs []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.From, &c.Target, &c.key, &c.Heading, &c.Alias, &c.Embed, &c.wiki); err != nil {
			rows.Close()
			return nil, err
		}
		cs = append(cs, c)
	}
	rows.Close()
	out := []Link{}
	for _, c := range cs {
		to, err := ix.resolveLink(c.key, c.From, c.wiki)
		if err != nil {
			return nil, err
		}
		if to == p {
			c.To = to
			out = append(out, c.Link)
		}
	}
	return out, nil
}

// Resolve resolves a wikilink target as written in the note at from.
func (ix *Index) Resolve(target, from string) (string, error) {
	return ix.resolveLink(pathKey(target), from, true)
}

// resolveLink finds the note a link points to, the way Obsidian does: an
// exact vault path, a path relative to the linking note, then a path
// suffix or file name (closest to the linking note wins), then a title,
// then an alias. Markdown links only resolve by exact path.
func (ix *Index) resolveLink(k, from string, wiki bool) (string, error) {
	if k == "" {
		return "", nil
	}
	pick := func(q string, args ...any) (string, error) {
		rows, err := ix.db.Query(q, args...)
		if err != nil {
			return "", err
		}
		var cands []string
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return "", err
			}
			cands = append(cands, p)
		}
		rows.Close()
		return closest(cands, from), rows.Err()
	}
	if p, err := pick(`SELECT path FROM notes WHERE path_key = ?`, k); p != "" || err != nil || !wiki {
		return p, err
	}
	if rel := pathKey(path.Join(vault.Folder(from), k)); rel != k {
		if p, err := pick(`SELECT path FROM notes WHERE path_key = ?`, rel); p != "" || err != nil {
			return p, err
		}
	}
	if strings.Contains(k, "/") {
		if p, err := pick(`SELECT path FROM notes WHERE path_key LIKE ? ESCAPE '\'`, "%/"+escapeLike(k)); p != "" || err != nil {
			return p, err
		}
	} else if p, err := pick(`SELECT path FROM notes WHERE name = ?`, k); p != "" || err != nil {
		return p, err
	}
	if p, err := pick(`SELECT path FROM notes WHERE title_key = ?`, k); p != "" || err != nil {
		return p, err
	}
	return pick(`SELECT n.path FROM aliases a JOIN notes n ON n.id = a.note_id WHERE a.alias_key = ?`, k)
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// closest picks the candidate nearest to the note at from: same folder
// first, then the shortest path, then lexical order.
func closest(cands []string, from string) string {
	if len(cands) == 0 {
		return ""
	}
	dir := vault.Folder(from)
	sort.Slice(cands, func(i, j int) bool {
		si, sj := vault.Folder(cands[i]) == dir, vault.Folder(cands[j]) == dir
		if si != sj {
			return si
		}
		if len(cands[i]) != len(cands[j]) {
			return len(cands[i]) < len(cands[j])
		}
		return cands[i] < cands[j]
	})
	return cands[0]
}

// Lookup finds notes for a user-supplied reference such as a CLI argument:
// an exact path (".md" optional), else a title or file name (any case),
// else a prefix of a path, title or file name. It returns every candidate
// of the first rule that matches, sorted by path.
func (ix *Index) Lookup(ref string) ([]string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if c, err := vault.CleanPath(ref); err == nil {
		ref = c
	}
	k := pathKey(ref)
	tk := key(ref)
	steps := []struct {
		q    string
		args []any
	}{
		{`SELECT path FROM notes WHERE path = ? OR path = ?`, []any{ref, ref + vault.Ext}},
		{`SELECT path FROM notes WHERE path_key = ?`, []any{k}},
		{`SELECT path FROM notes WHERE title_key = ? OR name = ?`, []any{tk, k}},
		{`SELECT path FROM notes WHERE path_key LIKE ? ESCAPE '\' OR title_key LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\'`,
			[]any{escapeLike(k) + "%", escapeLike(tk) + "%", escapeLike(k) + "%"}},
	}
	for _, s := range steps {
		rows, err := ix.db.Query(s.q+` ORDER BY path`, s.args...)
		if err != nil {
			return nil, err
		}
		var out []string
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, p)
		}
		rows.Close()
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, nil
}

// TagCount is a tag with the number of notes that carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Tags returns every tag in the vault with its note count, by name.
func (ix *Index) Tags() ([]TagCount, error) {
	rows, err := ix.db.Query(`SELECT min(tag), count(*) FROM tags GROUP BY tag_key ORDER BY tag_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
