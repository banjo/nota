package publish

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/banjo/nota/internal/markdown"
)

// notaSyntax adds ==highlights== and #tags to goldmark, and renders
// callouts, unfollowable links and the few HTML tags Nota shows.
type notaSyntax struct{}

func (notaSyntax) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(highlightParser{}, 500),
		util.Prioritized(tagParser{}, 500),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(&nodeRenderer{}, 100)))
}

// Highlight is ==text==.
type Highlight struct{ ast.BaseInline }

// KindHighlight is the NodeKind of Highlight.
var KindHighlight = ast.NewNodeKind("Highlight")

func (h *Highlight) Kind() ast.NodeKind     { return KindHighlight }
func (h *Highlight) Dump(src []byte, l int) { ast.DumpHelper(h, src, l, nil, nil) }

type highlightDelims struct{}

func (highlightDelims) IsDelimiter(b byte) bool { return b == '=' }
func (highlightDelims) CanOpenCloser(o, c *parser.Delimiter) bool {
	return o.Char == c.Char && o.Length >= 2 && c.Length >= 2
}
func (highlightDelims) OnMatch(int) ast.Node { return &Highlight{} }

type highlightParser struct{}

func (highlightParser) Trigger() []byte { return []byte{'='} }

func (highlightParser) Parse(_ ast.Node, block text.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, seg := block.PeekLine()
	d := parser.ScanDelimiter(line, before, 2, highlightDelims{})
	if d == nil || d.OriginalLength != 2 || before == '=' {
		return nil
	}
	d.Segment = seg.WithStop(seg.Start + d.OriginalLength)
	block.Advance(d.OriginalLength)
	pc.PushDelimiter(d)
	return d
}

// Tag is a #tag.
type Tag struct {
	ast.BaseInline
	Name string
}

// KindTag is the NodeKind of Tag.
var KindTag = ast.NewNodeKind("Tag")

func (t *Tag) Kind() ast.NodeKind     { return KindTag }
func (t *Tag) Dump(src []byte, l int) { ast.DumpHelper(t, src, l, nil, nil) }

var tagRe = regexp.MustCompile(`^#([\p{L}\p{N}_/\-]+)`)

type tagParser struct{}

func (tagParser) Trigger() []byte { return []byte{'#'} }

func (tagParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	if before != '\n' && before != utf8.RuneError && !unicode.IsSpace(before) && !strings.ContainsRune("([,;", before) {
		return nil
	}
	line, _ := block.PeekLine()
	m := tagRe.FindSubmatch(line)
	if m == nil {
		return nil
	}
	name := markdown.CleanTag(string(m[1]))
	if name == "" {
		return nil
	}
	block.Advance(len(m[0]))
	return &Tag{Name: strings.TrimRight(string(m[1]), "/")}
}

type nodeRenderer struct{}

func (r *nodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindHighlight, func(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString("<mark>")
		} else {
			_, _ = w.WriteString("</mark>")
		}
		return ast.WalkContinue, nil
	})
	reg.Register(KindTag, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString(`<span class="tag">#` + html.EscapeString(n.(*Tag).Name) + `</span>`)
		}
		return ast.WalkSkipChildren, nil
	})
	reg.Register(KindSpan, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString(`<span class="` + n.(*Span).Class + `">`)
		} else {
			_, _ = w.WriteString("</span>")
		}
		return ast.WalkContinue, nil
	})
	reg.Register(KindCallout, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		c := n.(*Callout)
		title := `<span class="callout-icon" aria-hidden="true"></span>` + html.EscapeString(c.Title)
		if !entering {
			if c.Fold != "" {
				_, _ = w.WriteString("</div></details>\n")
			} else {
				_, _ = w.WriteString("</div></div>\n")
			}
			return ast.WalkContinue, nil
		}
		cls := `class="callout callout-` + c.Color + `"`
		switch c.Fold {
		case "":
			_, _ = w.WriteString(`<div ` + cls + `><p class="callout-title">` + title + `</p><div class="callout-body">`)
		default:
			open := ""
			if c.Fold == "+" {
				open = " open"
			}
			_, _ = w.WriteString(`<details ` + cls + open + `><summary class="callout-title">` + title + `</summary><div class="callout-body">`)
		}
		return ast.WalkContinue, nil
	})
	reg.Register(ast.KindRawHTML, renderRawHTML)
}

// The HTML tags a note may use (docs/markdown.md), without attributes.
var allowedTag = regexp.MustCompile(`(?i)^<(/?)(kbd|sup|sub|mark|u|s|b|i|small|br)\s*(/?)>$`)

func renderRawHTML(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	raw := n.(*ast.RawHTML)
	var b strings.Builder
	for i := 0; i < raw.Segments.Len(); i++ {
		seg := raw.Segments.At(i)
		b.Write(seg.Value(src))
	}
	if m := allowedTag.FindStringSubmatch(b.String()); m != nil {
		tag := strings.ToLower(m[2])
		if tag == "br" {
			_, _ = w.WriteString("<br />")
		} else {
			_, _ = w.WriteString("<" + m[1] + tag + ">")
		}
	} else {
		_, _ = w.WriteString(html.EscapeString(b.String()))
	}
	return ast.WalkSkipChildren, nil
}
