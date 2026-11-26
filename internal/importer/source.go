package importer

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/banjo/nota/internal/vault"
)

// junk are files and folders other systems leave behind.
var junk = []string{"__macosx", "thumbs.db", "desktop.ini"}

// skip reports whether a source path is left out of an import: hidden
// files and folders (.obsidian, .trash, .git, .DS_Store …) and junk.
func skip(name string) bool {
	return strings.HasPrefix(name, ".") || slices.Contains(junk, strings.ToLower(name))
}

// Open reads a source on disk: a folder, a zip, or one Markdown file.
// It returns the files and the source's name, which an import's folder
// is named after.
func Open(p string) ([]File, string, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, "", err
	}
	name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	switch {
	case st.IsDir():
		files, err := FromFS(os.DirFS(p), true)
		return files, filepath.Base(filepath.Clean(p)), err
	case strings.EqualFold(filepath.Ext(p), ".zip"):
		f, err := os.Open(p)
		if err != nil {
			return nil, "", err
		}
		// The files are read during the import: the zip stays open for
		// the life of the process, which is the CLI's or a short one.
		files, inner, err := FromZip(f, st.Size())
		if inner != "" {
			name = inner
		}
		return files, name, err
	case IsNote(p):
		mod := st.ModTime()
		return []File{{
			Path:     filepath.Base(p),
			Read:     func() ([]byte, error) { return os.ReadFile(p) },
			Size:     st.Size(),
			Created:  birth(st),
			Modified: mod,
		}}, name, nil
	}
	return nil, "", fmt.Errorf("%s is not a folder, a zip or a Markdown file", p)
}

// FromFS lists the files of a source folder. times reads their creation
// times too, where the file system keeps them.
func FromFS(fsys fs.FS, times bool) ([]File, error) {
	var files []File
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if skip(d.Name()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		f := File{
			Path:     vault.NFC(p),
			Read:     func() ([]byte, error) { return fs.ReadFile(fsys, p) },
			Size:     info.Size(),
			Modified: info.ModTime(),
		}
		if times {
			f.Created = birth(info)
		}
		files = append(files, f)
		return nil
	})
	files = dropLogseq(files)
	if err == nil && len(files) == 0 {
		err = ErrNoFiles
	}
	return files, err
}

// dropLogseq leaves out a Logseq graph's own folder: its settings, and
// old copies of pages in logseq/bak that would come in as notes.
func dropLogseq(files []File) []File {
	if !slices.ContainsFunc(files, func(f File) bool { return f.Path == "logseq/config.edn" }) {
		return files
	}
	return slices.DeleteFunc(files, func(f File) bool { return strings.HasPrefix(f.Path, "logseq/") })
}

// FromZip lists the files of a zip. A zip whose files are all in one
// folder (most exports) is read from inside it, and that folder's name
// is returned.
func FromZip(r io.ReaderAt, size int64) ([]File, string, error) {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, "", fmt.Errorf("not a zip: %w", err)
	}
	type entry struct {
		f    *zip.File
		name string
	}
	var entries []entry
	for _, f := range z.File {
		name := vault.NFC(strings.TrimPrefix(strings.ReplaceAll(f.Name, `\`, "/"), "/"))
		if f.FileInfo().IsDir() || !f.Mode().IsRegular() {
			continue
		}
		if slices.ContainsFunc(strings.Split(name, "/"), skip) {
			continue
		}
		entries = append(entries, entry{f, name})
	}
	if len(entries) == 0 {
		return nil, "", ErrNoFiles
	}
	root := ""
	if first, _, ok := strings.Cut(entries[0].name, "/"); ok {
		root = first
		for _, e := range entries {
			if !strings.HasPrefix(e.name, root+"/") {
				root = ""
				break
			}
		}
	}
	files := make([]File, 0, len(entries))
	for _, e := range entries {
		zf := e.f
		p := e.name
		if root != "" {
			p = strings.TrimPrefix(p, root+"/")
		}
		files = append(files, File{
			Path:     p,
			Read:     func() ([]byte, error) { return readZip(zf) },
			Size:     int64(zf.UncompressedSize64),
			Modified: zipTime(zf),
		})
	}
	files = dropLogseq(files)
	if len(files) == 0 {
		return nil, "", ErrNoFiles
	}
	return files, root, nil
}

func readZip(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// zipTime is a zip entry's modification time. Without the extended
// timestamp it has no time zone and Go reads it as UTC: it is local.
func zipTime(f *zip.File) time.Time {
	t := f.Modified
	if t.IsZero() || t.Year() < 1981 {
		return time.Time{}
	}
	if t.Location() == time.UTC && f.ModifiedTime != 0 && !hasExtTime(f) {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
	}
	return t
}

// hasExtTime reports whether a zip entry has an extended timestamp
// field (0x5455), which is UTC.
func hasExtTime(f *zip.File) bool {
	b := f.Extra
	for len(b) >= 4 {
		id := uint16(b[0]) | uint16(b[1])<<8
		n := int(uint16(b[2]) | uint16(b[3])<<8)
		if id == 0x5455 {
			return true
		}
		if 4+n > len(b) {
			break
		}
		b = b[4+n:]
	}
	return false
}
