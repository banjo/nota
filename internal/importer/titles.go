package importer

import (
	"bytes"
	"strings"
	"unicode"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/vault"
)

// titleHeading keeps a note's title when it comes into Nota. Obsidian,
// Logseq and other apps that keep notes as files take the title from
// the file name; Nota takes it from the first line (vault.Title). A note
// whose first line says something else, such as a greeting or a section
// heading, gets its name as a heading on top. One whose first line is
// its name already, give or take case, punctuation and emoji, is left as
// it is, and so is an empty one: Nota titles that by its name too.
func titleHeading(data []byte, name string) []byte {
	name = strings.TrimSpace(name)
	doc := frontmatter.Split(data)
	body := doc.Body()
	title := vault.Title(body, "")
	if name == "" || title == "" || words(title) == words(name) {
		return data
	}
	nl := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		nl = "\r\n"
	}
	head := data[:len(data)-len(body)]
	for {
		i := bytes.IndexByte(body, '\n')
		if i < 0 || len(bytes.TrimSpace(body[:i])) > 0 {
			break
		}
		body = body[i+1:]
	}
	out := make([]byte, 0, len(data)+len(name)+8)
	out = append(out, head...)
	out = append(out, "# "+name+nl+nl...)
	return append(out, body...)
}

// words is s without what titles differ in: case, punctuation, symbols
// and spaces.
func words(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, vault.NFC(s))
}
