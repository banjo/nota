package publish

import (
	_ "embed"
	"fmt"
	"html/template"
	"image/color"
	"io"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/banjo/nota/internal/plugins"
	"github.com/banjo/nota/internal/protocol"
)

// tokens.css is a copy of packages/ui/src/tokens.css (TestTokensInSync).
//
//go:generate cp ../../packages/ui/src/tokens.css tokens.css
//go:embed tokens.css
var tokensCSS string

//go:embed page.css
var pageCSS string

// Lucide icons of the callouts, as in packages/editor/src/theme.ts.
var calloutIcons = map[string]string{
	"note":      "<circle cx='12' cy='12' r='10'/><path d='M12 16v-4'/><path d='M12 8h.01'/>",
	"tip":       "<path d='M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5'/><path d='M9 18h6'/><path d='M10 22h4'/>",
	"important": "<path d='M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z'/><path d='M12 7v2'/><path d='M12 13h.01'/>",
	"warning":   "<path d='m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3'/><path d='M12 9v4'/><path d='M12 17h.01'/>",
	"caution":   "<path d='M12 16h.01'/><path d='M12 8v4'/><path d='M15.312 2a2 2 0 0 1 1.414.586l4.688 4.688A2 2 0 0 1 22 8.688v6.624a2 2 0 0 1-.586 1.414l-4.688 4.688a2 2 0 0 1-1.414.586H8.688a2 2 0 0 1-1.414-.586l-4.688-4.688A2 2 0 0 1 2 15.312V8.688a2 2 0 0 1 .586-1.414l4.688-4.688A2 2 0 0 1 8.688 2z'/>",
}

// css is the stylesheet of every page: Nota's tokens, then the page.
var css = tokensCSS + pageCSS

// calloutIcon is a callout's icon as inline SVG in the callout's colour:
// printing (WebKit's PDFs too) leaves out the CSS masks the app uses.
func calloutIcon(kind string) string {
	paths, ok := calloutIcons[kind]
	if !ok {
		paths = calloutIcons["note"]
	}
	return `<svg class="callout-icon" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` +
		strings.ReplaceAll(paths, "'", `"`) + `</svg>`
}

// Theme is the look of a shared note.
type Theme = protocol.ShareTheme

// CheckTheme refuses a mode or token Nota doesn't know, and values that
// are not plain colours (plugins.IsColor).
func CheckTheme(t Theme) error {
	if t.Mode != "" && t.Mode != "light" && t.Mode != "dark" {
		return fmt.Errorf("mode %q is not light or dark", t.Mode)
	}
	for k, v := range t.Tokens {
		if !slices.Contains(plugins.Tokens, k) {
			return fmt.Errorf("%q is not a theme token", k)
		}
		if !plugins.IsColor(v) {
			return fmt.Errorf("%s: %q is not a colour", k, v)
		}
	}
	return nil
}

// style is a theme's tokens as CSS (CheckTheme has passed).
func style(t *Theme) template.CSS {
	if t == nil || len(t.Tokens) == 0 {
		return ""
	}
	keys := make([]string, 0, len(t.Tokens))
	for k := range t.Tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(":root{")
	for _, k := range keys {
		fmt.Fprintf(&b, "--%s:%s;", k, t.Tokens[k])
	}
	b.WriteString("}")
	return template.CSS(b.String()) //nolint:gosec // checked colours only
}

func mode(t *Theme) string {
	if t == nil {
		return ""
	}
	return t.Mode
}

// Page is a shared note.
type Page struct {
	Title    string
	Body     template.HTML
	Modified string // "12 March 2026", or ""
	Theme    *Theme
	// Home is Nota's site, linked from the footer ("" for none).
	Home string
	// Document leaves out the header and the footer: an exported note,
	// a file of its own rather than a page of the server.
	Document bool
	// Meta is what a chat or a social site shows of the link.
	Meta *Meta
}

// Meta is a page's description and Open Graph tags: the preview of its
// link. URL and Image are absolute.
type Meta struct {
	Title, Description string
	URL, Image         string
}

// Lock is the page that asks for a shared note's password.
type Lock struct {
	Theme *Theme
	// Error is shown under the field: a wrong password, too many tries.
	Error string
	Home  string
	// Meta is the preview of a protected note's link: it doesn't tell
	// what the note is.
	Meta *Meta
}

// Missing is the page of a link that leads nowhere (any more).
type Missing struct {
	Home string
}

const lockup = `<svg xmlns="http://www.w3.org/2000/svg" width="{{.}}" viewBox="0 0 216 64" role="img" aria-label="Nota"><g transform="scale(2)" style="color: var(--ink, #1e7363)"><path d="M7.5 25.5V8.5L20.5 24" fill="none" stroke="currentColor" stroke-width="5.2" stroke-linecap="round" stroke-linejoin="round"/><path d="M23.04 24.58L27.58 4.62A1 1 0 0 0 25.67 4.03L18.09 23.04A2.6 2.6 0 1 0 23.04 24.58Z" fill="currentColor"/></g><path transform="translate(78 7) scale(.8)" d="M4 20V60M4 41a17 17 0 0 1 34 0V60M53 41a17 17 0 1 0 34 0a17 17 0 1 0-34 0M107 8V48a12 12 0 0 0 12 12h3M98 24H121M132 41a17 17 0 1 0 34 0a17 17 0 1 0-34 0M166 20V60" fill="none" stroke="currentColor" stroke-width="8"/></svg>`

const favicon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="7.2" fill="#1e7363"/><g transform="translate(1.595 2.24) scale(0.86)" fill="#fff"><path d="M7.5 25.5V8.5L20.5 24" fill="none" stroke="#fff" stroke-width="5.2" stroke-linecap="round" stroke-linejoin="round"/><path d="M23.04 24.58L27.58 4.62A1 1 0 0 0 25.67 4.03L18.09 23.04A2.6 2.6 0 1 0 23.04 24.58Z"/></g></svg>`

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"css":        func() template.CSS { return template.CSS(css) },                                             //nolint:gosec // our own
	"favicon":    func() template.URL { return template.URL("data:image/svg+xml," + url.PathEscape(favicon)) }, //nolint:gosec // our own
	"cardWidth":  func() int { return CardWidth },
	"cardHeight": func() int { return CardHeight },
}).Parse(`
{{define "lockup"}}` + lockup + `{{end}}
{{define "head"}}<!doctype html>
<html lang="en"{{with .Mode}} data-mode="{{.}}"{{end}}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="robots" content="noindex, nofollow">
<meta name="referrer" content="no-referrer">
<link rel="icon" href="{{favicon}}">
{{with .Meta}}<meta name="description" content="{{.Description}}">
<meta property="og:type" content="article">
<meta property="og:site_name" content="Nota">
<meta property="og:title" content="{{.Title}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:url" content="{{.URL}}">
<meta property="og:image" content="{{.Image}}">
<meta property="og:image:type" content="image/png">
<meta property="og:image:width" content="{{cardWidth}}">
<meta property="og:image:height" content="{{cardHeight}}">
<meta property="og:image:alt" content="{{.Title}}">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="{{.Title}}">
<meta name="twitter:description" content="{{.Description}}">
<meta name="twitter:image" content="{{.Image}}">
{{end}}{{with .ThemeColor}}<meta name="theme-color" content="{{.}}">
{{end}}<style>{{css}}</style>
{{with .Style}}<style>{{.}}</style>{{end}}
{{end}}

{{define "foot"}}{{if .}}<a href="{{.}}">Made with Nota</a>{{else}}Made with Nota{{end}}{{end}}

{{define "page"}}{{template "head" .}}<title>{{.Page.Title}}</title>
</head>
<body{{if .Page.Document}} class="document"{{end}}>
{{if not .Page.Document}}<header class="top">
  <a class="brand"{{with .Page.Home}} href="{{.}}"{{end}} aria-label="Nota">{{template "lockup" 54}}</a>
</header>{{end}}
<article>
  <h1 class="title">{{.Page.Title}}</h1>
  {{with .Page.Modified}}<p class="meta">Updated {{.}}</p>{{end}}
  <div class="note">{{.Page.Body}}</div>
</article>
{{if not .Page.Document}}<footer>{{template "foot" .Page.Home}}</footer>{{end}}
</body>
</html>
{{end}}

{{define "lock"}}{{template "head" .}}<title>Protected note · Nota</title>
</head>
<body>
<main class="gate">
  <div>
    <div class="card">
      <span class="brand">{{template "lockup" 76}}</span>
      <div class="lock" aria-hidden="true"><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="11" x="3" y="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg></div>
      <h1>This note is protected</h1>
      <p>Enter the password you were given to read it.</p>
      <form method="post" autocomplete="off">
        <label for="password">Password</label>
        <input id="password" name="password" type="password" placeholder="Password" required autofocus{{if .Lock.Error}} aria-invalid="true" aria-describedby="error"{{end}}>
        {{with .Lock.Error}}<p class="error" id="error" role="alert">{{.}}</p>{{end}}
        <button type="submit">Open note</button>
      </form>
    </div>
    <p class="gate-foot">{{template "foot" .Lock.Home}}</p>
  </div>
</main>
</body>
</html>
{{end}}

{{define "missing"}}{{template "head" .}}<title>Not found · Nota</title>
</head>
<body>
<main class="gate">
  <div>
    <div class="card">
      <span class="brand">{{template "lockup" 76}}</span>
      <h1>This link doesn't lead anywhere</h1>
      <p>The note may have been unshared, deleted, or the link is mistyped.</p>
    </div>
    <p class="gate-foot">{{template "foot" .Missing.Home}}</p>
  </div>
</main>
</body>
</html>
{{end}}
`))

type data struct {
	Mode       string
	Style      template.CSS
	Meta       *Meta
	ThemeColor string
	Page       Page
	Lock       Lock
	Missing    Missing
}

// themeColor is the theme's background for browsers' chrome, on pages
// with a theme of their own ("" follows the reader's system).
func themeColor(t *Theme, m *Meta) string {
	if m == nil || t == nil || (t.Mode == "" && t.Tokens["background"] == "") {
		return ""
	}
	c := color.NRGBAModel.Convert(cardColor(t, "background")).(color.NRGBA)
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// WritePage writes a shared note's page.
func WritePage(w io.Writer, p Page) error {
	return tmpl.ExecuteTemplate(w, "page", data{Mode: mode(p.Theme), Style: style(p.Theme), Meta: p.Meta,
		ThemeColor: themeColor(p.Theme, p.Meta), Page: p})
}

// WriteLock writes the password page.
func WriteLock(w io.Writer, l Lock) error {
	return tmpl.ExecuteTemplate(w, "lock", data{Mode: mode(l.Theme), Style: style(l.Theme), Meta: l.Meta,
		ThemeColor: themeColor(l.Theme, l.Meta), Lock: l})
}

// WriteMissing writes the page of a link that leads nowhere.
func WriteMissing(w io.Writer, m Missing) error {
	return tmpl.ExecuteTemplate(w, "missing", data{Missing: m})
}
