package vault

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/banjo/nota/internal/icons"
)

// FolderIconsFile holds the icons of the vault's folders, by path:
//
//	Work/VGCS:
//	  icon: lucide:briefcase
//	  color: blue
//	Personal:
//	  icon: 🏠
//
// It syncs with the vault, so every device shows the same icons.
const FolderIconsFile = MetaDirName + "/folders.yaml"

// FolderIcon is how a folder looks in the sidebar: an icon (package
// icons names them) and its colour, either of which may be empty.
type FolderIcon struct {
	Icon  string `yaml:"icon,omitempty" json:"icon,omitempty"`
	Color string `yaml:"color,omitempty" json:"color,omitempty"`
}

// FolderIcons returns the folders' icons by folder path. A missing file
// is no icons. An entry may name a folder that is gone: the tree only
// shows the ones for folders it has.
func (v *Vault) FolderIcons() (map[string]FolderIcon, error) {
	data, err := v.ReadFile(FolderIconsFile)
	if errors.Is(err, ErrNotFound) {
		return map[string]FolderIcon{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]FolderIcon{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", FolderIconsFile, err)
	}
	clean := make(map[string]FolderIcon, len(out))
	for p, fi := range out {
		if c, err := CleanPath(p); err == nil && c != "" && (fi.Icon != "" || fi.Color != "") {
			clean[c] = fi
		}
	}
	return clean, nil
}

// SetFolderIcon gives a folder an icon and a colour; both empty removes
// them. The icon is checked against the icons Nota has, and stored
// normalized ("house" becomes "lucide:house"). A colour needs no icon:
// it colours the folder's own icon. It fails with ErrNotFound when there
// is no such folder.
func (v *Vault) SetFolderIcon(folder string, fi FolderIcon) (string, error) {
	folder, err := CleanPath(folder)
	if err != nil {
		return "", err
	}
	if folder == "" {
		return "", &PathError{Op: "icon", Path: folder, Err: fmt.Errorf("%w: the vault root has no icon", ErrInvalidPath)}
	}
	fi.Icon = icons.Normalize(fi.Icon)
	fi.Color = strings.TrimSpace(fi.Color)
	if fi.Icon != "" {
		if err := icons.Check(fi.Icon); err != nil {
			return "", &PathError{Op: "icon", Path: folder, Err: Invalid(err)}
		}
	}
	if err := icons.CheckColor(fi.Color); err != nil {
		return "", &PathError{Op: "icon", Path: folder, Err: Invalid(err)}
	}
	dirs, err := v.Folders()
	if err != nil {
		return "", err
	}
	if !slices.Contains(dirs, folder) {
		return "", &PathError{Op: "icon", Path: folder, Err: ErrNotFound}
	}
	all, err := v.FolderIcons()
	if err != nil {
		return "", err
	}
	if fi == (FolderIcon{}) {
		if _, ok := all[folder]; !ok {
			return folder, nil
		}
		delete(all, folder)
	} else {
		all[folder] = fi
	}
	return folder, v.writeFolderIcons(all)
}

// moveFolderIcons moves the icons of from and the folders in it to to,
// after RenameFolder. A folder whose icon is moved keeps nothing behind.
func (v *Vault) moveFolderIcons(from, to string) error {
	all, err := v.FolderIcons()
	if err != nil {
		return err
	}
	moved := false
	for p, fi := range all {
		if p == from || strings.HasPrefix(p, from+"/") {
			delete(all, p)
			all[to+strings.TrimPrefix(p, from)] = fi
			moved = true
		}
	}
	if !moved {
		return nil
	}
	return v.writeFolderIcons(all)
}

// writeFolderIcons writes the file sorted by path.
func (v *Vault) writeFolderIcons(all map[string]FolderIcon) error {
	ms := yaml.MapSlice{}
	for _, p := range slices.Sorted(maps.Keys(all)) {
		ms = append(ms, yaml.MapItem{Key: p, Value: all[p]})
	}
	data, err := yaml.Marshal(ms)
	if err != nil {
		return err
	}
	_, _, err = v.WriteFile(FolderIconsFile, data)
	return err
}

// Invalid marks err as a value Nota doesn't take, such as an icon it
// doesn't have: it is ErrInvalidPath to errors.Is, so it is reported as
// a usage error, but keeps its own message.
func Invalid(err error) error { return invalid{err} }

type invalid struct{ err error }

func (e invalid) Error() string        { return e.err.Error() }
func (e invalid) Unwrap() error        { return e.err }
func (e invalid) Is(target error) bool { return target == ErrInvalidPath }
