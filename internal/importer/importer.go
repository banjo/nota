// Package importer brings notes from elsewhere into a vault
// (docs/cli.md, Import): a folder or a zip of Markdown files (Obsidian,
// Bear, Notion, Logseq) or, on macOS, Apple Notes.
//
// An import never replaces anything. Every file goes into one folder,
// under a name the vault can hold: names with characters Nota keeps out
// of file names are cleaned, and a name that is taken gets " 2", " 3" ….
// Links between the imported notes are rewritten where a rename or the
// folder would break them, and dates in frontmatter become Nota's
// `created` and `updated`, from the source's file times when a note has
// none. The Report says what happened to each file.
package importer

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/banjo/nota/internal/vault"
)

// File is one file of a source: a note (Markdown) or any other file.
type File struct {
	// Path is where it is in the source, slash separated.
	Path string
	// Source names it in the report; Path when empty.
	Source string
	// Read returns its content.
	Read func() ([]byte, error)
	// Size is its size in bytes, when known before reading it.
	Size int64
	// Created and Modified are its times, zero when unknown.
	Created, Modified time.Time
	// Skip, when set, leaves the file out for this reason.
	Skip string
	// Detail is a remark for the report, such as what of it is left out.
	Detail string
}

// Options configure an import.
type Options struct {
	// Folder is the vault folder the files go into; "" is the vault root.
	Folder string
	// DryRun plans the import and reports it without writing anything.
	DryRun bool
	// MaxSize skips files larger than this many bytes; 0 is no limit.
	MaxSize int64
}

// Status is what happened to one file.
type Status string

const (
	// Imported files are in the vault under their own name.
	Imported Status = "imported"
	// Renamed files are in the vault under another name; Reason says why.
	Renamed Status = "renamed"
	// Skipped files were left out; Reason says why.
	Skipped Status = "skipped"
)

// Item is one file of the report.
type Item struct {
	// Source is the file in the source.
	Source string `json:"source"`
	// Path is where it is in the vault; empty when skipped.
	Path   string `json:"path,omitempty"`
	Status Status `json:"status"`
	Reason string `json:"reason,omitempty"`
	// Detail is a remark on a file that was imported, such as what of
	// it was left out.
	Detail string `json:"detail,omitempty"`
	// Note is true for a note, false for any other file.
	Note bool `json:"note"`
}

// Report says what an import did, or would do in a dry run.
type Report struct {
	// Folder is the vault folder the files went into; "" for the root.
	Folder string `json:"folder"`
	// Notes and Files count the notes and the other files written.
	Notes int `json:"notes"`
	Files int `json:"files"`
	// Renamed and Skipped count the items with those statuses.
	Renamed int `json:"renamed"`
	Skipped int `json:"skipped"`
	// Links is the number of links rewritten to keep pointing at the
	// imported notes.
	Links  int    `json:"links"`
	DryRun bool   `json:"dryRun"`
	Items  []Item `json:"items"`
}

// Reasons for renames and skips.
const (
	reasonName    = "the name has characters Nota keeps out of file names"
	reasonExt     = "Markdown notes end in .md"
	reasonExists  = "the name was taken"
	reasonIgnored = "the vault ignores this path"
	reasonNotion  = "Notion's page id is left out of the name"
)

// notionID is the page id Notion's export puts at the end of every name.
var notionID = regexp.MustCompile(` [0-9a-f]{32}$`)

// noteExts are the extensions of Markdown notes; all become .md.
var noteExts = []string{".md", ".markdown", ".mdown", ".mkd", ".mkdn"}

// IsNote reports whether a source file is a Markdown note.
func IsNote(name string) bool {
	return slices.Contains(noteExts, strings.ToLower(path.Ext(name)))
}

// plan is one file and where it goes.
type plan struct {
	f    File
	item Item
	// src is the cleaned source path, dest the vault path.
	src, dest string
	note      bool
}

// Run imports files into v. Files are imported in the order given; a
// file that fails to read is skipped and the import goes on. It stops
// at the first write that fails, returning the report so far.
func Run(v *vault.Vault, files []File, opts Options) (*Report, error) {
	folder, err := vault.CleanPath(opts.Folder)
	if err != nil {
		return nil, err
	}
	p := &planner{v: v, taken: map[string]bool{}, folders: map[string]string{}}
	if err := p.loadFolders(); err != nil {
		return nil, err
	}
	folder = p.folder(folder)
	r := &Report{Folder: folder, DryRun: opts.DryRun, Items: []Item{}}

	var plans []*plan
	for _, f := range files {
		pl := p.place(f, folder)
		if opts.MaxSize > 0 && f.Size > opts.MaxSize && pl.item.Status != Skipped {
			pl.item = Item{Source: pl.item.Source, Status: Skipped, Reason: tooLarge(opts.MaxSize), Note: pl.note}
		}
		plans = append(plans, pl)
	}
	links := newLinker(plans)
	for _, pl := range plans {
		if pl.item.Status != Skipped && !opts.DryRun {
			if err := write(v, pl, links, r, opts.MaxSize); err != nil {
				r.add(pl.item)
				return r, err
			}
		}
		if opts.DryRun && pl.item.Status != Skipped {
			if pl.note {
				r.Notes++
			} else {
				r.Files++
			}
		}
		r.add(pl.item)
	}
	return r, nil
}

func (r *Report) add(it Item) {
	switch it.Status {
	case Renamed:
		r.Renamed++
	case Skipped:
		r.Skipped++
	}
	r.Items = append(r.Items, it)
}

// write writes one planned file: a note with its dates in Nota's
// format and its links fixed.
func write(v *vault.Vault, pl *plan, links *linker, r *Report, max int64) error {
	data, err := pl.f.Read()
	if err != nil {
		pl.item = Item{Source: pl.item.Source, Status: Skipped, Reason: "unreadable: " + err.Error(), Note: pl.note}
		return nil
	}
	if max > 0 && int64(len(data)) > max {
		pl.item = Item{Source: pl.item.Source, Status: Skipped, Reason: tooLarge(max), Note: pl.note}
		return nil
	}
	if pl.note {
		var n int
		data, n = links.rewrite(pl, data)
		r.Links += n
		data = dates(data, pl.f.Created, pl.f.Modified)
	}
	// Planned as free; a file that appeared since is not replaced.
	if v.FileExists(pl.dest) {
		pl.item = Item{Source: pl.item.Source, Status: Skipped, Reason: reasonExists, Note: pl.note}
		return nil
	}
	if _, _, err := v.WriteFile(pl.dest, data); err != nil {
		return fmt.Errorf("import %s: %w", pl.item.Source, err)
	}
	if pl.note {
		r.Notes++
	} else {
		r.Files++
	}
	return nil
}

// planner hands out free vault paths.
type planner struct {
	v *vault.Vault
	// taken holds the fold keys of the paths planned so far.
	taken map[string]bool
	// folders maps the fold key of each folder, in the vault or planned,
	// to its name as it is, so a folder of another case is the same one.
	folders map[string]string
}

// key is what two paths collide on in a case-insensitive file system.
func key(p string) string { return strings.ToLower(vault.NFC(p)) }

func (p *planner) loadFolders() error {
	fs, err := p.v.Folders()
	if err != nil {
		return err
	}
	for _, f := range fs {
		p.folders[key(f)] = f
	}
	return nil
}

// folder returns folder as the vault has it, in the case of a folder
// already there, and records it.
func (p *planner) folder(folder string) string {
	if folder == "" {
		return ""
	}
	parent := p.folder(vault.Folder(folder))
	name := path.Join(parent, path.Base(folder))
	if have, ok := p.folders[key(name)]; ok {
		return have
	}
	p.folders[key(name)] = name
	return name
}

// place plans where f goes under folder.
func (p *planner) place(f File, folder string) *plan {
	src := f.Source
	if src == "" {
		src = f.Path
	}
	pl := &plan{f: f, note: IsNote(f.Path), item: Item{Source: src, Note: IsNote(f.Path), Detail: f.Detail}}
	skip := func(why string) *plan {
		pl.item.Status, pl.item.Reason, pl.item.Detail = Skipped, why, ""
		return pl
	}
	if f.Skip != "" {
		return skip(f.Skip)
	}
	clean, err := vault.CleanPath(f.Path)
	if err != nil || clean == "" {
		return skip("not a path inside the source")
	}
	pl.src = clean

	segs := strings.Split(clean, "/")
	reason := ""
	for i, s := range segs[:len(segs)-1] {
		if n := notionID.ReplaceAllString(s, ""); n != s && n != "" {
			s, segs[i], reason = n, n, reasonNotion
		}
		if n := folderName(s); n != s {
			segs[i], reason = n, reasonName
		}
	}
	base := segs[len(segs)-1]
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if n := notionID.ReplaceAllString(stem, ""); n != stem && n != "" {
		stem, reason = n, reasonNotion
	}
	if pl.note {
		if ext != vault.Ext && reason == "" {
			reason = reasonExt
		}
		ext = vault.Ext
	}
	if n := vault.Filename(stem); n != stem {
		stem, reason = n, reasonName
	}
	dir := p.folder(path.Join(append([]string{folder}, segs[:len(segs)-1]...)...))
	dest := path.Join(dir, stem+ext)
	if p.v.Ignored(dest, false) {
		return skip(reasonIgnored)
	}
	free, err := p.free(dest)
	if err != nil {
		return skip(err.Error())
	}
	if free != dest {
		reason = reasonExists
	}
	pl.dest = free
	pl.item.Path = free
	pl.item.Status = Imported
	if reason != "" {
		pl.item.Status, pl.item.Reason = Renamed, reason
	}
	return pl
}

// free returns dest, or dest with " 2", " 3" … before the extension,
// whichever neither the vault nor this import has yet.
func (p *planner) free(dest string) (string, error) {
	ext := path.Ext(dest)
	stem := strings.TrimSuffix(dest, ext)
	for i := 1; ; i++ {
		c := dest
		if i > 1 {
			c = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		if p.taken[key(c)] {
			continue
		}
		got, err := p.v.FreeName(c)
		if err != nil {
			return "", err
		}
		if got == c {
			p.taken[key(c)] = true
			return c, nil
		}
	}
}

// folderName cleans a folder name like a note's (vault.Filename); it
// may not look like a note either.
func folderName(s string) string {
	n := vault.Filename(s)
	if vault.IsNote(n) {
		n = strings.TrimSuffix(n, path.Ext(n)) + " md"
	}
	return n
}

// ErrNoFiles is returned by the sources for a source with nothing to import.
var ErrNoFiles = errors.New("nothing to import")

// DefaultFolder is the folder an import goes into when none is given: one
// named after the source (the folder, the zip, "Apple Notes"), with
// " 2", " 3" … when the vault has that name already, so each import
// lands in a folder of its own.
func DefaultFolder(v *vault.Vault, name string) (string, error) {
	n := folderName(name)
	if path.Ext(n) != "" {
		n = strings.ReplaceAll(n, ".", " ")
	}
	return v.FreeName(n)
}

func tooLarge(max int64) string {
	return fmt.Sprintf("larger than %d MB", max>>20)
}
