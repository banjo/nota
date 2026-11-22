package vault

import (
	"bytes"
	"path"
	"strings"
	"time"

	"github.com/banjo/nota/internal/frontmatter"
)

// Note is one markdown file in the vault.
type Note struct {
	// Path is the vault-relative, slash-separated NFC path, e.g. "Inbox/Möte.md".
	Path string `json:"path"`
	// Title comes from the first line of the body (without "# "), or the
	// file name when the body is empty.
	Title string `json:"title"`
	// Props is the decoded frontmatter. It is empty, not nil, without one.
	Props map[string]any `json:"properties"`
	// Body is everything after the frontmatter.
	Body string `json:"body"`
	// Raw is the whole file, byte for byte.
	Raw []byte `json:"-"`
	// ModTime and Size come from the file system.
	ModTime time.Time `json:"modified"`
	Size    int64     `json:"size"`
}

// Folder returns the note's vault-relative folder ("" for the root).
func (n *Note) Folder() string { return Folder(n.Path) }

// Name returns the file name without the extension.
func (n *Note) Name() string { return Stem(n.Path) }

// Folder returns the folder part of a vault-relative path ("" for the root).
func Folder(rel string) string {
	d := path.Dir(rel)
	if d == "." {
		return ""
	}
	return d
}

// Stem returns the file name of a vault-relative path without extension.
func Stem(rel string) string {
	b := path.Base(rel)
	return strings.TrimSuffix(b, path.Ext(b))
}

// ParseNote builds a Note from a file's contents. Broken frontmatter YAML
// does not fail the note: Props is then empty and the error is returned
// alongside the note so callers can warn about it.
func ParseNote(rel string, raw []byte) (*Note, error) {
	doc := frontmatter.Split(raw)
	props, err := doc.Properties()
	if props == nil {
		props = map[string]any{}
	}
	n := &Note{
		Path:  rel,
		Props: props,
		Body:  string(doc.Body()),
		Raw:   raw,
		Size:  int64(len(raw)),
	}
	n.Title = Title(doc.Body(), Stem(rel))
	return n, err
}

// maxTitleRunes caps titles taken from a first line that runs on.
const maxTitleRunes = 200

// Title returns the title of a note body: its first non-empty line with
// any ATX heading marker ("# ", "## " …) removed, or fallback when the
// body has no text.
func Title(body []byte, fallback string) string {
	for len(body) > 0 {
		var l []byte
		if i := bytes.IndexByte(body, '\n'); i >= 0 {
			l, body = body[:i], body[i+1:]
		} else {
			l, body = body, nil
		}
		s := strings.TrimSpace(string(l))
		if s == "" {
			continue
		}
		s = strings.TrimSpace(stripHeading(s))
		if s == "" {
			continue
		}
		if r := []rune(s); len(r) > maxTitleRunes {
			s = string(r[:maxTitleRunes])
		}
		return NFC(s)
	}
	return fallback
}

// stripHeading removes an ATX heading marker and its optional closing run
// of "#". "#tag" is not a heading and is kept.
func stripHeading(s string) string {
	i := 0
	for i < len(s) && i < 7 && s[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || (i < len(s) && s[i] != ' ' && s[i] != '\t') {
		return s
	}
	s = strings.TrimSpace(s[i:])
	if t := strings.TrimRight(s, "#"); t != s && (t == "" || strings.HasSuffix(t, " ")) {
		s = t
	}
	return s
}
