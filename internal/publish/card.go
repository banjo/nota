package publish

import (
	"bytes"
	_ "embed"
	"html/template"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/net/html"
)

// The social card of a shared note: the picture a chat or a social site
// shows with its link (og:image), drawn on the server in the note's
// theme. Inter (card/Inter-LICENSE.txt), subset to Latin, Greek and
// Cyrillic; the lockup is assets/brand/lockup.svg split into the mark
// and the wordmark and rendered at 44 px (card/*.svg, render.sh), as
// alpha masks so they take the theme's colours.

// CardWidth and CardHeight are the card's size, the one Open Graph and
// X recommend for a large preview.
const (
	CardWidth  = 1200
	CardHeight = 630
)

var (
	//go:embed card/InterDisplay-Bold.ttf
	boldTTF []byte
	//go:embed card/Inter-Regular.ttf
	regularTTF []byte
	//go:embed card/Inter-Medium.ttf
	mediumTTF []byte
	//go:embed card/lockup-mark.png
	markPNG []byte
	//go:embed card/lockup-word.png
	wordPNG []byte

	fontBold    = mustFont(boldTTF)
	fontRegular = mustFont(regularTTF)
	fontMedium  = mustFont(mediumTTF)
	markMask    = mustMask(markPNG)
	wordMask    = mustMask(wordPNG)
)

func mustFont(b []byte) *opentype.Font {
	f, err := opentype.Parse(b)
	if err != nil {
		panic(err)
	}
	return f
}

func mustMask(b []byte) image.Image {
	m, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		panic(err)
	}
	return m
}

// Card is what a social card shows.
type Card struct {
	Title   string
	Excerpt string
	// Footer is the line at the bottom left ("Updated 12 March 2026"),
	// Site the one at the bottom right (the server's host).
	Footer, Site string
	Theme        *Theme
}

// The colours a card takes from a theme, and Nota's own of each mode
// (packages/ui/src/tokens.css).
var cardTokens = map[string][2]string{
	"background":       {"#ffffff", "#1c1c1e"},
	"foreground":       {"#1d1f24", "#ecebe8"},
	"muted-foreground": {"#6b6f78", "#9a9da5"},
	"hairline":         {"#e7e7e4", "#313134"},
	"ink":              {"#1e7363", "#5fd9c4"},
}

func cardColor(t *Theme, token string) color.Color {
	defaults := cardTokens[token]
	def := defaults[0]
	if t != nil && t.Mode == "dark" {
		def = defaults[1]
	}
	if t != nil {
		if c, ok := parseColor(t.Tokens[token]); ok {
			return c
		}
	}
	c, _ := parseColor(def)
	return c
}

var rgbFn = regexp.MustCompile(`^rgba?\(\s*([0-9.]+)[\s,]+([0-9.]+)[\s,]+([0-9.]+)(?:[\s,/]+([0-9.]+%?))?\s*\)$`)

// parseColor reads a hex or rgb() colour; others (hsl()) are not taken,
// and the card falls back to Nota's colour.
func parseColor(s string) (color.NRGBA, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, c := range h {
				b.WriteRune(c)
				b.WriteRune(c)
			}
			h = b.String()
		}
		if len(h) == 6 {
			h += "ff"
		}
		if len(h) != 8 {
			return color.NRGBA{}, false
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return color.NRGBA{}, false
		}
		return color.NRGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
	}
	m := rgbFn.FindStringSubmatch(s)
	if m == nil {
		return color.NRGBA{}, false
	}
	var c [4]float64
	for i := range 3 {
		c[i], _ = strconv.ParseFloat(m[i+1], 64)
	}
	c[3] = 255
	if a := m[4]; a != "" {
		if p, ok := strings.CutSuffix(a, "%"); ok {
			v, _ := strconv.ParseFloat(p, 64)
			c[3] = v / 100 * 255
		} else {
			v, _ := strconv.ParseFloat(a, 64)
			c[3] = v * 255
		}
	}
	u := func(f float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(255, f)))) }
	return color.NRGBA{u(c[0]), u(c[1]), u(c[2]), u(c[3])}, true
}

// face is a font at a size, with the letter spacing it is set in.
type face struct {
	font.Face
	f     *sfnt.Font
	buf   sfnt.Buffer
	track fixed.Int26_6
}

func newFace(f *opentype.Font, size, track float64) *face {
	ff, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	}
	return &face{Face: ff, f: f, track: fixed.Int26_6(track * size * 64)}
}

// has says whether the font draws r: Inter has no emoji or CJK, and
// a box for each would look broken, so those are left out.
func (fc *face) has(r rune) bool {
	i, err := fc.f.GlyphIndex(&fc.buf, r)
	return err == nil && i != 0
}

// clean leaves out what the font doesn't draw and collapses spaces.
func (fc *face) clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if !fc.has(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func (fc *face) width(s string) fixed.Int26_6 {
	var w fixed.Int26_6
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			w += fc.Kern(prev, r) + fc.track
		}
		a, _ := fc.GlyphAdvance(r)
		w += a
		prev = r
	}
	return w
}

func (fc *face) draw(dst draw.Image, s string, x, baseline int, c color.Color) {
	src := image.NewUniform(c)
	dot := fixed.P(x, baseline)
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			dot.X += fc.Kern(prev, r) + fc.track
		}
		dr, mask, mp, adv, ok := fc.Glyph(dot, r)
		if ok {
			draw.DrawMask(dst, dr, src, image.Point{}, mask, mp, draw.Over)
		}
		dot.X += adv
		prev = r
	}
}

// wrap breaks s into lines of at most width, at most max of them; text
// that doesn't fit ends the last line with "…". A word wider than a line
// is broken where it must be.
func (fc *face) wrap(s string, width, max int) (lines []string, cut bool) {
	w := fixed.I(width)
	var line string
	words := strings.Fields(s)
	for len(words) > 0 {
		word := words[0]
		next := word
		if line != "" {
			next = line + " " + word
		}
		if fc.width(next) <= w {
			line, words = next, words[1:]
			continue
		}
		if line == "" {
			// One word wider than the line: take what fits of it.
			_, n := utf8.DecodeRuneInString(word)
			for j := range word {
				if j > n && fc.width(word[:j]) <= w {
					n = j
				}
			}
			line, words[0] = word[:n], word[n:]
		}
		lines = append(lines, line)
		line = ""
		if len(lines) == max {
			return fc.ellipsis(lines, w), true
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines, false
}

func (fc *face) ellipsis(lines []string, w fixed.Int26_6) []string {
	last := strings.TrimRight(lines[len(lines)-1], " ")
	for last != "" && fc.width(last+"…") > w {
		_, n := utf8.DecodeLastRuneInString(last)
		last = strings.TrimRight(last[:len(last)-n], " ")
	}
	last = strings.TrimRight(last, " ,.;:–—-")
	lines[len(lines)-1] = last + "…"
	return lines
}

// WriteCard draws a card as PNG.
func WriteCard(w io.Writer, c Card) error {
	const pad = 80
	bg := cardColor(c.Theme, "background")
	fg := cardColor(c.Theme, "foreground")
	muted := cardColor(c.Theme, "muted-foreground")
	ink := cardColor(c.Theme, "ink")
	hairline := cardColor(c.Theme, "hairline")

	img := image.NewRGBA(image.Rect(0, 0, CardWidth, CardHeight))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	// An ink edge on the left, as a mark in the margin.
	draw.Draw(img, image.Rect(0, 0, 14, CardHeight), image.NewUniform(ink), image.Point{}, draw.Src)

	// The lockup.
	at := image.Pt(pad, 62)
	draw.DrawMask(img, markMask.Bounds().Add(at), image.NewUniform(ink), image.Point{}, markMask, image.Point{}, draw.Over)
	draw.DrawMask(img, wordMask.Bounds().Add(at), image.NewUniform(fg), image.Point{}, wordMask, image.Point{}, draw.Over)

	width := CardWidth - 2*pad
	footerBase := CardHeight - 66
	footerTop := footerBase - 26 - 34

	// The title: as large as fits in three lines.
	var title *face
	var lines []string
	for i, size := range []float64{84, 70, 58} {
		title = newFace(fontBold, size, -0.025)
		text := title.clean(c.Title)
		if text == "" {
			text = "A shared note"
		}
		var cut bool
		lines, cut = title.wrap(text, width, 3)
		if !cut || i == 2 {
			break
		}
	}
	m := title.Metrics()
	lh := int(math.Round(float64(m.Height.Round()) * 1.06))
	y := 172 + m.Ascent.Round()
	for _, l := range lines {
		title.draw(img, l, pad, y, fg)
		y += lh
	}
	y -= lh

	// The excerpt, in the lines left above the footer.
	if c.Excerpt != "" {
		body := newFace(fontRegular, 30, -0.005)
		bm := body.Metrics()
		blh := 44
		first := y + m.Descent.Round() + 30 + bm.Ascent.Round()
		if n := (footerTop-24-bm.Descent.Round()-first)/blh + 1; n > 0 {
			ex, _ := body.wrap(body.clean(c.Excerpt), width, min(n, 3))
			for i, l := range ex {
				body.draw(img, l, pad, first+i*blh, muted)
			}
		}
	}

	// The footer.
	draw.Draw(img, image.Rect(pad, footerTop, CardWidth-pad, footerTop+1), image.NewUniform(hairline), image.Point{}, draw.Src)
	small := newFace(fontMedium, 24, 0)
	right := CardWidth - pad
	if site := small.clean(c.Site); site != "" {
		sw := small.width(site).Ceil()
		right -= sw
		small.draw(img, site, right, footerBase, ink)
		right -= 40
	}
	if foot := small.clean(c.Footer); foot != "" {
		lines, _ := small.wrap(foot, right-pad, 1)
		if len(lines) > 0 {
			small.draw(img, lines[0], pad, footerBase, muted)
		}
	}

	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(w, img)
}

// Excerpt is the start of a rendered note as plain text, for a page's
// description: the words of its paragraphs, lists and quotes, without
// code blocks or footnotes, cut at a word before max runes.
func Excerpt(body template.HTML, max int) string {
	z := html.NewTokenizer(strings.NewReader(string(body)))
	var b strings.Builder
	skip := 0
	for b.Len() < max*4 {
		switch z.Next() {
		case html.ErrorToken:
			return cut(b.String(), max)
		case html.StartTagToken:
			name, hasAttr := z.TagName()
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				if string(k) == "class" && string(v) == "footnotes" {
					// The footnotes come last.
					return cut(b.String(), max)
				}
			}
			switch string(name) {
			case "pre", "svg", "sup", "section", "style", "script":
				skip++
			case "p", "li", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "td", "th", "br", "div":
				b.WriteByte(' ')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "pre", "svg", "sup", "section", "style", "script":
				if skip > 0 {
					skip--
				}
			case "p", "li", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "td", "th", "div":
				b.WriteByte(' ')
			}
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
	return cut(b.String(), max)
}

func cut(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)[:max]
	out := string(r)
	if i := strings.LastIndexByte(out, ' '); i > max/2 {
		out = out[:i]
	}
	return strings.TrimRight(out, " ,.;:–—-") + "…"
}
