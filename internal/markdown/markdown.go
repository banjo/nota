// Package markdown extracts what the index needs from a note body: tags
// (#tag, nested #a/b), wikilinks ([[Title]], [[path|alias]], [[x#heading]]),
// embeds (![[x]]) and links to local notes ([text](other.md)).
//
// goldmark finds code blocks, code spans and raw HTML, which are masked
// out first, so a "#define" in a code block is not a tag. The Obsidian
// syntax itself is matched with small regular expressions on what is left.
package markdown

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Link is one outgoing link of a note.
type Link struct {
	// Target is the link target without heading or block reference, as
	// written: "Title", "folder/Note", "Note.md". For markdown links it is
	// the destination resolved against the note's folder.
	Target string `json:"target"`
	// Heading is the part after "#" (a heading, or "^id" for a block).
	Heading string `json:"heading,omitempty"`
	// Alias is the display text after "|".
	Alias string `json:"alias,omitempty"`
	// Embed is true for ![[x]].
	Embed bool `json:"embed,omitempty"`
	// Wiki is true for [[…]] links, false for [text](dest.md) links.
	Wiki bool `json:"wiki"`
}

// Parsed is the result of Parse.
type Parsed struct {
	Tags  []string
	Links []Link
}

var (
	md         = goldmark.New()
	wikiRe     = regexp.MustCompile(`(!?)\[\[([^\[\]\n]+?)\]\]`)
	mdLinkRe   = regexp.MustCompile(`(!?)\[[^\]\n]*\]\(<?([^)\s>]+)>?(?:\s+"[^"]*")?\)`)
	tagRe      = regexp.MustCompile(`(?:^|[\s(\[,;])#([\p{L}\p{N}_/\-]+)`)
	schemeLike = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*:`)
)

// Parse extracts tags and links from a note body (without frontmatter).
// folder is the note's vault-relative folder, used to resolve relative
// markdown links.
func Parse(body []byte, folder string) Parsed {
	src := mask(body)
	var p Parsed
	seenTag := map[string]bool{}
	for _, m := range tagRe.FindAllSubmatch(src, -1) {
		if t := CleanTag(string(m[1])); t != "" && !seenTag[strings.ToLower(t)] {
			seenTag[strings.ToLower(t)] = true
			p.Tags = append(p.Tags, t)
		}
	}
	for _, m := range wikiRe.FindAllSubmatch(src, -1) {
		if l, ok := parseWiki(string(m[2])); ok {
			l.Embed = len(m[1]) > 0
			p.Links = append(p.Links, l)
		}
	}
	for _, m := range mdLinkRe.FindAllSubmatch(src, -1) {
		if l, ok := parseMarkdownLink(string(m[2]), folder); ok {
			l.Embed = len(m[1]) > 0
			p.Links = append(p.Links, l)
		}
	}
	return p
}

// CleanTag normalizes a tag: no leading "#", no trailing "/", and at least
// one character that is not a digit (Obsidian treats "#2026" as text).
// It returns "" for something that is not a tag.
func CleanTag(t string) string {
	t = strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")), "/")
	if t == "" {
		return ""
	}
	for _, r := range t {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/') {
			return ""
		}
	}
	if strings.IndexFunc(t, func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
		return ""
	}
	return t
}

func parseWiki(inner string) (Link, bool) {
	l := Link{Wiki: true}
	target := inner
	if i := strings.IndexByte(inner, '|'); i >= 0 {
		// "\|" is the alias bar inside a table, where "|" splits cells.
		target, l.Alias = strings.TrimSuffix(inner[:i], `\`), strings.TrimSpace(inner[i+1:])
	}
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target, l.Heading = target[:i], strings.TrimSpace(target[i+1:])
	}
	l.Target = strings.TrimSpace(target)
	return l, l.Target != "" // [[#heading]] points into the same note
}

func parseMarkdownLink(dest, folder string) (Link, bool) {
	if schemeLike.MatchString(dest) || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "//") {
		return Link{}, false
	}
	heading := ""
	if i := strings.IndexByte(dest, '#'); i >= 0 {
		dest, heading = dest[:i], dest[i+1:]
	}
	if u, err := url.PathUnescape(dest); err == nil {
		dest = u
	}
	if !strings.EqualFold(path.Ext(dest), ".md") {
		return Link{}, false
	}
	var p string
	if strings.HasPrefix(dest, "/") {
		p = path.Clean(dest)
	} else {
		p = path.Clean("/" + path.Join(folder, dest))
	}
	if strings.HasPrefix(p, "/..") {
		return Link{}, false
	}
	return Link{Target: strings.TrimPrefix(p, "/"), Heading: heading}, true
}

// mask returns a copy of body with code blocks, code spans and raw HTML
// replaced by spaces (newlines kept), so they yield no tags or links.
func mask(body []byte) []byte {
	out := append([]byte(nil), body...)
	blank := func(start, stop int) {
		for i := start; i < stop && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	blankLines := func(n ast.Node) {
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			s := lines.At(i)
			blank(s.Start, s.Stop)
		}
	}
	doc := md.Parser().Parse(text.NewReader(body))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.FencedCodeBlock:
			blankLines(n)
			if n.Info != nil {
				blank(n.Info.Segment.Start, n.Info.Segment.Stop)
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock, *ast.HTMLBlock:
			blankLines(n)
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					blank(t.Segment.Start, t.Segment.Stop)
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			for i := 0; i < n.Segments.Len(); i++ {
				s := n.Segments.At(i)
				blank(s.Start, s.Stop)
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return out
}
