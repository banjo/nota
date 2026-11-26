package server

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/banjo/nota/internal/export"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/publish"
)

// ExportRequest is the body of POST …/export: a note's path, a folder's,
// or "" for the vault; the format (html, md, zip); and the look of HTML.
type ExportRequest struct {
	Path   string               `json:"path"`
	Format export.Format        `json:"format"`
	Theme  *protocol.ShareTheme `json:"theme,omitempty"`
}

// apiExport is POST …/export: the export as a download, named in its
// Content-Disposition. It is written to a temporary file under the
// vault's lock, then sent without it.
func (s *Server) apiExport(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req ExportRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if !slices.Contains(export.Formats, req.Format) {
		return badRequest("format must be html, md or zip")
	}
	if req.Theme != nil {
		if err := publish.CheckTheme(*req.Theme); err != nil {
			return badRequest("theme: %v", err)
		}
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "nota-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	ws.mu.Lock()
	e, err := export.New(ws.client, ws.ix, req.Path, export.Options{Format: req.Format, Theme: req.Theme, Name: ws.name})
	if err == nil {
		_, err = e.WriteTo(tmp)
	}
	ws.mu.Unlock()
	switch {
	case errors.Is(err, export.ErrNotFound):
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: err.Error()}}
	case err != nil:
		return fmt.Errorf("export %s: %w", req.Path, err)
	}
	ri := info(r)
	ri.attrs = append(ri.attrs, "format", req.Format, "notes", e.Notes)
	w.Header().Set("Content-Type", e.Type)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": e.Name}))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", time.Time{}, tmp)
	return nil
}
