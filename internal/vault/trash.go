package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/banjo/nota/internal/events"
)

// TrashItem is a file in the vault's .trash/.
type TrashItem struct {
	// Path is where it is now, ".trash/…".
	Path string `json:"path"`
	// From is the vault path it was deleted from.
	From string `json:"from"`
	// DeletedAt is when it was deleted; the file's modification time for
	// files put in the trash by hand or by an older Nota.
	DeletedAt time.Time `json:"deletedAt"`
	Size      int64     `json:"size"`
}

// trashLog is where deletes are recorded: local state, never synced.
const trashLog = MetaDirName + "/trash.json"

type trashRecord struct {
	From      string    `json:"from"`
	DeletedAt time.Time `json:"deletedAt"`
}

func (v *Vault) readTrashLog() map[string]trashRecord {
	recs := map[string]trashRecord{}
	if data, err := v.fs.ReadFile(trashLog); err == nil {
		_ = json.Unmarshal(data, &recs) // a broken log only loses the dates
	}
	return recs
}

// writeTrashLog keeps the records of files still in the trash.
func (v *Vault) writeTrashLog(recs map[string]trashRecord) error {
	for p := range recs {
		if r, err := v.resolve(p); err != nil || !r.exists {
			delete(recs, p)
		}
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	if err := v.fs.MkdirAll(MetaDirName); err != nil {
		return wrap(err)
	}
	return wrap(v.fs.WriteFile(trashLog, append(data, '\n')))
}

// recordTrash notes where a file in the trash came from and when.
func (v *Vault) recordTrash(trash, from string) {
	recs := v.readTrashLog()
	recs[trash] = trashRecord{From: from, DeletedAt: v.now().UTC()}
	// The note is in the trash either way; without the record it lists
	// with its modification time and the path it has in the trash.
	_ = v.writeTrashLog(recs)
}

// Trash lists the files in .trash/, most recently deleted first.
func (v *Vault) Trash() ([]TrashItem, error) {
	recs := v.readTrashLog()
	var out []TrashItem
	err := fs.WalkDir(v.fs, TrashDir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && name == TrashDir {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		p := NFC(name)
		it := TrashItem{Path: p, From: strings.TrimPrefix(p, TrashDir+"/"), DeletedAt: info.ModTime().UTC(), Size: info.Size()}
		if r, ok := recs[p]; ok {
			it.From, it.DeletedAt = r.From, r.DeletedAt
		}
		out = append(out, it)
		return nil
	})
	if err != nil {
		return nil, wrap(err)
	}
	slices.SortStableFunc(out, func(a, b TrashItem) int {
		if c := b.DeletedAt.Compare(a.DeletedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out, nil
}

// FindTrash finds a file in the trash by its path there (with or without
// ".trash/") or by the path it was deleted from, the newest delete first.
func (v *Vault) FindTrash(p string) (TrashItem, error) {
	c, err := CleanPath(p)
	if err != nil {
		return TrashItem{}, err
	}
	items, err := v.Trash()
	if err != nil {
		return TrashItem{}, err
	}
	in := strings.TrimPrefix(c, TrashDir+"/")
	candidates := []string{in}
	if !IsNote(in) && path.Ext(in) == "" {
		candidates = append(candidates, in+Ext)
	}
	for _, want := range candidates {
		for _, it := range items {
			if it.Path == TrashDir+"/"+want {
				return it, nil
			}
		}
	}
	for _, want := range candidates {
		for _, it := range items {
			if it.From == want {
				return it, nil
			}
		}
	}
	return TrashItem{}, &PathError{Op: "trash", Path: p, Err: ErrNotFound}
}

// trashPath checks that p names a file in the trash and returns its
// resolved disk name.
func (v *Vault) trashPath(op, p string) (string, string, error) {
	c, err := CleanPath(p)
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(c, TrashDir+"/") {
		return "", "", &PathError{Op: op, Path: p, Err: fmt.Errorf("%w: not in %s/", ErrInvalidPath, TrashDir)}
	}
	r, err := v.resolve(c)
	if err != nil {
		return "", "", err
	}
	if !r.exists {
		return "", "", &PathError{Op: op, Path: c, Err: ErrNotFound}
	}
	return c, r.disk, nil
}

// ReadTrash reads a file in the trash.
func (v *Vault) ReadTrash(p string) ([]byte, error) {
	_, disk, err := v.trashPath("read", p)
	if err != nil {
		return nil, err
	}
	data, err := v.fs.ReadFile(disk)
	return data, wrap(err)
}

// RestoreTrash moves a file out of the trash to the vault path to, or
// back where it was deleted from when to is empty, and returns where it
// went. It refuses to overwrite a file that has taken the path since.
func (v *Vault) RestoreTrash(p, to string) (string, error) {
	c, disk, err := v.trashPath("restore", p)
	if err != nil {
		return "", err
	}
	recs := v.readTrashLog()
	if to == "" {
		to = strings.TrimPrefix(c, TrashDir+"/")
		if r, ok := recs[c]; ok {
			to = r.From
		}
	}
	if to, err = filePath(to); err != nil {
		return "", err
	}
	if err := v.checkWritable("restore", to); err != nil {
		return "", err
	}
	dst, err := v.resolve(to)
	if err != nil {
		return "", err
	}
	if dst.exists {
		return "", &PathError{Op: "restore", Path: to, Existing: to, Err: ErrExists}
	}
	if dst.collision != "" {
		return "", &PathError{Op: "restore", Path: to, Existing: dst.collision, Err: ErrCaseCollision}
	}
	dir, err := v.mkdirs(Folder(to))
	if err != nil {
		return "", err
	}
	if err := v.fs.Rename(disk, fsJoin(dir, path.Base(to))); err != nil {
		return "", wrap(err)
	}
	v.pruneTrashDirs(path.Dir(c))
	delete(recs, c)
	_ = v.writeTrashLog(recs)
	if IsNote(to) {
		v.emit(events.Event{Type: events.NoteCreated, Path: to}, true)
	}
	return to, nil
}

// PurgeTrash deletes a file in the trash for good.
func (v *Vault) PurgeTrash(p string) error {
	c, disk, err := v.trashPath("purge", p)
	if err != nil {
		return err
	}
	if err := v.fs.Remove(disk); err != nil {
		return wrap(err)
	}
	v.pruneTrashDirs(path.Dir(c))
	recs := v.readTrashLog()
	delete(recs, c)
	return v.writeTrashLog(recs)
}

// EmptyTrash deletes for good every file in the trash deleted before
// cutoff (all of them for the zero time) and returns how many it deleted.
func (v *Vault) EmptyTrash(cutoff time.Time) (int, error) {
	items, err := v.Trash()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range items {
		if !cutoff.IsZero() && !it.DeletedAt.Before(cutoff) {
			continue
		}
		r, err := v.resolve(it.Path)
		if err != nil {
			return n, err
		}
		if err := v.fs.Remove(r.disk); err != nil {
			return n, wrap(err)
		}
		v.pruneTrashDirs(path.Dir(it.Path))
		n++
	}
	return n, v.writeTrashLog(v.readTrashLog())
}

// pruneTrashDirs removes the empty folders from dir up to .trash/ itself.
func (v *Vault) pruneTrashDirs(dir string) {
	for dir != TrashDir && strings.HasPrefix(dir, TrashDir+"/") {
		r, err := v.resolve(dir)
		if err != nil || !r.exists {
			return
		}
		entries, err := v.fs.ReadDir(r.disk)
		if err != nil || len(entries) > 0 {
			return
		}
		if v.fs.Remove(r.disk) != nil {
			return
		}
		dir = path.Dir(dir)
	}
}
