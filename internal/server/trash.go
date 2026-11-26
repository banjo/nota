package server

import (
	"context"
	"net/http"
	"path"
	"time"

	"github.com/banjo/nota/internal/protocol"
	nvault "github.com/banjo/nota/internal/vault"
)

// The web app's trash. The working copy keeps no .trash/: the version
// log is the trash. A deleted note is a path whose newest version is a
// delete that was not half of a move, and restoring it writes its last
// content back as a new version.

// TrashItem is a deleted note. ID is what the other trash calls take
// (its path: the server has one deleted note per path).
type TrashItem struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	DeletedAt time.Time `json:"deletedAt"`
	Size      int64     `json:"size"`
	// Version is the last version with content, the one a restore brings back.
	Version int64 `json:"version"`
}

// trashLimit caps the list: the most recent deletes are what matter.
const trashLimit = 500

func deletedNotes(ctx context.Context, q querier, vaultID int64) ([]protocol.Entry, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+entryCols+` FROM files f
		WHERE f.vault_id = ? AND f.deleted = 1
		AND NOT EXISTS (SELECT 1 FROM versions m WHERE m.vault_id = f.vault_id
			AND m.version = f.version + 1 AND m.moved_from = f.path)
		ORDER BY f.version DESC`, vaultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Entry
	for rows.Next() && len(out) < trashLimit {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		if nvault.IsNote(e.Path) && workPath(e.Path) {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// lastContent is the newest version of a deleted path that has content.
func lastContent(ctx context.Context, q querier, vaultID int64, p string) (protocol.Entry, error) {
	vs, err := history(ctx, q, vaultID, p)
	if err != nil {
		return protocol.Entry{}, err
	}
	if len(vs) == 0 || !vs[0].Deleted {
		return protocol.Entry{}, &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: p + " is not in the trash"}}
	}
	for _, e := range vs {
		if !e.Deleted {
			return e, nil
		}
	}
	return protocol.Entry{}, &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: p + " has no content to restore"}}
}

// apiTrash lists the deleted notes, most recently deleted first
// (GET …/trash).
func (s *Server) apiTrash(w http.ResponseWriter, r *http.Request, p Principal) error {
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ctx := r.Context()
	dels, err := deletedNotes(ctx, s.store.db, ws.id)
	if err != nil {
		return err
	}
	items := make([]TrashItem, 0, len(dels))
	for _, d := range dels {
		last, err := lastContent(ctx, s.store.db, ws.id, d.Path)
		if err != nil {
			continue // deleted before it had content: nothing to restore
		}
		items = append(items, TrashItem{ID: d.Path, Path: d.Path, DeletedAt: d.ModifiedAt, Size: last.Size, Version: last.Version})
	}
	info(r).attrs = append(info(r).attrs, "items", len(items))
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// apiTrashNote returns a deleted note's last content (GET …/trash/{path}).
func (s *Server) apiTrashNote(w http.ResponseWriter, r *http.Request, p Principal) error {
	np, err := notePath(r)
	if err != nil {
		return err
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	last, err := lastContent(r.Context(), s.store.db, ws.id, np)
	if err != nil {
		return err
	}
	data, err := s.readBlob(r.Context(), last.Hash)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": np, "version": last.Version, "content": string(data)})
	return nil
}

// TrashRestoreRequest is the body of POST …/trash/restore: the deleted
// note, and the folder to restore it into ("" for where it was).
type TrashRestoreRequest struct {
	ID     string `json:"id"`
	Folder string `json:"folder,omitempty"`
}

// apiTrashRestore writes a deleted note's last content back as a new
// version, where it was or in another folder (POST …/trash/restore).
func (s *Server) apiTrashRestore(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req TrashRestoreRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	np, err := nvault.NotePath(req.ID)
	if err != nil {
		return vaultErr(err)
	}
	to := np
	if req.Folder != "" {
		folder, err := nvault.CleanPath(req.Folder)
		if err != nil {
			return vaultErr(err)
		}
		to = path.Join(folder, path.Base(np))
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ctx := r.Context()
	last, err := lastContent(ctx, s.store.db, ws.id, np)
	if err != nil {
		return err
	}
	if _, live, err := ws.current(ctx, to); err != nil {
		return err
	} else if live {
		return &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeExists, Message: to + " already exists", Existing: to}}
	}
	for _, q := range []string{np, to} {
		if other, err := collision(ctx, s.store.db, ws.id, q, ""); err != nil {
			return err
		} else if other != "" {
			return &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeCaseCollision, Message: q + " differs only in case from " + other, Existing: other}}
		}
	}
	data, err := s.readBlob(ctx, last.Hash)
	if err != nil {
		return err
	}
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	// Elsewhere is back where it was and then a move, so the delete is
	// no longer the path's newest version and history follows the note.
	if _, _, err := ed.WriteFile(np, data); err != nil {
		return vaultErr(err)
	}
	if to != np {
		if _, err := ed.Move(np, path.Dir(to)); err != nil {
			return vaultErr(err)
		}
	}
	info(r).attrs = append(info(r).attrs, "file", to, "restored", last.Version)
	writeJSON(w, http.StatusOK, map[string]string{"path": to})
	return nil
}
