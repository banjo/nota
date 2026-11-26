// Package export turns a note, a folder or the whole vault into files to
// hand to someone or to another app (docs/cli.md, Export): a self-contained
// HTML page in Reading mode's look, Markdown without Nota's and
// Obsidian's own syntax, or the files as they are, in a zip.
//
// A note exports as one file when it can; a folder, the vault, and a
// note whose Markdown needs its images along export as a zip. Links
// between exported notes lead from file to file; a link to a note that
// is not part of the export is left as its text.
package export

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/publish"
	"github.com/banjo/nota/internal/vault"
)

// Format is what an export is made of.
type Format string

const (
	// HTML is a web page per note, in Reading mode's look.
	HTML Format = "html"
	// Markdown is the notes without wikilinks, %%comments%%, block ids
	// and inline footnotes, for apps that don't read them.
	Markdown Format = "md"
	// Files are the notes and their images as they are in the vault.
	Files Format = "zip"
)

// Formats lists the formats, for usage and errors.
var Formats = []Format{HTML, Markdown, Files}

// ErrNotFound is returned for a target that is neither a note nor a folder.
var ErrNotFound = errors.New("no such note or folder")

// Options configure an export.
type Options struct {
	Format Format
	// Theme is the look of the HTML pages; nil is Nota's own, following
	// the reader's light or dark mode.
	Theme *publish.Theme
	// Name names the zip of the whole vault ("Notes" makes "Notes.zip").
	Name string
}

// Export is a prepared export: its file name and type, and its content
// for WriteTo.
type Export struct {
	// Name is the file name to save it under: "Plan.html", "Projekt.zip".
	Name string
	// Type is its content type.
	Type string
	// Notes is the number of notes in it.
	Notes int

	v     *vault.Vault
	one   []byte // the single file, when it is not a zip
	files []file // the zip's files
}

type file struct {
	name string
	data []byte // or, when nil, the vault file at src
	src  string
}

// New prepares the export of target: a note's path, a folder's, or ""
// for the whole vault. Notes are read and rendered here; images are read
// when the export is written.
func New(v *vault.Vault, ix *index.Index, target string, o Options) (*Export, error) {
	if !slices.Contains(Formats, o.Format) {
		return nil, fmt.Errorf("unknown format %q (want html, md or zip)", o.Format)
	}
	target = strings.Trim(target, "/")
	if target != "" {
		c, err := vault.CleanPath(target)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, target)
		}
		target = c
	}
	var notes []string
	var root, name string
	switch {
	case vault.IsNote(target) && v.Exists(target) && !v.Ignored(target, false):
		notes = []string{target}
		root = vault.Folder(target)
		name = strings.TrimSuffix(path.Base(target), ".md")
	case target == "" || isFolder(v, target):
		err := v.Walk(func(e vault.Entry) error {
			if target == "" || strings.HasPrefix(e.Path, target+"/") {
				notes = append(notes, e.Path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		root = target
		name = path.Base(target)
		if target == "" {
			name = o.Name
			if name == "" {
				name = "Nota"
			}
		}
	default:
		return nil, fmt.Errorf("%w: %s", ErrNotFound, target)
	}
	slices.Sort(notes)
	e := &Export{v: v, Notes: len(notes)}
	single := vault.IsNote(target)

	if o.Format == Files {
		e.Name, e.Type = name+".zip", "application/zip"
		seen := map[string]bool{}
		add := func(p string) {
			if !seen[p] {
				seen[p] = true
				e.files = append(e.files, file{name: p, src: p})
			}
		}
		if !single {
			err := v.WalkFiles(func(f vault.Entry) error {
				if target == "" || strings.HasPrefix(f.Path, target+"/") {
					add(f.Path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		for _, n := range notes {
			add(n)
			raw, err := v.ReadFile(n)
			if err != nil {
				return nil, err
			}
			for _, img := range images(v, raw, n) {
				add(img)
			}
		}
		slices.SortFunc(e.files, func(a, b file) int { return strings.Compare(a.name, b.name) })
		return e, nil
	}

	m := &mapper{v: v, ix: ix, root: root, format: o.Format, inline: single && o.Format == HTML,
		notes: map[string]string{}, images: map[string]string{}, taken: map[string]bool{}}
	for _, n := range notes {
		m.notes[n] = m.place(n, m.ext(n))
	}
	var out []file
	for _, n := range notes {
		raw, err := v.ReadFile(n)
		if err != nil {
			return nil, err
		}
		var data []byte
		if o.Format == HTML {
			if data, err = m.html(raw, n, o.Theme); err != nil {
				return nil, err
			}
		} else {
			data = m.markdown(raw, n)
		}
		out = append(out, file{name: m.notes[n], data: data})
	}
	if single && len(m.order) == 0 {
		e.Name, e.one = name+"."+string(o.Format), out[0].data
		e.Type = map[Format]string{HTML: "text/html; charset=utf-8", Markdown: "text/markdown; charset=utf-8"}[o.Format]
		return e, nil
	}
	for _, img := range m.order {
		out = append(out, file{name: m.images[img], src: img})
	}
	slices.SortFunc(out, func(a, b file) int { return strings.Compare(a.name, b.name) })
	e.Name, e.Type, e.files = name+".zip", "application/zip", out
	return e, nil
}

// WriteTo writes the export: the file, or the zip.
func (e *Export) WriteTo(w io.Writer) (int64, error) {
	cw := &counter{w: w}
	if e.files == nil {
		_, err := cw.Write(e.one)
		return cw.n, err
	}
	z := zip.NewWriter(cw)
	now := time.Now()
	for _, f := range e.files {
		data := f.data
		if data == nil {
			var err error
			if data, err = e.v.ReadFile(f.src); err != nil {
				return cw.n, err
			}
		}
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: now}
		if vault.ImageType(f.name) != "" {
			h.Method = zip.Store // already compressed
		}
		fw, err := z.CreateHeader(h)
		if err != nil {
			return cw.n, err
		}
		if _, err := fw.Write(data); err != nil {
			return cw.n, err
		}
	}
	err := z.Close()
	return cw.n, err
}

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// mapper places the files of an HTML or Markdown export: notes keep
// their place under the exported folder, images outside it go to
// attachments/. It resolves links to the place of their target.
type mapper struct {
	v      *vault.Vault
	ix     *index.Index
	root   string
	format Format
	inline bool // images as data: URLs (a single HTML page)

	notes  map[string]string // vault path → name in the export
	images map[string]string
	order  []string // images, in the order first linked
	taken  map[string]bool
}

func (m *mapper) ext(p string) string {
	if m.format == HTML && vault.IsNote(p) {
		return ".html"
	}
	return path.Ext(p)
}

// place names a vault file in the export, unique.
func (m *mapper) place(p, ext string) string {
	name := strings.TrimSuffix(p, path.Ext(p))
	if m.root != "" {
		if rest, ok := strings.CutPrefix(name, m.root+"/"); ok {
			name = rest
		} else {
			name = "attachments/" + path.Base(name)
		}
	}
	try := name + ext
	for i := 2; m.taken[strings.ToLower(try)]; i++ {
		try = fmt.Sprintf("%s %d%s", name, i, ext)
	}
	m.taken[strings.ToLower(try)] = true
	return try
}

// note returns where a link to the note at p leads from the note at
// from, ok false when p is not part of the export.
func (m *mapper) note(p, from string) (string, bool) {
	to, ok := m.notes[p]
	if !ok {
		return "", false
	}
	if p == from {
		return "", true
	}
	return relURL(m.notes[from], to), true
}

// image returns where an image of the vault at p is, from the note at
// from, and takes it along.
func (m *mapper) image(p, from string) (string, bool) {
	if m.inline {
		data, err := m.v.ReadFile(p)
		if err != nil {
			return "", false
		}
		return "data:" + vault.ImageType(p) + ";base64," + base64.StdEncoding.EncodeToString(data), true
	}
	name, ok := m.images[p]
	if !ok {
		name = m.place(p, path.Ext(p))
		m.images[p] = name
		m.order = append(m.order, p)
	}
	return relURL(m.notes[from], name), true
}

func (m *mapper) resolveNote(l markdown.Link, from string) string {
	if m.ix == nil {
		if l.Wiki {
			return ""
		}
		return l.Target
	}
	p, err := m.ix.ResolveLink(l.Target, from, l.Wiki)
	if err != nil {
		return ""
	}
	return p
}

func (m *mapper) resolveImage(target, from string, wiki bool) string {
	if wiki {
		p, err := m.v.FindFile(target, from)
		if err != nil {
			return ""
		}
		target = p
	} else if !m.v.HasFile(target) {
		return ""
	}
	if vault.ImageType(target) == "" {
		return ""
	}
	return target
}

// html renders a note as a page.
func (m *mapper) html(raw []byte, from string, theme *publish.Theme) ([]byte, error) {
	title, body := publish.RenderNote(raw, publish.Options{Path: from, Resolver: resolver{m, from}})
	var b bytes.Buffer
	err := publish.WritePage(&b, publish.Page{Title: title, Body: template.HTML(body), Theme: theme, Document: true}) //nolint:gosec // rendered by publish
	return b.Bytes(), err
}

type resolver struct {
	m    *mapper
	from string
}

func (r resolver) Note(l markdown.Link) (string, bool) {
	p := r.m.resolveNote(l, r.from)
	if p == "" {
		return "", false
	}
	return r.m.note(p, r.from)
}

func (r resolver) Image(target string, wiki bool) (string, bool) {
	p := r.m.resolveImage(target, r.from, wiki)
	if p == "" {
		return "", false
	}
	return r.m.image(p, r.from)
}

// relURL is the relative URL of the export file to from the file from.
func relURL(from, to string) string {
	f := strings.Split(path.Dir(from), "/")
	if path.Dir(from) == "." {
		f = nil
	}
	t := strings.Split(to, "/")
	i := 0
	for i < len(f) && i < len(t)-1 && f[i] == t[i] {
		i++
	}
	var parts []string
	for range f[i:] {
		parts = append(parts, "..")
	}
	for _, s := range t[i:] {
		parts = append(parts, escapeSegment(s))
	}
	return strings.Join(parts, "/")
}

func escapeSegment(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80 || strings.IndexByte("-._~!$&'()*+,;=:@", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// images returns the vault paths of the images a note shows.
func images(v *vault.Vault, raw []byte, from string) []string {
	var out []string
	for _, r := range markdown.Refs(frontmatter.Split(raw).Body(), vault.Folder(from)) {
		var p string
		switch {
		case r.Wiki && r.Embed:
			if f, err := v.FindFile(r.Target, from); err == nil {
				p = f
			}
		case r.File && v.HasFile(r.Target):
			p = r.Target
		}
		if p != "" && vault.ImageType(p) != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func isFolder(v *vault.Vault, p string) bool {
	fs, err := v.Folders()
	return err == nil && slices.Contains(fs, p)
}

// Save writes the export to the file dest, through a temporary file
// next to it, so a failed export leaves nothing half written.
func (e *Export) Save(dest string) (int64, error) {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".nota-export-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	// CreateTemp makes it private; an export is a file like any other.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return 0, err
	}
	n, err := e.WriteTo(tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	return n, os.Rename(tmp.Name(), dest)
}
