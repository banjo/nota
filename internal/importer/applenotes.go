package importer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/banjo/nota/internal/vault"
)

// AppleNote is one note as Apple Notes' scripting hands it out.
type AppleNote struct {
	ID string `json:"id"`
	// Name is the note's title, its first line.
	Name string `json:"name"`
	// Folder is the note's folder path, its account first when there
	// is more than one: "iCloud/Recipes/Bread".
	Folder string `json:"folder"`
	// Body is the note as HTML.
	Body     string    `json:"body"`
	Created  time.Time `json:"created"`
	Modified time.Time `json:"modified"`
	// Locked notes have no body to read.
	Locked bool `json:"locked"`
	// Attachments counts the note's attachments, images included.
	Attachments int `json:"attachments"`
}

// ErrNotAllowed is returned when macOS does not let Nota read Apple Notes.
var ErrNotAllowed = errors.New("Nota isn't allowed to read Apple Notes: allow it in System Settings › Privacy & Security › Automation, then try again")

// AppleNotesFiles turns notes into files to import: each note becomes
// Markdown at its folder path under its title, with `created` and
// `updated` from Notes, and the images in it become files in
// Attachments/ that the note embeds. A locked note is skipped, and a
// note says how many of its attachments are left behind (Notes' scripting
// only hands out images).
func AppleNotesFiles(notes []AppleNote) []File {
	var files []File
	used := map[string]bool{}
	for _, n := range notes {
		src := path.Join(n.Folder, n.Name)
		name := vault.Filename(n.Name)
		p := path.Join(folderPath(n.Folder), name+vault.Ext)
		if n.Locked {
			files = append(files, File{Path: p, Source: src, Skip: "the note is locked in Apple Notes"})
			continue
		}
		images := 0
		var imgs []File
		md := HTMLToMarkdown(n.Body, func(s string) string {
			data, ext, ok := dataURL(s)
			if !ok {
				return ""
			}
			images++
			var file string
			for i := images; ; i++ {
				file = fmt.Sprintf("%s %d%s", name, i, ext)
				if !used[key(file)] {
					break
				}
			}
			used[key(file)] = true
			imgs = append(imgs, File{
				Path:     path.Join(vault.AttachmentsDir, file),
				Source:   src + " › " + file,
				Read:     func() ([]byte, error) { return data, nil },
				Created:  n.Created,
				Modified: n.Modified,
			})
			return "![[" + file + "]]"
		})
		md = titled(md, n.Name)
		f := File{
			Path:     p,
			Source:   src,
			Read:     func() ([]byte, error) { return []byte(md), nil },
			Created:  n.Created,
			Modified: n.Modified,
			Titled:   true,
		}
		if left := n.Attachments - images; left > 0 {
			f.Detail = fmt.Sprintf("%d %s left in Apple Notes", left, plural(left, "attachment"))
		}
		files = append(files, f)
		files = append(files, imgs...)
	}
	return files
}

// folderPath cleans each name of a Notes folder path.
func folderPath(folder string) string {
	if folder == "" {
		return ""
	}
	segs := strings.Split(folder, "/")
	for i, s := range segs {
		segs[i] = folderName(s)
	}
	return path.Join(segs...)
}

// titled makes the note's first line, its title in Notes, a heading.
// A title that is not in the body (a tag, which Notes leaves out of it)
// is put on top.
func titled(md, name string) string {
	name = strings.TrimSpace(name)
	first, rest, _ := strings.Cut(md, "\n")
	t := strings.TrimSpace(strings.TrimLeft(plain(strings.ReplaceAll(first, "*", "")), "# "))
	switch {
	case name == "":
		return md
	case t == name:
		return "# " + t + "\n" + rest
	case strings.TrimSpace(md) == "":
		return "# " + name + "\n"
	}
	return "# " + name + "\n\n" + md
}

// dataURL decodes an image in a data: URL.
func dataURL(s string) ([]byte, string, bool) {
	rest, ok := strings.CutPrefix(s, "data:")
	if !ok {
		return nil, "", false
	}
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok || !strings.HasSuffix(meta, ";base64") {
		return nil, "", false
	}
	typ := strings.TrimSuffix(meta, ";base64")
	exts, _ := mime.ExtensionsByType(typ)
	ext := ""
	switch typ {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	default:
		if len(exts) > 0 {
			ext = exts[0]
		}
	}
	if ext == "" || vault.ImageType("x"+ext) == "" {
		return nil, "", false
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", false
	}
	return data, ext, true
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
