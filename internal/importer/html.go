package importer

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTMLToMarkdown turns a note's HTML, the kind Apple Notes keeps, into
// Markdown: each <div> is a line, as in Notes; headings, lists (with
// checklists), tables, quotes, code, links and emphasis carry over.
// image returns the Markdown that shows an <img> by its src, "" to
// leave it out.
func HTMLToMarkdown(src string, image func(src string) string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src
	}
	c := &converter{image: image}
	body := find(doc, atom.Body)
	if body == nil {
		body = doc
	}
	c.blocks(body, "")
	out := strings.Join(c.lines, "\n")
	out = blankRuns.ReplaceAllString(out, "\n\n")
	return strings.Trim(out, "\n") + "\n"
}

var (
	blankRuns = regexp.MustCompile(`\n(?:[ \t]*\n){2,}`)
	spaces    = regexp.MustCompile(`[ \t\r\n\f]+`)
	embeds    = regexp.MustCompile(`!\[\[[^\]]*\]\]|!\[[^\]]*\]\([^)]*\)`)
)

type converter struct {
	lines []string
	image func(string) string
}

func find(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := find(c, a); f != nil {
			return f
		}
	}
	return nil
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func isBlock(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.Div, atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol,
		atom.Table, atom.Blockquote, atom.Pre, atom.Hr, atom.Object, atom.Section, atom.Article,
		atom.Header, atom.Footer, atom.Main, atom.Body:
		return true
	}
	return false
}

func hasBlock(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if isBlock(c) {
			return true
		}
	}
	return false
}

// line adds lines of text, each after prefix (a quote's "> ").
func (c *converter) line(prefix, text string) {
	for _, l := range strings.Split(text, "\n") {
		c.lines = append(c.lines, strings.TrimRight(prefix+l, " "))
	}
}

// blank ends the block before: a list or a table would take in the
// line after it.
func (c *converter) blank(prefix string) {
	c.lines = append(c.lines, strings.TrimRight(prefix, " "))
}

func (c *converter) blocks(n *html.Node, prefix string) {
	var run strings.Builder
	flush := func() {
		if s := strings.TrimSpace(run.String()); s != "" {
			c.line(prefix, s)
		}
		run.Reset()
	}
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		if !isBlock(k) {
			if k.Type == html.ElementNode && k.DataAtom == atom.Br && strings.TrimSpace(run.String()) == "" {
				c.blank(prefix)
				continue
			}
			run.WriteString(c.inline(k))
			continue
		}
		flush()
		c.block(k, prefix)
	}
	flush()
}

func (c *converter) block(n *html.Node, prefix string) {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		if t := strings.TrimSpace(plain(c.inline(n))); t != "" {
			c.line(prefix, strings.Repeat("#", level)+" "+strings.ReplaceAll(t, "\n", " "))
		}
	case atom.Ul, atom.Ol:
		c.list(n, prefix, 0)
		c.blank(prefix)
	case atom.Table:
		c.table(n, prefix)
		c.blank(prefix)
	case atom.Blockquote:
		c.blocks(n, prefix+"> ")
		c.blank(prefix)
	case atom.Pre:
		c.line(prefix, "```\n"+strings.TrimRight(text(n), "\n")+"\n```")
	case atom.Hr:
		c.blank(prefix)
		c.line(prefix, "---")
		c.blank(prefix)
	default:
		if hasBlock(n) {
			c.blocks(n, prefix)
			return
		}
		s := strings.TrimSpace(c.inline(n))
		if s == "" {
			c.blank(prefix) // <div><br></div>: Notes' empty line
			return
		}
		c.line(prefix, s)
	}
}

func (c *converter) list(n *html.Node, prefix string, depth int) {
	i := 1
	if s, err := strconv.Atoi(attr(n, "start")); err == nil {
		i = s
	}
	checklist := strings.Contains(attr(n, "class"), "checklist")
	indent := strings.Repeat("    ", depth)
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		if k.Type != html.ElementNode {
			continue
		}
		switch k.DataAtom {
		case atom.Ul, atom.Ol:
			c.list(k, prefix, depth+1)
		case atom.Li:
			marker := "- "
			if n.DataAtom == atom.Ol {
				marker = strconv.Itoa(i) + ". "
			}
			if cls := attr(k, "class"); checklist || strings.Contains(cls, "checked") {
				if strings.Contains(" "+cls+" ", " checked ") {
					marker += "[x] "
				} else {
					marker += "[ ] "
				}
			}
			var own strings.Builder
			var nested []*html.Node
			for x := k.FirstChild; x != nil; x = x.NextSibling {
				if x.Type == html.ElementNode && (x.DataAtom == atom.Ul || x.DataAtom == atom.Ol) {
					nested = append(nested, x)
					continue
				}
				own.WriteString(c.inline(x))
			}
			t := strings.TrimSpace(own.String())
			t = strings.ReplaceAll(t, "\n", "\n"+indent+strings.Repeat(" ", len(marker)))
			c.line(prefix, indent+marker+t)
			for _, x := range nested {
				c.list(x, prefix, depth+1)
			}
			i++
		}
	}
}

func (c *converter) table(n *html.Node, prefix string) {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.ElementNode && x.DataAtom == atom.Tr {
			var row []string
			for td := x.FirstChild; td != nil; td = td.NextSibling {
				if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
					s := strings.TrimSpace(c.inline(td))
					s = strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", "<br>")
					row = append(row, s)
				}
			}
			rows = append(rows, row)
			return
		}
		for k := x.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	if width == 0 {
		return
	}
	c.blank(prefix)
	for i, r := range rows {
		for len(r) < width {
			r = append(r, "")
		}
		c.line(prefix, "| "+strings.Join(r, " | ")+" |")
		if i == 0 {
			c.line(prefix, "|"+strings.Repeat(" --- |", width))
		}
	}
}

// inline renders a node as Markdown text; <br> and nested blocks become
// line breaks.
func (c *converter) inline(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return spaces.ReplaceAllString(strings.ReplaceAll(n.Data, "\u00a0", " "), " ")
	case html.ElementNode:
	default:
		return ""
	}
	inner := func() string {
		var b strings.Builder
		for k := n.FirstChild; k != nil; k = k.NextSibling {
			b.WriteString(c.inline(k))
		}
		return b.String()
	}
	style := strings.ToLower(strings.ReplaceAll(attr(n, "style"), " ", ""))
	switch n.DataAtom {
	case atom.Br:
		return "\n"
	case atom.Img:
		if c.image == nil {
			return ""
		}
		return c.image(attr(n, "src"))
	case atom.B, atom.Strong:
		return wrap("**", inner())
	case atom.I, atom.Em:
		return wrap("*", inner())
	case atom.S, atom.Strike, atom.Del:
		return wrap("~~", inner())
	case atom.Tt, atom.Code, atom.Kbd, atom.Samp:
		t := spaces.ReplaceAllString(text(n), " ")
		if strings.TrimSpace(t) == "" {
			return t
		}
		return wrap("`", t)
	case atom.A:
		href := attr(n, "href")
		t := strings.TrimSpace(inner())
		switch {
		case href == "" || strings.HasPrefix(href, "#"):
			return inner()
		case t == "" || t == href:
			return "<" + href + ">"
		}
		return "[" + t + "](" + strings.ReplaceAll(strings.ReplaceAll(href, " ", "%20"), ")", "%29") + ")"
	case atom.Script, atom.Style, atom.Head, atom.Title:
		return ""
	case atom.Div, atom.P, atom.Li, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return strings.TrimRight(inner(), "\n") + "\n"
	case atom.Span:
		s := inner()
		if strings.Contains(style, "font-weight:bold") || strings.Contains(style, "font-weight:700") {
			s = wrap("**", s)
		}
		if strings.Contains(style, "font-style:italic") {
			s = wrap("*", s)
		}
		return s
	}
	return inner()
}

// wrap puts marks around text, outside its leading and trailing spaces,
// line by line.
func wrap(mark, s string) string {
	if strings.TrimSpace(embeds.ReplaceAllString(s, "")) == "" {
		return s // marks around an image only would show as text
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		start := strings.Index(l, t)
		lines[i] = l[:start] + mark + t + mark + l[start+len(t):]
	}
	return strings.Join(lines, "\n")
}

// plain removes emphasis marks, for headings.
func plain(s string) string {
	return strings.NewReplacer("**", "", "~~", "").Replace(s)
}

// text is the text in a node, as it is.
func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type == html.ElementNode && n.DataAtom == atom.Br {
		return "\n"
	}
	var b strings.Builder
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		b.WriteString(text(k))
	}
	if n.Type == html.ElementNode && (n.DataAtom == atom.Div || n.DataAtom == atom.P) {
		b.WriteString("\n")
	}
	return b.String()
}
