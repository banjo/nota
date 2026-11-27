// Package attachments lists the files in a vault's attachments folder
// (vault.AttachmentsFolder), where pasted and dropped images go, with
// the notes that show or link to each one. The app shows them on a page
// of their own instead of as a folder of notes.
package attachments

import (
	"path"
	"slices"
	"strings"
	"time"

	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
)

// File is one file in the attachments folder.
type File struct {
	// Path is its vault path; Name its file name.
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// Image is true for a type Nota shows (vault.ImageType).
	Image bool `json:"image"`
	// UsedBy are the notes that embed or link to it, by path.
	UsedBy []string `json:"usedBy"`
}

// List is the attachments folder and its files, newest first.
type List struct {
	// Folder is the folder as it is in the vault; "" when there is none.
	Folder string `json:"folder"`
	Files  []File `json:"files"`
}

// Folder returns the vault's attachments folder, "" when it has none.
func Folder(v *vault.Vault) string {
	if dir, ok := v.AttachmentsFolder(); ok {
		return dir
	}
	return ""
}

// Load lists the files in the attachments folder and its subfolders,
// notes left out, with the notes that use each one. A link uses a file
// when it resolves to it: a wikilink or embed the way the app finds an
// embed's file (vault.FindFile), a Markdown link by its path.
func Load(v *vault.Vault, ix *index.Index) (*List, error) {
	out := &List{Files: []File{}}
	dir := Folder(v)
	if dir == "" {
		return out, nil
	}
	out.Folder = dir
	byName := map[string][]int{}
	err := v.WalkFiles(func(e vault.Entry) error {
		if !strings.HasPrefix(e.Path, dir+"/") || vault.IsNote(e.Path) {
			return nil
		}
		name := path.Base(e.Path)
		byName[strings.ToLower(name)] = append(byName[strings.ToLower(name)], len(out.Files))
		out.Files = append(out.Files, File{
			Path:     e.Path,
			Name:     name,
			Size:     e.Size,
			Modified: e.ModTime,
			Image:    vault.ImageType(name) != "",
			UsedBy:   []string{},
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out.Files) > 0 {
		if err := usedBy(v, ix, out.Files, byName); err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(out.Files, func(a, b File) int {
		if c := b.Modified.Compare(a.Modified); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out, nil
}

// usedBy fills in the notes that use each file: the wikilinks and embeds
// whose target ends in one of the files' names, resolved from their
// note, and the markdown links to the file's path.
func usedBy(v *vault.Vault, ix *index.Index, files []File, byName map[string][]int) error {
	type link struct {
		from, target string
		wiki         bool
	}
	var links []link
	for _, q := range []struct {
		sql  string
		wiki bool
	}{
		{`SELECT n.path, l.target FROM links l JOIN notes n ON n.id = l.note_id WHERE l.wiki`, true},
		{`SELECT n.path, f.path FROM files f JOIN notes n ON n.id = f.note_id`, false},
	} {
		rows, err := ix.DB().Query(q.sql)
		if err != nil {
			return err
		}
		for rows.Next() {
			l := link{wiki: q.wiki}
			if err := rows.Scan(&l.from, &l.target); err != nil {
				rows.Close()
				return err
			}
			base := strings.ToLower(path.Base(strings.ReplaceAll(l.target, `\`, "/")))
			if _, ok := byName[base]; ok {
				links = append(links, l)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	at := map[string]int{}
	for i, f := range files {
		at[strings.ToLower(f.Path)] = i
	}
	// FindFile can walk the vault, so each target is resolved once per
	// folder it is linked from.
	found := map[[2]string]string{}
	for _, l := range links {
		to := l.target
		if l.wiki {
			k := [2]string{l.target, vault.Folder(l.from)}
			var ok bool
			if to, ok = found[k]; !ok {
				to, _ = v.FindFile(l.target, l.from)
				found[k] = to
			}
		}
		i, ok := at[strings.ToLower(to)]
		if !ok || slices.Contains(files[i].UsedBy, l.from) {
			continue
		}
		files[i].UsedBy = append(files[i].UsedBy, l.from)
	}
	for i := range files {
		slices.Sort(files[i].UsedBy)
	}
	return nil
}
