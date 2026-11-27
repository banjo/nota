package server

import (
	"net/http"
	"strconv"

	"github.com/banjo/nota/internal/icons"
	nvault "github.com/banjo/nota/internal/vault"
	"github.com/banjo/nota/internal/views"
)

// IconRequest is the body of PUT …/folders/icon and …/views/{id}/icon:
// an icon and a colour, both empty to take them away.
type IconRequest struct {
	Path  string `json:"path,omitempty"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// apiFolderIcon gives a folder an icon (.nota/folders.yaml), as a
// version of the calling device.
func (s *Server) apiFolderIcon(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req IconRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	folder, err := ed.SetFolderIcon(req.Path, nvault.FolderIcon{Icon: req.Icon, Color: req.Color})
	if err != nil {
		return vaultErr(err)
	}
	all, err := ed.FolderIcons()
	if err != nil {
		return err
	}
	fi := all[folder]
	info(r).attrs = append(info(r).attrs, "folder", folder, "icon", fi.Icon)
	writeJSON(w, http.StatusOK, icons.LookOf(fi.Icon, fi.Color))
	return nil
}

// apiViewIcon writes a saved view's icon into its file.
func (s *Server) apiViewIcon(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req IconRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	vw, err := views.SetIcon(ed, r.PathValue("id"), req.Icon, req.Color)
	if err != nil {
		return vaultErr(err)
	}
	writeJSON(w, http.StatusOK, icons.LookOf(vw.Icon, vw.Color))
	return nil
}

// apiIcons is GET …/icons?q=&set=&limit=: icons.Search.
func (s *Server) apiIcons(w http.ResponseWriter, r *http.Request, p Principal) error {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	set := q.Get("set")
	if set != "" && !validSet(set) {
		return badRequest("no icon set %q", set)
	}
	found, err := icons.Search(q.Get("q"), set, min(limit, 500))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{"icons": found})
	return nil
}

func validSet(set string) bool {
	for _, s := range icons.Sets {
		if s.Prefix == set {
			return true
		}
	}
	return false
}
