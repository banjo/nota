// Package replace finds and replaces text across notes: in note bodies
// only (never frontmatter), skipping code unless asked and the targets
// of links to notes and files always. The CLI, MCP,
// the desktop app and the server's API share it.
package replace

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/vault"
)

// Options is what to find and what to put instead.
type Options struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
	// Regex reads Find as a regular expression (RE2) and expands $1,
	// ${name} in Replace; otherwise both are literal text.
	Regex bool `json:"regex,omitempty"`
	// Case matches case exactly; by default it is ignored.
	Case bool `json:"case,omitempty"`
	// Word matches only whole words: no letter, digit or _ on either side.
	Word bool `json:"word,omitempty"`
	// Code includes code blocks and inline code, which are skipped by default.
	Code bool `json:"code,omitempty"`
}

// Match is one place a note would change.
type Match struct {
	// Line is the 1-based line in the file.
	Line int `json:"line"`
	// Start and End are byte offsets in the file.
	Start int `json:"start"`
	End   int `json:"end"`
	// Text is what is there, With what it becomes.
	Text string `json:"text"`
	With string `json:"with"`
	// Before and After are the rest of the line around the match,
	// shortened, for showing it.
	Before string `json:"before"`
	After  string `json:"after"`
}

// Note is a note with matches: Hash is the file the matches were found
// in, so a later Apply can tell whether it changed since.
type Note struct {
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Hash    string  `json:"hash"`
	Matches []Match `json:"matches"`
}

// Replacer is a compiled Options.
type Replacer struct {
	opts Options
	re   *regexp.Regexp
}

// New checks and compiles o.
func New(o Options) (*Replacer, error) {
	if o.Find == "" {
		return nil, errors.New("nothing to find")
	}
	pat := o.Find
	if !o.Regex {
		pat = regexp.QuoteMeta(pat)
	} else if _, err := syntax.Parse(pat, syntax.Perl); err != nil {
		var se *syntax.Error
		if errors.As(err, &se) {
			return nil, fmt.Errorf("invalid regular expression: %s: `%s`", se.Code, se.Expr)
		}
		return nil, fmt.Errorf("invalid regular expression: %w", err)
	}
	if !o.Case {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile("(?m)" + pat)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression: %w", err)
	}
	if o.Regex {
		o.Replace = numberedGroups(o.Replace)
	}
	return &Replacer{opts: o, re: re}, nil
}

// numberedGroups writes $1 as ${1}, so "$1st" is group 1 and "st" as in
// JavaScript and most editors, not a group named "1st" as in Go.
func numberedGroups(tmpl string) string {
	var b strings.Builder
	for i := 0; i < len(tmpl); i++ {
		c := tmpl[i]
		if c != '$' || i+1 == len(tmpl) {
			b.WriteByte(c)
			continue
		}
		if tmpl[i+1] == '$' {
			b.WriteString("$$")
			i++
			continue
		}
		j := i + 1
		for j < len(tmpl) && tmpl[j] >= '0' && tmpl[j] <= '9' {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			continue
		}
		b.WriteString("${" + tmpl[i+1:j] + "}")
		i = j - 1
	}
	return b.String()
}

const context = 40

// Matches lists the matches in a note's raw content.
func (r *Replacer) Matches(raw []byte) []Match {
	body := frontmatter.Split(raw).Body()
	off := len(raw) - len(body)
	// Where a link points (and its #heading) is left alone: renaming a note relinks it,
	// and text replaced there would break the link.
	var skip [][2]int
	for _, ref := range markdown.Refs(body, "") {
		e := ref.End // and the #heading after it
		for e < len(body) && !strings.ContainsRune("|])> \t\n", rune(body[e])) {
			e++
		}
		skip = append(skip, [2]int{ref.Start, e})
	}
	if !r.opts.Code {
		skip = append(skip, markdown.CodeRanges(body)...)
	}
	skipped := func(s, e int) bool {
		for _, c := range skip {
			if s < c[1] && e > c[0] {
				return true
			}
		}
		return false
	}
	var out []Match
	for _, loc := range r.re.FindAllSubmatchIndex(body, -1) {
		s, e := loc[0], loc[1]
		if s == e || skipped(s, e) || (r.opts.Word && !wholeWord(body, s, e)) {
			continue
		}
		with := r.opts.Replace
		if r.opts.Regex {
			with = string(r.re.Expand(nil, []byte(r.opts.Replace), body, loc))
		}
		ls := strings.LastIndexByte(string(body[:s]), '\n') + 1
		le := len(body)
		if i := strings.IndexByte(string(body[e:]), '\n'); i >= 0 {
			le = e + i
		}
		out = append(out, Match{
			Line:   strings.Count(string(raw[:off+s]), "\n") + 1,
			Start:  off + s,
			End:    off + e,
			Text:   string(body[s:e]),
			With:   with,
			Before: tail(string(body[ls:s]), context),
			After:  head(string(body[e:le]), context),
		})
	}
	return out
}

// Apply returns raw with the given matches (from Matches on the same
// raw) replaced.
func Apply(raw []byte, ms []Match) []byte {
	sorted := append([]Match(nil), ms...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start > sorted[j].Start })
	out := append([]byte(nil), raw...)
	last := len(out) + 1
	for _, m := range sorted {
		if m.End > last {
			continue // overlapping: the later one already replaced
		}
		out = append(out[:m.Start], append([]byte(m.With), out[m.End:]...)...)
		last = m.Start
	}
	return out
}

func wholeWord(b []byte, s, e int) bool {
	word := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	if s > 0 {
		if r, _ := utf8.DecodeLastRune(b[:s]); word(r) {
			return false
		}
	}
	if e < len(b) {
		if r, _ := utf8.DecodeRune(b[e:]); word(r) {
			return false
		}
	}
	return true
}

// tail keeps the last n runes of s, with "…" when cut.
func tail(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)
	return "…" + string(rs[len(rs)-n:])
}

func head(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// Find lists the notes matching q that have matches, in q's order.
func Find(v *vault.Vault, ix *index.Index, r *Replacer, q index.Query) ([]Note, error) {
	es, err := ix.Query(q)
	if err != nil {
		return nil, err
	}
	out := []Note{}
	for _, e := range es {
		n, err := v.Read(e.Path)
		if errors.Is(err, vault.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if ms := r.Matches(n.Raw); len(ms) > 0 {
			out = append(out, Note{Path: n.Path, Title: e.Title, Hash: protocol.Hash(n.Raw), Matches: ms})
		}
	}
	return out, nil
}

// Pick is the matches to replace in one note, by index into the list
// Find gave; nil means all of them. Hash, when set, must be the note's
// hash for the replace to go ahead.
type Pick struct {
	Path    string `json:"path"`
	Hash    string `json:"hash,omitempty"`
	Matches []int  `json:"matches,omitempty"`
}

// Result is what a replace changed: the notes, with how many matches
// each, and their content before, for undo.
type Result struct {
	Notes []Changed `json:"notes"`
	Count int       `json:"count"`
	// Stale are the picked notes that changed since their matches were
	// found; they were left alone.
	Stale []string `json:"stale"`
}

// Changed is one note a replace wrote.
type Changed struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
	// Before is the body the note had (frontmatter excluded): saving it
	// back undoes the replace.
	Before string `json:"before"`
	// Hash is the note as written, so an undo can check nothing changed since.
	Hash string `json:"hash"`
}

// ApplyNotes replaces the picked matches, saving each note as an edit
// (updated, hooks and sync as usual). A note whose hash no longer
// matches is left alone and listed in Stale.
func ApplyNotes(v *vault.Vault, r *Replacer, picks []Pick) (Result, error) {
	res := Result{Notes: []Changed{}, Stale: []string{}}
	for _, p := range picks {
		n, err := v.Read(p.Path)
		if err != nil {
			return res, err
		}
		if p.Hash != "" && protocol.Hash(n.Raw) != p.Hash {
			res.Stale = append(res.Stale, n.Path)
			continue
		}
		ms := r.Matches(n.Raw)
		if p.Matches != nil {
			var sel []Match
			for _, i := range p.Matches {
				if i < 0 || i >= len(ms) {
					return res, fmt.Errorf("%s: no match %d", n.Path, i)
				}
				sel = append(sel, ms[i])
			}
			ms = sel
		}
		if len(ms) == 0 {
			continue
		}
		out := Apply(n.Raw, ms)
		if _, _, err := v.Save(n.Path, out); err != nil {
			return res, err
		}
		written, err := v.Read(n.Path)
		if err != nil {
			return res, err
		}
		res.Notes = append(res.Notes, Changed{Path: n.Path, Count: len(ms), Before: string(frontmatter.Split(n.Raw).Body()), Hash: protocol.Hash(written.Raw)})
		res.Count += len(ms)
	}
	return res, nil
}
