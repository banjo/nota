package export

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/publish"
	"github.com/banjo/nota/internal/vault"
)

var (
	wikiRe    = regexp.MustCompile(`(!?)\[\[([^\[\]\n]+?)\]\]`)
	mdLinkRe  = regexp.MustCompile(`(!?)\[([^\]\n]*)\]\((?:<([^<>\n]+)>|([^)\s<>]+))(\s+"[^"]*")?\)`)
	commentRe = regexp.MustCompile(`(?s)%%.*?%%`)
	blockIDRe = regexp.MustCompile(`(?m)(^|[ \t])\^[A-Za-z0-9-]+[ \t]*$`)
	inlineFn  = regexp.MustCompile(`\^\[([^\[\]\n]+)\]`)
	mdEscape  = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `*`, `\*`, `_`, `\_`, "`", "\\`")
)

// markdown writes a note in Markdown most apps read: the frontmatter as
// it is; wikilinks and embeds as Markdown links and images (to the files
// of the export, else their text); Markdown links to notes and images
// left out of the export as their text; %%comments%% and block ids
// removed; inline footnotes as numbered ones. Code is left alone.
func (m *mapper) markdown(raw []byte, from string) []byte {
	doc := frontmatter.Split(raw)
	body := doc.Body()
	front := raw[:len(raw)-len(body)]
	folder := vault.Folder(from)

	code := markdown.CodeRanges(body)
	inCode := func(s int) bool {
		for _, c := range code {
			if s >= c[0] && s < c[1] {
				return true
			}
		}
		return false
	}
	type edit struct {
		start, end int
		with       string
	}
	var edits []edit
	for _, x := range commentRe.FindAllIndex(body, -1) {
		if !inCode(x[0]) {
			edits = append(edits, edit{x[0], x[1], ""})
		}
	}
	for _, x := range blockIDRe.FindAllSubmatchIndex(body, -1) {
		if !inCode(x[0]) {
			edits = append(edits, edit{x[0], x[1], ""})
		}
	}
	for _, x := range wikiRe.FindAllSubmatchIndex(body, -1) {
		if !inCode(x[0]) {
			edits = append(edits, edit{x[0], x[1], m.wiki(string(body[x[4]:x[5]]), x[3] > x[2], from)})
		}
	}
	for _, x := range mdLinkRe.FindAllSubmatchIndex(body, -1) {
		if inCode(x[0]) {
			continue
		}
		dest := ""
		if x[6] >= 0 {
			dest = string(body[x[6]:x[7]])
		} else {
			dest = string(body[x[8]:x[9]])
		}
		if with, ok := m.mdLink(string(body[x[4]:x[5]]), dest, x[3] > x[2], from, folder); ok {
			title := ""
			if x[10] >= 0 {
				title = string(body[x[10]:x[11]])
			}
			if strings.HasSuffix(with, ")") && title != "" {
				with = with[:len(with)-1] + title + ")"
			}
			edits = append(edits, edit{x[0], x[1], with})
		}
	}
	var notes []string
	for _, x := range inlineFn.FindAllSubmatchIndex(body, -1) {
		if inCode(x[0]) || x[0] > 0 && body[x[0]-1] == '!' {
			continue
		}
		notes = append(notes, string(body[x[2]:x[3]]))
		edits = append(edits, edit{x[0], x[1], fmt.Sprintf("[^inline-%d]", len(notes))})
	}
	// Outer edits win: a link inside a comment goes with the comment.
	slices.SortFunc(edits, func(a, b edit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return b.end - a.end
	})
	out := append([]byte(nil), front...)
	at := 0
	for _, e := range edits {
		if e.start < at {
			continue
		}
		out = append(out, body[at:e.start]...)
		out = append(out, e.with...)
		at = e.end
	}
	out = append(out, body[at:]...)
	if len(notes) > 0 {
		out = append(bytes.TrimRight(out, "\n"), '\n')
		for i, n := range notes {
			out = fmt.Appendf(out, "\n[^inline-%d]: %s\n", i+1, n)
		}
	}
	return out
}

// wiki rewrites the inside of a [[wikilink]] or ![[embed]].
func (m *mapper) wiki(inner string, embed bool, from string) string {
	l := markdown.ParseWiki(inner)
	if embed {
		if p := m.resolveImage(l.Target, from, true); p != "" {
			alt := l.Alias
			if alt != "" && isSize(alt) {
				alt = ""
			}
			url, _ := m.image(p, from)
			return "![" + mdEscape.Replace(alt) + "](" + url + ")"
		}
	}
	label := l.Alias
	if label == "" {
		label = strings.TrimSuffix(l.Target, ".md")
		if h := strings.TrimPrefix(l.Heading, "^"); h != "" {
			label = strings.TrimPrefix(label+" › "+h, " › ")
		}
	}
	label = mdEscape.Replace(label)
	anchor := ""
	if l.Heading != "" && !strings.HasPrefix(l.Heading, "^") {
		parts := strings.Split(l.Heading, "#")
		anchor = "#" + publish.Slug(parts[len(parts)-1])
	}
	if l.Target == "" {
		if anchor == "" {
			return label
		}
		return "[" + label + "](" + anchor + ")"
	}
	p := m.resolveNote(l, from)
	if p == "" {
		return label
	}
	url, ok := m.note(p, from)
	if !ok || url+anchor == "" {
		return label
	}
	return "[" + label + "](" + url + anchor + ")"
}

// mdLink rewrites a Markdown link or image to a local file: ok false to
// leave it as written (a web link, #heading).
func (m *mapper) mdLink(text, dest string, image bool, from, folder string) (string, bool) {
	l, isFile, ok := markdown.ParseLocalLink(dest, folder)
	if !ok {
		return "", false
	}
	bang := ""
	if image {
		bang = "!"
	}
	if isFile {
		if m.v.HasFile(l.Target) {
			p := l.Target
			url, _ := m.image(p, from)
			return bang + "[" + text + "](" + url + ")", true
		}
		return text, true
	}
	l.Wiki = false
	p := m.resolveNote(l, from)
	if p == "" {
		return text, true
	}
	url, ok := m.note(p, from)
	if !ok {
		return text, true
	}
	if l.Heading != "" {
		url += "#" + l.Heading
	}
	if url == "" {
		return text, true
	}
	return bang + "[" + text + "](" + url + ")", true
}

func isSize(s string) bool {
	w, _, _ := strings.Cut(s, "x")
	if w == "" {
		return false
	}
	for _, r := range w {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
