package index

import (
	"path"
	"slices"
	"strings"

	"github.com/banjo/nota/internal/vault"
)

// FolderInfo is a folder with the number of notes in it, subfolders
// included. Empty folders have 0.
type FolderInfo struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Notes int    `json:"notes"`
}

// Folders lists every folder of v (see vault.Vault.Folders: empty ones
// too), with the note counts from the index, sorted by path. Folders that
// only exist as the parent of a note are included. The caller has
// brought the index up to date.
func (ix *Index) Folders(v *vault.Vault) ([]FolderInfo, error) {
	es, err := ix.Query(Query{})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, e := range es {
		for f := e.Folder; f != ""; f = vault.Folder(f) {
			counts[f]++
		}
	}
	dirs, err := v.Folders()
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		for f := d; f != ""; f = vault.Folder(f) {
			if _, ok := counts[f]; !ok {
				counts[f] = 0
			}
		}
	}
	out := make([]FolderInfo, 0, len(counts))
	for f, n := range counts {
		out = append(out, FolderInfo{Path: f, Name: path.Base(f), Notes: n})
	}
	slices.SortFunc(out, func(a, b FolderInfo) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}
