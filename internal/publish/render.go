// Package publish renders a note as a static HTML page, for shared
// links: the Markdown Nota reads (docs/markdown.md) as Reading mode shows
// it, without the editor and without any script. Links and images go
// through a Resolver, so the caller decides what a reader may follow and
// see: a note that isn't shared is plain text, and only images of the
// vault are shown.
package publish

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/markdown"
)

// Resolver decides where links and images of a note lead.
type Resolver interface {
	// Note returns the URL of a linked note, ok false to show the link's
	// text only. A wikilink's Target is the name as written; a markdown
	// link's is the vault path it resolves to.
	Note(l markdown.Link) (href string, ok bool)
	// Image returns the URL of an image of the vault: target is a
	// ![[name]] as written (wiki) or the vault path of ![](path).
	Image(target string, wiki bool) (src string, ok bool)
}

// Options are the note's place in the vault and its Resolver.
type Options struct {
	// Path is the note's vault path; relative links resolve from its folder.
	Path     string
	Resolver Resolver
}

// Render returns a note's body (frontmatter left out) as HTML.
func Render(raw []byte, o Options) template.HTML {
	return render(frontmatter.Split(raw).Body(), o)
}

// RenderNote splits a note as the app shows it: its title (the first
// line of the body, without "# "; the file name when that is empty) and
// the rest of the body as HTML.
func RenderNote(raw []byte, o Options) (title string, body template.HTML) {
	b := frontmatter.Split(raw).Body()
	first, rest, _ := bytes.Cut(b, []byte("\n"))
	title = strings.TrimSpace(titlePrefix.ReplaceAllString(string(first), ""))
	if title == "" {
		title = strings.TrimSuffix(path.Base(o.Path), ".md")
		rest = b
	}
	return title, render(rest, o)
}

var titlePrefix = regexp.MustCompile(`^#{1,6}[ \t]+`)

func render(body []byte, o Options) template.HTML {
	body = prepare(body)
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote, notaSyntax{}),
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(&transformer{o: o}, 100))),
		goldmark.WithRendererOptions(html.WithXHTML()),
	)
	var buf bytes.Buffer
	if err := md.Convert(body, &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(string(body)))
	}
	return template.HTML(buf.String()) //nolint:gosec // goldmark escapes; raw HTML is limited to a few tags below
}

var (
	wikiRe    = regexp.MustCompile(`(!?)\[\[([^\[\]\n]+?)\]\]`)
	commentRe = regexp.MustCompile(`(?s)%%.*?%%`)
	blockIDRe = regexp.MustCompile(`(?m)(^|[ \t])\^[A-Za-z0-9-]+[ \t]*$`)
	inlineFn  = regexp.MustCompile(`\^\[([^\[\]\n]+)\]`)
	imageExts = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg", ".bmp"}
)

const (
	linkScheme  = "nota-link:"
	embedScheme = "nota-embed:"
)

// prepare rewrites what goldmark does not know into what it does, outside
// code: %%comments%% and block ids go, wikilinks become links with a
// nota-link: destination (embeds images with nota-embed:), and inline
// footnotes become numbered ones.
func prepare(body []byte) []byte {
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
	for _, m := range commentRe.FindAllIndex(body, -1) {
		if !inCode(m[0]) {
			edits = append(edits, edit{m[0], m[1], ""})
		}
	}
	for _, m := range blockIDRe.FindAllSubmatchIndex(body, -1) {
		if !inCode(m[0]) {
			edits = append(edits, edit{m[0], m[1], string(body[m[2]:m[3]])})
		}
	}
	for _, m := range wikiRe.FindAllSubmatchIndex(body, -1) {
		if inCode(m[0]) {
			continue
		}
		inner := string(body[m[4]:m[5]])
		if m[3] > m[2] {
			edits = append(edits, edit{m[0], m[1], "![](<" + embedScheme + url.PathEscape(inner) + ">)"})
			continue
		}
		l := markdown.ParseWiki(inner)
		label := l.Alias
		if label == "" {
			label = strings.TrimSuffix(l.Target, ".md")
			if l.Heading != "" {
				label = strings.TrimPrefix(label+" › "+strings.TrimPrefix(l.Heading, "^"), " › ")
			}
		}
		edits = append(edits, edit{m[0], m[1], "[" + escapeText(label) + "](<" + linkScheme + url.PathEscape(inner) + ">)"})
	}
	var notes []string
	for _, m := range inlineFn.FindAllSubmatchIndex(body, -1) {
		if inCode(m[0]) {
			continue
		}
		notes = append(notes, string(body[m[2]:m[3]]))
		edits = append(edits, edit{m[0], m[1], fmt.Sprintf("[^nota-inline-%d]", len(notes))})
	}
	// Outer edits win: a link inside a comment goes with the comment.
	slices.SortFunc(edits, func(a, b edit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return b.end - a.end
	})
	kept := edits[:0]
	end := -1
	for _, e := range edits {
		if e.start >= end {
			kept = append(kept, e)
			end = e.end
		}
	}
	out := append([]byte(nil), body...)
	for i := len(kept) - 1; i >= 0; i-- {
		e := kept[i]
		out = append(out[:e.start], append([]byte(e.with), out[e.end:]...)...)
	}
	for i, n := range notes {
		out = fmt.Appendf(out, "\n\n[^nota-inline-%d]: %s\n", i+1, n)
	}
	return out
}

// escapeText backslash-escapes Markdown punctuation, so a link label is
// shown as written.
func escapeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && unicode.IsPunct(r) || r == '`' || r == '^' || r == '|' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Slug is a heading's anchor as GitHub (and Nota) make it: "Mål & plan!"
// is "mål--plan".
func Slug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r) || r == '_' || r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('-')
		}
	}
	return b.String()
}

func isImage(p string) bool {
	return slices.Contains(imageExts, strings.ToLower(path.Ext(p)))
}

// transformer resolves links and images, gives headings their anchors and
// turns "> [!type]" quotes into callouts.
type transformer struct{ o Options }

func (t *transformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	folder := path.Dir(t.o.Path)
	if folder == "." {
		folder = ""
	}
	var links, images, headings, quotes []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindLink:
			links = append(links, n)
		case ast.KindImage:
			images = append(images, n)
		case ast.KindHeading:
			headings = append(headings, n)
		case ast.KindBlockquote:
			quotes = append(quotes, n)
		}
		return ast.WalkContinue, nil
	})
	used := map[string]int{}
	for _, h := range headings {
		id := Slug(string(plain(h, src)))
		if id == "" {
			id = "section"
		}
		if n := used[id]; n > 0 {
			used[id] = n + 1
			id = fmt.Sprintf("%s-%d", id, n)
		} else {
			used[id] = 1
		}
		h.SetAttributeString("id", []byte(id))
	}
	for _, n := range links {
		t.link(n.(*ast.Link), folder)
	}
	for _, n := range images {
		t.image(n.(*ast.Image), folder, src)
	}
	for _, q := range quotes {
		callout(q, src)
	}
}

func (t *transformer) note(l markdown.Link) (string, bool) {
	anchor := ""
	if l.Heading != "" && !strings.HasPrefix(l.Heading, "^") {
		parts := strings.Split(l.Heading, "#")
		anchor = "#" + Slug(parts[len(parts)-1])
	}
	if l.Target == "" {
		return anchor, anchor != ""
	}
	if t.o.Resolver == nil {
		return "", false
	}
	href, ok := t.o.Resolver.Note(l)
	return href + anchor, ok
}

func (t *transformer) link(n *ast.Link, folder string) {
	dest := string(n.Destination)
	switch {
	case strings.HasPrefix(dest, linkScheme):
		inner, _ := url.PathUnescape(strings.TrimPrefix(dest, linkScheme))
		if href, ok := t.note(markdown.ParseWiki(inner)); ok {
			n.Destination = []byte(href)
			n.SetAttributeString("class", []byte("internal"))
			return
		}
	case strings.HasPrefix(dest, "#"):
		return
	case strings.HasPrefix(dest, "http://"), strings.HasPrefix(dest, "https://"), strings.HasPrefix(dest, "mailto:"):
		n.SetAttributeString("rel", []byte("noopener noreferrer nofollow"))
		return
	default:
		if l, file, ok := markdown.ParseLocalLink(dest, folder); ok && !file {
			l.Wiki = false
			if href, ok := t.note(l); ok {
				n.Destination = []byte(href)
				n.SetAttributeString("class", []byte("internal"))
				return
			}
		}
	}
	unwrap(n, "unresolved")
}

func (t *transformer) image(n *ast.Image, folder string, src []byte) {
	dest := string(n.Destination)
	alt := string(plain(n, src))
	width := ""
	if i := strings.LastIndexByte(alt, '|'); i >= 0 {
		if w := strings.TrimSpace(alt[i+1:]); isSize(w) {
			alt, width = strings.TrimSpace(alt[:i]), strings.SplitN(w, "x", 2)[0]
		}
	}
	var target string
	var wiki bool
	switch {
	case strings.HasPrefix(dest, embedScheme):
		inner, _ := url.PathUnescape(strings.TrimPrefix(dest, embedScheme))
		l := markdown.ParseWiki(inner)
		if !isImage(l.Target) {
			// An embedded note is shown as a link to it: its text is not
			// part of this page.
			label := l.Alias
			if label == "" {
				label = strings.TrimSuffix(l.Target, ".md")
			}
			if href, ok := t.note(l); ok {
				link := ast.NewLink()
				link.Destination = []byte(href)
				link.SetAttributeString("class", []byte("internal embed"))
				link.AppendChild(link, ast.NewString([]byte(label)))
				n.Parent().ReplaceChild(n.Parent(), n, link)
			} else {
				replaceText(n, label, "unresolved")
			}
			return
		}
		target, wiki = l.Target, true
		if l.Alias != "" {
			if isSize(l.Alias) {
				width = strings.SplitN(l.Alias, "x", 2)[0]
			} else {
				alt = l.Alias
			}
		}
	case strings.HasPrefix(dest, "http://"), strings.HasPrefix(dest, "https://"):
		// Images from the web are not loaded, as in the app: a link.
		link := ast.NewLink()
		link.Destination = n.Destination
		link.SetAttributeString("rel", []byte("noopener noreferrer nofollow"))
		label := alt
		if label == "" {
			label = dest
		}
		link.AppendChild(link, ast.NewString([]byte(label)))
		n.Parent().ReplaceChild(n.Parent(), n, link)
		return
	default:
		l, file, ok := markdown.ParseLocalLink(dest, folder)
		if !ok || !file || !isImage(l.Target) {
			replaceText(n, alt, "unresolved")
			return
		}
		target = l.Target
	}
	if t.o.Resolver == nil {
		replaceText(n, alt, "unresolved")
		return
	}
	url, ok := t.o.Resolver.Image(target, wiki)
	if !ok {
		replaceText(n, alt, "unresolved")
		return
	}
	n.Destination = []byte(url)
	for n.FirstChild() != nil {
		n.RemoveChild(n, n.FirstChild())
	}
	if alt != "" {
		n.AppendChild(n, ast.NewString([]byte(alt)))
	}
	n.SetAttributeString("loading", []byte("lazy"))
	if width != "" {
		n.SetAttributeString("width", []byte(width))
	}
}

func isSize(s string) bool {
	w, _, _ := strings.Cut(s, "x")
	_, err := strconv.Atoi(w)
	return err == nil && w != ""
}

// plain is a node's text, without markup.
func plain(n ast.Node, src []byte) []byte {
	var b bytes.Buffer
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(src))
			if c.SoftLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		case *ast.CodeSpan:
			for g := c.FirstChild(); g != nil; g = g.NextSibling() {
				if t, ok := g.(*ast.Text); ok {
					b.Write(t.Segment.Value(src))
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return b.Bytes()
}

// unwrap replaces a link with its text, in a span with class.
func unwrap(n ast.Node, class string) {
	span := &Span{Class: class}
	for n.FirstChild() != nil {
		c := n.FirstChild()
		n.RemoveChild(n, c)
		span.AppendChild(span, c)
	}
	n.Parent().ReplaceChild(n.Parent(), n, span)
}

func replaceText(n ast.Node, s, class string) {
	span := &Span{Class: class}
	if s != "" {
		span.AppendChild(span, ast.NewString([]byte(s)))
	}
	n.Parent().ReplaceChild(n.Parent(), n, span)
}

// Callouts.

var calloutKinds = map[string]string{
	"note": "note", "info": "note", "todo": "note", "abstract": "note", "summary": "note", "tldr": "note",
	"quote": "note", "cite": "note",
	"tip": "tip", "hint": "tip", "success": "tip", "check": "tip", "done": "tip",
	"important": "important", "question": "important", "help": "important", "faq": "important", "example": "important",
	"warning": "warning", "attention": "warning",
	"caution": "caution", "failure": "caution", "fail": "caution", "missing": "caution", "danger": "caution",
	"error": "caution", "bug": "caution",
}

var calloutRe = regexp.MustCompile(`^\[!([\w-]+)\]([+-]?)[ \t]*(.*)$`)

// callout turns a quote whose first line is "[!type]± title" into a
// Callout, in place.
func callout(q ast.Node, src []byte) {
	p, ok := q.FirstChild().(*ast.Paragraph)
	if !ok || p.Lines().Len() == 0 {
		return
	}
	first := p.Lines().At(0)
	m := calloutRe.FindSubmatch(bytes.TrimRight(first.Value(src), "\r\n"))
	if m == nil {
		return
	}
	typ := strings.ToLower(string(m[1]))
	kind := calloutKinds[typ]
	if kind == "" {
		kind = "note"
	}
	title := strings.TrimSpace(string(m[3]))
	if title == "" {
		title = strings.ToUpper(typ[:1]) + typ[1:]
	}
	c := &Callout{Color: kind, Title: title, Fold: string(m[2])}
	// The first line is the title: drop the inline nodes on it.
	for n := p.FirstChild(); n != nil; {
		next := n.NextSibling()
		start, end, ok := span(n)
		if !ok || start >= first.Stop {
			break
		}
		p.RemoveChild(p, n)
		if end >= first.Stop || isLineEnd(n) {
			break
		}
		n = next
	}
	if p.FirstChild() == nil {
		q.RemoveChild(q, p)
	}
	for q.FirstChild() != nil {
		ch := q.FirstChild()
		q.RemoveChild(q, ch)
		c.AppendChild(c, ch)
	}
	q.Parent().ReplaceChild(q.Parent(), q, c)
}

func isLineEnd(n ast.Node) bool {
	t, ok := n.(*ast.Text)
	return ok && (t.SoftLineBreak() || t.HardLineBreak())
}

// span is where an inline node's text is in the source.
func span(n ast.Node) (start, end int, ok bool) {
	start, end = -1, -1
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, isText := c.(*ast.Text); isText && entering {
			if start < 0 {
				start = t.Segment.Start
			}
			end = t.Segment.Stop
		}
		return ast.WalkContinue, nil
	})
	return start, end, start >= 0
}

// Callout is a "> [!type] title" quote. Color is one of GitHub's five
// (note, tip, important, warning, caution); Fold is "+" or "-" for one
// that folds, open or closed.
type Callout struct {
	ast.BaseBlock
	Color, Title, Fold string
}

// KindCallout is the NodeKind of Callout.
var KindCallout = ast.NewNodeKind("Callout")

func (c *Callout) Kind() ast.NodeKind     { return KindCallout }
func (c *Callout) Dump(src []byte, l int) { ast.DumpHelper(c, src, l, nil, nil) }

// Span is text that was a link or image the reader can't follow.
type Span struct {
	ast.BaseInline
	Class string
}

// KindSpan is the NodeKind of Span.
var KindSpan = ast.NewNodeKind("Span")

func (s *Span) Kind() ast.NodeKind     { return KindSpan }
func (s *Span) Dump(src []byte, l int) { ast.DumpHelper(s, src, l, nil, nil) }

var _ renderer.NodeRenderer = (*nodeRenderer)(nil)
