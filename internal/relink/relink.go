// Package relink keeps links pointing at the same notes when notes move.
//
// A move is done in two steps around the vault call that moves the files:
// Prepare records, while the index still describes the vault before the
// move, every link that could break or point somewhere else afterwards
// and which note it points to now. Apply then runs with the index
// describing the vault after the move, and rewrites the links that no
// longer resolve to the same note. Only the target inside each link
// changes; headings, block ids, aliases and the rest of the file stay
// byte for byte, and each changed note is saved like any other edit.
package relink

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/vault"
)

// Result says which notes Apply changed.
type Result struct {
	// Count is the number of links rewritten.
	Count int `json:"count"`
	// Notes are the changed notes, by their path after the move.
	Notes []Changed `json:"notes"`
}

// Changed is one note whose links were rewritten.
type Changed struct {
	Path  string `json:"path"`
	Links int    `json:"links"`
}

// Pending is a move whose links have been recorded but not rewritten.
type Pending struct {
	v       *vault.Vault
	ix      *index.Index
	remap   func(string) string
	sources []*source
}

type source struct {
	path string
	raw  []byte
	refs []ref
}

// ref is a link and where it pointed before the move.
type ref struct {
	markdown.Ref
	// to is the note (or file) the link resolved to before the move.
	to string
	// named is true when the wikilink named its note by path or file
	// name, not by title or alias.
	named bool
}

// Prepare records the links affected by moving notes from the keys of
// moves to their values (planned paths; Apply takes where they really
// went). The index must be current.
func Prepare(v *vault.Vault, ix *index.Index, moves map[string]string) (*Pending, error) {
	return prepare(v, ix, moves, func(p string) string {
		if to, ok := moves[p]; ok {
			return to
		}
		return p
	})
}

// PrepareFolder is Prepare for renaming the folder from to the vault path
// to: every note in it moves, and files in it (images) move too.
func PrepareFolder(v *vault.Vault, ix *index.Index, from, to string) (*Pending, error) {
	es, err := ix.Query(index.Query{Folder: from})
	if err != nil {
		return nil, err
	}
	remap := func(p string) string {
		if rest, ok := strings.CutPrefix(p, from+"/"); ok {
			return path.Join(to, rest)
		}
		return p
	}
	moves := make(map[string]string, len(es))
	for _, e := range es {
		moves[e.Path] = remap(e.Path)
	}
	return prepare(v, ix, moves, remap)
}

func prepare(v *vault.Vault, ix *index.Index, moves map[string]string, remap func(string) string) (*Pending, error) {
	p := &Pending{v: v, ix: ix, remap: remap}
	if len(moves) == 0 {
		return p, nil
	}
	seen := map[string]bool{}
	var paths []string
	keys := make([]string, 0, 2*len(moves))
	for from, to := range moves {
		keys = append(keys, from, to)
		if !seen[from] {
			seen[from] = true
			paths = append(paths, from)
		}
	}
	refs, err := ix.Referrers(keys...)
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			paths = append(paths, r)
		}
	}
	slices.Sort(paths)
	for _, sp := range paths {
		n, err := v.Read(sp)
		if errors.Is(err, vault.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		_, moved := moves[sp]
		src := &source{path: sp, raw: n.Raw}
		for _, r := range markdown.Refs([]byte(n.Body), vault.Folder(sp)) {
			var to string
			if r.File {
				// A file link breaks when its note moves, or when the
				// file moves with its folder.
				if (!moved && remap(r.Target) == r.Target) || !v.FileExists(r.Target) {
					continue
				}
				to = r.Target
			} else if to, err = ix.ResolveLink(r.Target, sp, r.Wiki); err != nil {
				return nil, err
			}
			if to == "" {
				continue // a broken link has nothing to keep pointing at
			}
			if remap(to) == to && !moved && !r.Wiki {
				continue // a path link to a note that stays keeps working
			}
			src.refs = append(src.refs, ref{Ref: r, to: to, named: r.Wiki && slices.Contains(index.PathKeys(to), index.LinkKey(r.Target))})
		}
		if len(src.refs) > 0 {
			slices.SortFunc(src.refs, func(a, b ref) int { return a.Start - b.Start })
			p.sources = append(p.sources, src)
		}
	}
	return p, nil
}

// Apply rewrites the recorded links that no longer point where they did.
// moved maps the old path of each moved note to where it ended up (hooks
// may have filed it elsewhere than planned); notes it leaves out are
// taken to have moved as planned. The index must describe the vault after
// the move. A note that changed since Prepare is left alone.
func (p *Pending) Apply(moved map[string]string) (Result, error) {
	res := Result{Notes: []Changed{}}
	final := func(x string) string {
		if to, ok := moved[x]; ok {
			return to
		}
		return p.remap(x)
	}
	for _, s := range p.sources {
		at := final(s.path)
		n, err := p.v.Read(at)
		if errors.Is(err, vault.ErrNotFound) {
			continue
		}
		if err != nil {
			return res, err
		}
		if !bytes.Equal(n.Raw, s.raw) {
			continue
		}
		offset := len(n.Raw) - len(frontmatter.Split(n.Raw).Body())
		out := slices.Clone(n.Raw)
		count := 0
		// From the end, so earlier offsets stay valid.
		for i := len(s.refs) - 1; i >= 0; i-- {
			r := s.refs[i]
			want := final(r.to)
			text, err := p.target(r, at, want)
			if err != nil {
				return res, err
			}
			if text == "" || text == r.Raw {
				continue
			}
			start, end := offset+r.Start, offset+r.End
			out = slices.Concat(out[:start], []byte(text), out[end:])
			count++
		}
		if count == 0 {
			continue
		}
		if _, _, err := p.v.Save(at, out); err != nil {
			return res, err
		}
		res.Count += count
		res.Notes = append(res.Notes, Changed{Path: at, Links: count})
	}
	slices.SortFunc(res.Notes, func(a, b Changed) int { return strings.Compare(a.Path, b.Path) })
	return res, nil
}

// target returns the new target text of r in the note at, so that it
// points to want; "" or r.Raw when it can stay as it is.
func (p *Pending) target(r ref, at, want string) (string, error) {
	if r.File || !r.Wiki {
		// Where the link, as written, points from the note's folder now.
		dest := r.Raw
		if r.Angle {
			dest = "<" + dest + ">"
		}
		cur := ""
		if rs := markdown.Refs([]byte("[]("+dest+")"), vault.Folder(at)); len(rs) == 1 {
			cur = rs[0].Target
		}
		if cur == want {
			return "", nil
		}
		return markdownTarget(r, at, want), nil
	}
	now, err := p.ix.ResolveLink(r.Target, at, true)
	if err != nil {
		return "", err
	}
	if now == want && (!r.named || slices.Contains(index.PathKeys(want), index.LinkKey(r.Target))) {
		return "", nil
	}
	return p.wikiTarget(r, at, want)
}

// wikiTarget writes a wikilink to want in the style of r: the file name
// when that is enough and r used one, else the vault path.
func (p *Pending) wikiTarget(r ref, at, want string) (string, error) {
	ext := strings.HasSuffix(strings.ToLower(r.Raw), vault.Ext)
	full := strings.TrimSuffix(want, vault.Ext)
	if !strings.Contains(r.Raw, "/") {
		name := vault.Stem(want)
		if ext {
			name += vault.Ext
		}
		got, err := p.ix.ResolveLink(name, at, true)
		if err != nil {
			return "", err
		}
		if got == want {
			return name, nil
		}
	}
	if ext {
		full += vault.Ext
	}
	return full, nil
}

// markdownTarget writes a markdown link destination for want from the
// note at at: from the vault root when r was, else relative to at's
// folder, escaped unless r is in <angle brackets>.
func markdownTarget(r ref, at, want string) string {
	var dest string
	switch {
	case strings.HasPrefix(r.Raw, "/"):
		dest = "/" + want
	default:
		dest = relative(vault.Folder(at), want)
		if strings.HasPrefix(r.Raw, "./") && !strings.HasPrefix(dest, "../") {
			dest = "./" + dest
		}
	}
	return escape(dest, r.Angle)
}

// Destination is markdownTarget for a link written raw (in <angle
// brackets> when angle) in the note at at: a destination that points
// to want.
func Destination(raw string, angle bool, at, want string) string {
	return markdownTarget(ref{Ref: markdown.Ref{Raw: raw, Angle: angle}}, at, want)
}

// relative returns the path of the vault path to from the folder dir.
func relative(dir, to string) string {
	if dir == "" {
		return to
	}
	from := strings.Split(dir, "/")
	parts := strings.Split(to, "/")
	i := 0
	for i < len(from) && i < len(parts)-1 && from[i] == parts[i] {
		i++
	}
	return strings.Repeat("../", len(from)-i) + strings.Join(parts[i:], "/")
}

// escape makes a path safe as a markdown link destination: inside angle
// brackets it is used as is; otherwise the characters that would end or
// change the link are percent-encoded, and other text (letters in any
// script) is kept readable.
func escape(p string, angle bool) string {
	if angle {
		return p
	}
	var b strings.Builder
	for _, c := range p {
		switch c {
		case ' ', '%', '(', ')', '<', '>', '#', '?', '"', '\t':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}
