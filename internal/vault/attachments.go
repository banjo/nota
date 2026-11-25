package vault

import (
	"errors"
	"path"
	"strings"
)

// Attachments are the vault's other files that notes show: images, by
// ![](path) or ![[name]]. Pasted and dropped files all go to one folder
// (docs/markdown.md); a note links them by path, so they can live
// anywhere else too.

// AttachmentsDir is the folder pasted and dropped files are saved in.
const AttachmentsDir = "Attachments"

// ErrNotImage is returned for an attachment that is not an image type
// Nota shows.
var ErrNotImage = errors.New("not an image Nota can show")

var imageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
}

// ImageType returns the content type of an image file Nota shows, by its
// extension, or "" for any other file.
func ImageType(name string) string { return imageTypes[strings.ToLower(path.Ext(name))] }

// SaveAttachment writes an image to AttachmentsDir under its own name,
// or with " 2", " 3" … when that is taken, and returns its vault path.
// Only the base of name is used.
func (v *Vault) SaveAttachment(name string, data []byte) (string, error) {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	if base == "." || base == "/" || strings.HasPrefix(base, ".") {
		return "", &PathError{Op: "attach", Path: name, Err: ErrInvalidPath}
	}
	if ImageType(base) == "" {
		return "", &PathError{Op: "attach", Path: name, Err: ErrNotImage}
	}
	// An "attachments" folder of another case (Obsidian's) is the same one.
	dir := AttachmentsDir
	if r, err := v.resolve(dir); err == nil && !r.exists && r.collision != "" {
		dir = r.collision
	}
	p, err := v.FreeName(dir + "/" + base)
	if err != nil {
		return "", err
	}
	p, _, err = v.WriteFile(p, data)
	return p, err
}

// HasFile reports whether a file a note may show is at exactly rel: it
// exists, and is not ignored or Nota's own.
func (v *Vault) HasFile(rel string) bool {
	c, err := CleanPath(rel)
	if err != nil || c == "" || v.Ignored(c, false) || c == MetaDirName || strings.HasPrefix(c, MetaDirName+"/") {
		return false
	}
	for f := Folder(c); f != ""; f = Folder(f) {
		if v.Ignored(f, true) {
			return false
		}
	}
	r, err := v.resolve(c)
	if err != nil || !r.exists {
		return false
	}
	info, err := v.fs.Stat(r.disk)
	return err == nil && info.Mode().IsRegular()
}

// FindFile resolves an embed's target (![[name]]) from the note at from,
// as Obsidian does: a vault path, then the note's folder, then
// AttachmentsDir, then any file whose path ends in the target, the
// shortest such path first. It fails with ErrNotFound.
func (v *Vault) FindFile(target, from string) (string, error) {
	t := strings.TrimPrefix(strings.ReplaceAll(target, `\`, "/"), "/")
	cands := []string{t}
	if f := Folder(from); f != "" {
		cands = append(cands, f+"/"+t)
	}
	cands = append(cands, AttachmentsDir+"/"+t)
	for _, c := range cands {
		if cp, err := CleanPath(c); err == nil && v.HasFile(cp) {
			return cp, nil
		}
	}
	want, err := CleanPath(t)
	if err != nil || want == "" {
		return "", &PathError{Op: "find", Path: target, Err: ErrNotFound}
	}
	key := foldKey(want)
	best := ""
	_ = v.WalkFiles(func(e Entry) error {
		k := foldKey(e.Path)
		if k != key && !strings.HasSuffix(k, "/"+key) {
			return nil
		}
		if best == "" || len(e.Path) < len(best) || (len(e.Path) == len(best) && e.Path < best) {
			best = e.Path
		}
		return nil
	})
	if best == "" {
		return "", &PathError{Op: "find", Path: target, Err: ErrNotFound}
	}
	return best, nil
}
