// Package icons is the icons Nota draws for folders and saved views: the
// Lucide, Tabler and Simple Icons sets, embedded from Iconify's packages
// (go generate writes data/), and the system's emoji.
//
// An icon is named "<set>:<name>", as in Iconify ("lucide:briefcase",
// "simple-icons:github"), or is an emoji ("🏠"). A bare name is a Lucide
// icon, so the short names saved views have used keep working
// ("pen", "check").
package icons

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
)

//go:generate go run ./gen data

//go:embed data/*.gz
var data embed.FS

// Set is one source of icons.
type Set struct {
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
}

// EmojiSet is the prefix search results of an emoji have; an emoji's id
// is the emoji itself.
const EmojiSet = "emoji"

// Sets are the icon sets, in the order search ranks them.
var Sets = []Set{
	{"lucide", "Lucide"},
	{"tabler", "Tabler"},
	{EmojiSet, "Emoji"},
	{"simple-icons", "Simple Icons"},
}

// Icon is one icon, as search returns it.
type Icon struct {
	// ID is what a folder or view stores: "lucide:house" or "🏠".
	ID   string `json:"id"`
	Set  string `json:"set"`
	Name string `json:"name"`
	// SVG is the icon to draw, in currentColor; empty for an emoji.
	SVG string `json:"svg,omitempty"`
}

// legacy are the names saved views had before any Lucide icon would do.
var legacy = map[string]string{"pen": "pen-line", "check": "circle-check"}

type iconData struct {
	Body   string `json:"body"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

type alias struct {
	Parent string `json:"parent"`
}

type iconSet struct {
	Prefix  string              `json:"prefix"`
	Width   int                 `json:"width,omitempty"`
	Height  int                 `json:"height,omitempty"`
	Icons   map[string]iconData `json:"icons"`
	Aliases map[string]alias    `json:"aliases,omitempty"`
	// names are the icons search shows, sorted; hidden ones and aliases
	// are left out.
	names []string
}

type emoji struct{ char, name, group string }

var (
	setsOnce sync.Map // prefix → func() (*iconSet, error)
	emojiAll = sync.OnceValues(loadEmoji)
	// emojiSet holds every emoji without its variation selectors.
	emojiSet = sync.OnceValue(func() map[string]bool {
		es, _ := emojiAll()
		m := make(map[string]bool, len(es))
		for _, e := range es {
			m[strings.ReplaceAll(e.char, "\ufe0f", "")] = true
		}
		return m
	})
)

func readGz(name string) ([]byte, error) {
	f, err := data.Open("data/" + name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}

// load reads a set the first time it is needed.
func load(prefix string) (*iconSet, error) {
	if !slices.ContainsFunc(Sets, func(s Set) bool { return s.Prefix == prefix && prefix != EmojiSet }) {
		return nil, fmt.Errorf("no icon set %q", prefix)
	}
	f, _ := setsOnce.LoadOrStore(prefix, sync.OnceValues(func() (*iconSet, error) {
		raw, err := readGz(prefix + ".json.gz")
		if err != nil {
			return nil, err
		}
		var s iconSet
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		for n, d := range s.Icons {
			if !d.Hidden {
				s.names = append(s.names, n)
			}
		}
		sort.Strings(s.names)
		return &s, nil
	}))
	return f.(func() (*iconSet, error))()
}

func loadEmoji() ([]emoji, error) {
	raw, err := readGz("emoji.txt.gz")
	if err != nil {
		return nil, err
	}
	var out []emoji
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) == 3 {
			out = append(out, emoji{f[0], f[1], f[2]})
		}
	}
	return out, sc.Err()
}

// split returns the set and name of an icon id; a bare name is Lucide's.
func split(id string) (prefix, name string) {
	if p, n, ok := strings.Cut(id, ":"); ok {
		return p, n
	}
	if n, ok := legacy[id]; ok {
		return "lucide", n
	}
	return "lucide", id
}

// isEmoji reports whether s is one emoji Nota knows, with or without
// its variation selector.
func isEmoji(s string) bool {
	return s != "" && emojiSet()[strings.ReplaceAll(s, "\ufe0f", "")]
}

// SVG returns an icon as an <svg> element in currentColor, sized 1em.
// An emoji has no SVG: it returns "", nil. An icon Nota doesn't have is
// an error.
func SVG(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("no icon given")
	}
	if isEmoji(id) {
		return "", nil
	}
	prefix, name := split(id)
	s, err := load(prefix)
	if err != nil {
		return "", err
	}
	for range 8 {
		if _, ok := s.Icons[name]; ok {
			break
		}
		a, ok := s.Aliases[name]
		if !ok {
			break
		}
		name = a.Parent
	}
	d, ok := s.Icons[name]
	if !ok {
		return "", fmt.Errorf("no icon %q in %s", name, prefix)
	}
	w, h := d.Width, d.Height
	if w == 0 {
		w = or(s.Width, 16)
	}
	if h == 0 {
		h = or(s.Height, 16)
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 %d %d">%s</svg>`, w, h, d.Body), nil
}

func or(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}

// Check reports whether Nota can draw an icon.
func Check(id string) error {
	_, err := SVG(id)
	return err
}

// Normalize returns an icon id as Nota stores it: trimmed, and a bare
// Lucide name with its set ("house" → "lucide:house"). An emoji stays.
func Normalize(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || isEmoji(id) {
		return id
	}
	p, n := split(id)
	return p + ":" + n
}

// suggestions is what the picker shows before anything is typed.
var suggestions = []string{
	"folder", "folder-open", "briefcase", "house", "user", "users", "book-open", "notebook",
	"graduation-cap", "code", "terminal", "git-branch", "server", "database", "cloud", "cpu",
	"wrench", "settings", "lightbulb", "rocket", "target", "flag", "star", "heart",
	"bookmark", "calendar", "clock", "inbox", "archive", "trash-2", "file-text", "pen-line",
	"image", "camera", "music", "film", "plane", "map", "globe", "car",
	"utensils", "coffee", "dumbbell", "wallet", "piggy-bank", "receipt", "shopping-cart", "gift",
	"baby", "dog", "leaf", "sun", "moon", "zap", "flame", "sparkles",
}

// Search finds icons whose name has every word of q, best matches
// first: a name that is q, then one that starts with it, then one with a
// word that does, then any. Sets break ties in the order of Sets. An
// empty q returns a selection of common icons. set limits the search to
// one set; "" searches them all.
func Search(q, set string, limit int) ([]Icon, error) {
	if limit <= 0 {
		limit = 120
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" && set == "" {
		out := make([]Icon, 0, len(suggestions))
		for _, n := range suggestions {
			if ic, err := icon("lucide", n); err == nil {
				out = append(out, ic)
			}
		}
		return cut(out, limit), nil
	}
	words := strings.FieldsFunc(q, func(r rune) bool { return r == ' ' || r == '-' || r == '_' || r == ':' })
	type hit struct {
		rank, set int
		ic        Icon
	}
	var hits []hit
	for si, s := range Sets {
		if set != "" && s.Prefix != set {
			continue
		}
		if s.Prefix == EmojiSet {
			es, err := emojiAll()
			if err != nil {
				return nil, err
			}
			for _, e := range es {
				if strings.Contains(e.name, "skin tone") && !strings.Contains(q, "skin") {
					continue
				}
				if r, ok := rank(e.name, q, words); ok {
					hits = append(hits, hit{r, si, Icon{ID: e.char, Set: EmojiSet, Name: e.name}})
				}
			}
			continue
		}
		is, err := load(s.Prefix)
		if err != nil {
			return nil, err
		}
		for _, n := range is.names {
			if r, ok := rank(n, q, words); ok {
				hits = append(hits, hit{r, si, Icon{ID: s.Prefix + ":" + n, Set: s.Prefix, Name: n}})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].set < hits[j].set
	})
	out := make([]Icon, 0, min(limit, len(hits)))
	for _, h := range hits {
		if len(out) == limit {
			break
		}
		if h.ic.Set != EmojiSet {
			svg, err := SVG(h.ic.ID)
			if err != nil {
				continue
			}
			h.ic.SVG = svg
		}
		out = append(out, h.ic)
	}
	return out, nil
}

func icon(prefix, name string) (Icon, error) {
	id := prefix + ":" + name
	svg, err := SVG(id)
	return Icon{ID: id, Set: prefix, Name: name, SVG: svg}, err
}

func cut(ics []Icon, n int) []Icon {
	if len(ics) > n {
		return ics[:n]
	}
	return ics
}

// rank scores how well name matches q (lower is better); ok is false
// when a word of q is not in it. An empty q matches everything.
func rank(name, q string, words []string) (int, bool) {
	if q == "" {
		return 4, true
	}
	n := strings.ToLower(name)
	for _, w := range words {
		if !strings.Contains(n, w) {
			return 0, false
		}
	}
	norm := strings.NewReplacer(" ", "-", "_", "-").Replace
	switch nq := norm(q); {
	case norm(n) == nq:
		return 0, true
	case strings.HasPrefix(norm(n), nq):
		return 1, true
	}
	for _, part := range strings.FieldsFunc(n, func(r rune) bool { return r == ' ' || r == '-' || r == ':' }) {
		if strings.HasPrefix(part, words[0]) {
			return 2, true
		}
	}
	return 3, true
}

// Colors are the colours a folder or view icon can have; the app draws
// each from the theme. "" is the colour of the text around it.
var Colors = []string{"accent", "red", "orange", "yellow", "green", "blue", "purple", "pink", "gray"}

// CheckColor reports whether c is "" or one of Colors.
func CheckColor(c string) error {
	if c == "" || slices.Contains(Colors, c) {
		return nil
	}
	return fmt.Errorf("no colour %q: want one of %s", c, strings.Join(Colors, ", "))
}

// Look is how a folder or a view is drawn: its icon and colour as
// stored, and the icon's SVG (empty for an emoji).
type Look struct {
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	SVG   string `json:"svg,omitempty"`
}

// LookOf draws a stored icon and colour. An icon Nota doesn't have is
// left out, and so is a colour; a newer Nota may have written them.
func LookOf(icon, color string) Look {
	l := Look{Icon: strings.TrimSpace(icon), Color: strings.TrimSpace(color)}
	if l.Icon != "" {
		svg, err := SVG(l.Icon)
		if err != nil {
			l.Icon = ""
		}
		l.SVG = svg
	}
	if CheckColor(l.Color) != nil {
		l.Color = ""
	}
	return l
}
