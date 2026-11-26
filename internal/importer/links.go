package importer

import (
	"path"
	"slices"
	"strings"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/relink"
	"github.com/banjo/nota/internal/vault"
)

// linker rewrites the links of imported notes that point at other
// imported files, where the file's new name or folder would break them.
// Links are resolved in the source the way Obsidian does: a wikilink by
// path, then by the end of a path (the file name); a markdown link from
// the note's folder, then from the source's root.
type linker struct {
	bySrc map[string]*plan
	// byName lists the plans by the fold key of their file name, and of
	// their file name without .md for notes.
	byName map[string][]*plan
}

func newLinker(plans []*plan) *linker {
	l := &linker{bySrc: map[string]*plan{}, byName: map[string][]*plan{}}
	for _, p := range plans {
		if p.item.Status == Skipped {
			continue
		}
		l.bySrc[key(p.src)] = p
		base := key(path.Base(p.src))
		l.byName[base] = append(l.byName[base], p)
		if p.note {
			stem := strings.TrimSuffix(base, path.Ext(base))
			l.byName[stem] = append(l.byName[stem], p)
		}
	}
	return l
}

// wiki finds the file a wikilink target names.
func (l *linker) wiki(target string) *plan {
	t := key(strings.TrimPrefix(strings.TrimPrefix(target, "./"), "/"))
	for _, c := range []string{t, t + vault.Ext} {
		if p, ok := l.bySrc[c]; ok {
			return p
		}
	}
	var best *plan
	for _, p := range l.byName[path.Base(t)] {
		s := key(p.src)
		if p.note {
			s = strings.TrimSuffix(s, path.Ext(s))
		}
		if s != t && !strings.HasSuffix(s, "/"+t) && key(p.src) != t && !strings.HasSuffix(key(p.src), "/"+t) {
			continue
		}
		if best == nil || len(p.src) < len(best.src) || (len(p.src) == len(best.src) && p.src < best.src) {
			best = p
		}
	}
	return best
}

// rewrite returns a note's content with its links fixed, and how many
// it changed.
func (l *linker) rewrite(pl *plan, data []byte) ([]byte, int) {
	body := frontmatter.Split(data).Body()
	offset := len(data) - len(body)
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, r := range markdown.Refs(body, vault.Folder(pl.src)) {
		var text string
		if r.Wiki {
			text = l.wikiText(r)
			// A link shows its target: keep showing what it did.
			if text != "" && !r.Embed && r.Alias == "" && r.Heading == "" && strings.HasPrefix(string(body[r.End:]), "]]") {
				text += "|" + strings.ReplaceAll(r.Raw, "|", "")
			}
		} else {
			text = l.markdownText(r, pl)
		}
		if text != "" && text != r.Raw {
			edits = append(edits, edit{r.Start, r.End, text})
		}
	}
	if len(edits) == 0 {
		return data, 0
	}
	slices.SortFunc(edits, func(a, b edit) int { return a.start - b.start })
	out := slices.Clone(data)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out = slices.Concat(out[:offset+e.start], []byte(e.text), out[offset+e.end:])
	}
	return out, len(edits)
}

// wikiText is the new target of a wikilink, "" to keep it: the file
// name when the link named its file by name, else the vault path, in
// the link's style (with or without .md).
func (l *linker) wikiText(r markdown.Ref) string {
	p := l.wiki(r.Target)
	if p == nil {
		return ""
	}
	ext := !p.note || strings.EqualFold(path.Ext(r.Raw), vault.Ext)
	name := func(s string) string {
		if !ext {
			return strings.TrimSuffix(s, path.Ext(s))
		}
		return s
	}
	t := key(strings.TrimPrefix(strings.TrimPrefix(r.Raw, "./"), "/"))
	if !strings.Contains(r.Raw, "/") {
		if n := name(path.Base(p.dest)); key(n) != t {
			return n
		}
		return ""
	}
	full := name(p.dest)
	if k := key(full); k == t || strings.HasSuffix(k, "/"+t) {
		return ""
	}
	return full
}

// markdownText is the new destination of a markdown link, "" to keep it.
func (l *linker) markdownText(r markdown.Ref, pl *plan) string {
	p := l.bySrc[key(r.Target)]
	if p == nil && !strings.HasPrefix(r.Raw, ".") {
		if root := markdown.Refs([]byte("[]("+dest(r)+")"), ""); len(root) == 1 {
			p = l.bySrc[key(root[0].Target)]
		}
	}
	if p == nil {
		return ""
	}
	if now := markdown.Refs([]byte("[]("+dest(r)+")"), vault.Folder(pl.dest)); len(now) == 1 && now[0].Target == p.dest {
		return ""
	}
	return relink.Destination(r.Raw, r.Angle, pl.dest, p.dest)
}

// dest is a markdown link's destination as written.
func dest(r markdown.Ref) string {
	if r.Angle {
		return "<" + r.Raw + ">"
	}
	return r.Raw
}
