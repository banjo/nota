package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/banjo/nota/internal/importer"
)

// apiImport is POST …/import: a zip of notes (a folder the web app
// zipped, or an export of another app) imported into the vault
// (importer.Run). ?name= names the folder it goes into by default (the
// zip's or the folder's name), ?folder= picks the folder ("" or "/" for
// the top), and ?dryRun=1 only plans it. The answer is the report.
func (s *Server) apiImport(w http.ResponseWriter, r *http.Request, p Principal) error {
	q := r.URL.Query()
	limit := 10 * s.cfg.MaxFileSize
	if r.ContentLength > limit {
		return ErrTooLarge
	}
	tmp, err := os.CreateTemp("", "nota-import-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, io.LimitReader(r.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return ErrTooLarge
	}
	files, root, err := importer.FromZip(tmp, n)
	switch {
	case errors.Is(err, importer.ErrNoFiles):
		return badRequest("the zip has nothing to import")
	case err != nil:
		return badRequest("%v", err)
	}
	name := strings.TrimSuffix(q.Get("name"), ".zip")
	if name == "" {
		name = root
	}
	if name == "" {
		name = "Import"
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
	folder := q.Get("folder")
	if !q.Has("folder") {
		if folder, err = importer.DefaultFolder(ed, name); err != nil {
			return vaultErr(err)
		}
	}
	rep, err := importer.Run(ed, files, importer.Options{
		Folder:  folder,
		DryRun:  q.Get("dryRun") == "1" || q.Get("dryRun") == "true",
		MaxSize: s.cfg.MaxFileSize,
	})
	if err != nil {
		return vaultErr(err)
	}
	info(r).attrs = append(info(r).attrs, "notes", rep.Notes, "files", rep.Files, "skipped", rep.Skipped)
	writeJSON(w, http.StatusOK, rep)
	return nil
}
