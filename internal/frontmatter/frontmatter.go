// Package frontmatter reads and edits the YAML frontmatter of a markdown note.
//
// Edits are line-based: setting or deleting a key rewrites only that key's
// lines. Every other byte of the file (comments, key order, blank lines,
// quoting style, line endings and the body) is left exactly as it was.
// Values are parsed and encoded with goccy/go-yaml.
package frontmatter

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

var bom = []byte("\xef\xbb\xbf")

// Doc is a note split into its frontmatter and body.
type Doc struct {
	src []byte
	// has reports whether the note starts with a closed frontmatter block.
	has bool
	// prefix is an optional UTF-8 BOM before the opening delimiter.
	prefix int
	// fmStart and fmEnd delimit the YAML between the two delimiter lines.
	fmStart, fmEnd int
	// bodyStart is the first byte after the closing delimiter line.
	bodyStart int
	nl        string
}

// Split parses src into a Doc. A note has frontmatter only when its first
// line is "---" and a later line is "---" or "...", as in Obsidian.
func Split(src []byte) Doc {
	d := Doc{src: src, nl: "\n"}
	if bytes.Contains(src, []byte("\r\n")) {
		d.nl = "\r\n"
	}
	rest := src
	if bytes.HasPrefix(rest, bom) {
		d.prefix = len(bom)
		rest = rest[len(bom):]
	}
	first, n := line(rest)
	if strings.TrimRight(first, " \t") != "---" {
		return d
	}
	pos := d.prefix + n
	start := pos
	for pos < len(src) {
		l, n := line(src[pos:])
		if t := strings.TrimRight(l, " \t"); t == "---" || t == "..." {
			d.has = true
			d.fmStart, d.fmEnd = start, pos
			d.bodyStart = pos + n
			return d
		}
		pos += n
	}
	return d
}

// line returns the first line of b without its line ending, and the number
// of bytes it takes including the line ending.
func line(b []byte) (string, int) {
	i := bytes.IndexByte(b, '\n')
	if i < 0 {
		return string(b), len(b)
	}
	return strings.TrimSuffix(string(b[:i]), "\r"), i + 1
}

// Has reports whether the note has a frontmatter block.
func (d Doc) Has() bool { return d.has }

// Front returns the raw YAML between the delimiters.
func (d Doc) Front() []byte {
	if !d.has {
		return nil
	}
	return d.src[d.fmStart:d.fmEnd]
}

// Body returns everything after the frontmatter block.
func (d Doc) Body() []byte {
	if !d.has {
		return d.src
	}
	return d.src[d.bodyStart:]
}

// Properties decodes the frontmatter into a map. A note without frontmatter
// has no properties.
func (d Doc) Properties() (map[string]any, error) {
	out := map[string]any{}
	front := d.Front()
	if len(bytes.TrimSpace(front)) == 0 {
		return out, nil
	}
	if err := yaml.Unmarshal(front, &out); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	return out, nil
}

// keyRange is the byte range [start, end) of one top-level key's lines
// inside the frontmatter, relative to src.
type keyRange struct{ start, end int }

// findKey locates a top-level key. Its range runs from the key's line up to
// the next top-level line (another key, a comment at column 0, or the end).
// Trailing blank lines stay outside the range so spacing is kept.
func (d Doc) findKey(key string) (keyRange, bool) {
	pos := d.fmStart
	found := false
	var r keyRange
	lastContent := 0
	for pos < d.fmEnd {
		l, n := line(d.src[pos:d.fmEnd])
		top := l != "" && l[0] != ' ' && l[0] != '\t'
		if found && top {
			r.end = lastContent
			return r, true
		}
		if !found && top && topKey(l) == key {
			found = true
			r.start = pos
		}
		if strings.TrimSpace(l) != "" {
			lastContent = pos + n
		}
		pos += n
	}
	if found {
		r.end = lastContent
	}
	return r, found
}

// topKey returns the key of a top-level "key: value" line, or "".
func topKey(l string) string {
	if l == "" || l[0] == '#' || l[0] == '-' {
		return ""
	}
	i := strings.Index(l, ":")
	for i >= 0 && i+1 < len(l) && l[i+1] != ' ' && l[i+1] != '\t' {
		j := strings.Index(l[i+1:], ":")
		if j < 0 {
			i = -1
			break
		}
		i += 1 + j
	}
	if i < 0 {
		return ""
	}
	k := strings.TrimSpace(l[:i])
	if len(k) >= 2 && (k[0] == '"' && k[len(k)-1] == '"' || k[0] == '\'' && k[len(k)-1] == '\'') {
		k = k[1 : len(k)-1]
	}
	return k
}

// encodeLine renders "key: value" on one line, using flow style for lists
// and maps (tags: [a, b]) as Obsidian writes them.
func encodeLine(key string, value any) (string, error) {
	k, err := yaml.Marshal(key)
	if err != nil {
		return "", err
	}
	v, err := yaml.MarshalWithOptions(value, yaml.Flow(true))
	if err != nil {
		return "", err
	}
	vs := strings.TrimSpace(string(v))
	if strings.Contains(vs, "\n") {
		return "", errors.New("frontmatter: value does not fit on one line")
	}
	return strings.TrimSpace(string(k)) + ": " + vs, nil
}

// Set returns src with key set to value. Only the key's own lines change;
// a missing key is appended at the end of the block, and a note without
// frontmatter gets a new block.
func Set(src []byte, key string, value any) ([]byte, error) {
	d := Split(src)
	ln, err := encodeLine(key, value)
	if err != nil {
		return nil, err
	}
	if !d.has {
		var b bytes.Buffer
		b.Write(src[:d.prefix])
		b.WriteString("---" + d.nl + ln + d.nl + "---" + d.nl)
		b.Write(src[d.prefix:])
		return b.Bytes(), nil
	}
	if r, ok := d.findKey(key); ok {
		return splice(src, r.start, r.end, ln+d.nl), nil
	}
	ins := d.fmEnd
	prefix := ""
	if ins > d.fmStart && src[ins-1] != '\n' {
		prefix = d.nl
	}
	return splice(src, ins, ins, prefix+ln+d.nl), nil
}

// Delete returns src without key. Deleting a missing key returns src as is.
func Delete(src []byte, key string) []byte {
	d := Split(src)
	if !d.has {
		return src
	}
	r, ok := d.findKey(key)
	if !ok {
		return src
	}
	return splice(src, r.start, r.end, "")
}

func splice(src []byte, start, end int, s string) []byte {
	out := make([]byte, 0, len(src)-(end-start)+len(s))
	out = append(out, src[:start]...)
	out = append(out, s...)
	return append(out, src[end:]...)
}
