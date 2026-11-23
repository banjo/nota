package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// Managing vaults from the web app: rename and delete (create and list
// are the sync protocol's, mounted under /v1/api too). Both take a
// browser session only, so a device token (a synced laptop, an agent on
// /mcp) can never rename or delete a vault.

// RenameRequest is the body of PATCH /v1/api/vaults/{vault}.
type RenameRequest struct {
	Name string `json:"name"`
}

// DeleteVaultRequest is the body of DELETE /v1/api/vaults/{vault}:
// Confirm must be the vault's current name, as the user typed it.
type DeleteVaultRequest struct {
	Confirm string `json:"confirm"`
}

func decodeBody(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v); err != nil {
		return badRequest("bad JSON: %v", err)
	}
	return nil
}

func (s *Server) renameVault(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req RenameRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	v, err := s.store.RenameVault(r.Context(), p.UserID, r.PathValue("vault"), req.Name)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "to", v.Name)
	writeJSON(w, http.StatusOK, v)
	return nil
}

func (s *Server) deleteVault(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req DeleteVaultRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	rm, err := s.RemoveVault(r.Context(), p.UserID, r.PathValue("vault"), func(v vault) error {
		if req.Confirm != v.Name {
			return badRequest("type the vault's name, %s, to delete it", v.Name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "files", rm.Files, "blobs", rm.Blobs)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// RemoveVault deletes a user's vault (DeleteVault), closes its working
// copy and removes what it left on disk. check, when set, runs on the
// vault inside the deleting transaction and can refuse.
func (s *Server) RemoveVault(ctx context.Context, userID int64, name string, check func(vault) error) (VaultRemoval, error) {
	rm, err := s.store.deleteVault(ctx, userID, name, check)
	if err != nil {
		return rm, err
	}
	s.wsMu.Lock()
	ws := s.workspaces[rm.Vault.ID]
	delete(s.workspaces, rm.Vault.ID)
	s.wsMu.Unlock()
	if ws != nil {
		ws.close() // waits for a write in flight (ws.mu)
	}
	if _, _, err := RemoveVaultData(ctx, s.store, s.cfg.DataDir, rm); err != nil {
		// The vault is gone from the database, which is the truth; a
		// leftover directory is reconciled away if the id is reused and
		// "nota serve blobs gc" sweeps leftover blobs.
		s.log.Warn("delete vault: disk cleanup", "vault_id", rm.Vault.ID, "err", err)
	}
	return rm, nil
}
