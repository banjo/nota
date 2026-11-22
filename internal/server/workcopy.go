package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/hooks"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/protocol"
	nvault "github.com/banjo/nota/internal/vault"
)

// The server keeps a working copy of every vault it serves: the current
// version of each file materialized under <data>/vaults/<vault id>/, with
// a vault index (internal/index) in its .nota/ directory. The version log
// stays the truth: the working copy follows it, and is rebuilt from it
// when it is missing or was not closed cleanly.
//
// Every change to a vault goes through its workspace's mutex: sync
// writes from clients (committed to the log, then materialized through
// the "client" vault, whose events run the index and the server hooks)
// and edits made on the server (web API, HTTP MCP, hooks), which go
// through a vault on a commitFS: each file write is committed to the log
// as a new version first and then written to disk, so every client sees
// it on its next pull.

// DeviceServerHook is the device of versions written by server hooks.
const DeviceServerHook = "server-hook"

// OriginClient is the event origin of changes materialized from clients.
// Transform hooks run on them (unlike events.OriginSync on devices),
// which is how notes captured on a phone get filed by the server.
const OriginClient = "client"

// stateFile marks a working copy that was closed cleanly at a known
// cursor, so the next start can skip the full comparison with the log.
const stateFile = nvault.MetaDirName + "/server-state.json"

// workPath reports whether the working copy holds path p. Sync
// clients never send Nota's own state, but the server does not trust
// them: anything in .nota/ except the shared configuration (hooks,
// views, plugins) and anything in .trash/ stays out.
func workPath(p string) bool {
	if p == nvault.TrashDir || strings.HasPrefix(p, nvault.TrashDir+"/") || p == nvault.MetaDirName {
		return false
	}
	if rest, ok := strings.CutPrefix(p, nvault.MetaDirName+"/"); ok {
		return rest == "hooks.toml" || strings.HasPrefix(rest, "views/") || strings.HasPrefix(rest, "plugins/")
	}
	return true
}

// workspace is the working copy of one vault.
type workspace struct {
	mu      sync.Mutex // held for every change and every index read
	srv     *Server
	userID  int64
	id      int64
	name    string
	created time.Time
	dir     string
	log     *slog.Logger

	bus    *events.Bus
	client *nvault.Vault // materializes client changes (origin "client")
	ix     *index.Index
	hooks  *hooks.Runner
	detach func()
}

// workspace returns the open working copy of a user's vault, opening
// (and if needed rebuilding) it on first use. Another user's vault is
// ErrNotFound.
func (s *Server) workspace(ctx context.Context, userID int64, name string) (*workspace, error) {
	v, err := vaultOf(ctx, s.store.db, userID, name)
	if err != nil {
		return nil, err
	}
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if ws := s.workspaces[v.ID]; ws != nil {
		if ws.userID == v.UserID && ws.name == v.Name && ws.created.Equal(v.CreatedAt) {
			return ws, nil
		}
		// The vault was deleted (by the admin, from another process) and
		// its id reused: drop the stale copy; reconcile cleans the disk.
		ws.close()
		_ = os.Remove(filepath.Join(ws.dir, filepath.FromSlash(stateFile)))
		delete(s.workspaces, v.ID)
	}
	ws, err := s.openWorkspace(ctx, v)
	if err != nil {
		return nil, fmt.Errorf("server: working copy of vault %s: %w", name, err)
	}
	s.workspaces[v.ID] = ws
	return ws, nil
}

func (s *Server) openWorkspace(ctx context.Context, v vault) (*workspace, error) {
	dir := filepath.Join(s.cfg.DataDir, "vaults", fmt.Sprint(v.ID))
	if err := os.MkdirAll(filepath.Join(dir, nvault.MetaDirName), 0o700); err != nil {
		return nil, err
	}
	ws := &workspace{srv: s, userID: v.UserID, id: v.ID, name: v.Name, created: v.CreatedAt, dir: dir, bus: events.New(),
		log: s.log.With("vault", v.Name, "vault_id", v.ID, "user_id", v.UserID)}
	cv, err := nvault.Open(dir, nvault.Options{Bus: ws.bus, Now: s.cfg.Now})
	if err != nil {
		return nil, err
	}
	ws.client = cv.WithOrigin(OriginClient)
	if err := ws.reconcile(ctx); err != nil {
		return nil, err
	}
	if ws.ix, err = index.OpenVault(ws.client); err != nil {
		return nil, err
	}
	if _, err := ws.ix.Sync(ws.client); err != nil {
		ws.ix.Close()
		return nil, err
	}
	ws.bus.Subscribe(ws.onEvent)
	ws.loadHooks()
	return ws, nil
}

// close writes the clean-shutdown marker and releases the index.
func (ws *workspace) close() {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.hooks != nil {
		ws.hooks.Wait()
	}
	ws.bus.Close()
	if ws.ix != nil {
		ws.ix.Close()
	}
	var seq int64
	if err := ws.srv.store.db.QueryRow(`SELECT seq FROM vaults WHERE id = ?`, ws.id).Scan(&seq); err == nil {
		data, _ := json.Marshal(map[string]int64{"cursor": seq})
		_ = os.WriteFile(filepath.Join(ws.dir, filepath.FromSlash(stateFile)), data, 0o600)
	}
}

// reconcile makes the files on disk match the log's current state. It
// is skipped when the clean-shutdown marker says the copy is at the
// vault's cursor; the marker is removed while the copy is in use, so a
// crash forces a full pass next time.
func (ws *workspace) reconcile(ctx context.Context) error {
	st := ws.srv.store
	marker := filepath.Join(ws.dir, filepath.FromSlash(stateFile))
	var seq int64
	if err := st.db.QueryRowContext(ctx, `SELECT seq FROM vaults WHERE id = ?`, ws.id).Scan(&seq); err != nil {
		return err
	}
	if data, err := os.ReadFile(marker); err == nil {
		var m struct{ Cursor int64 }
		if json.Unmarshal(data, &m) == nil && m.Cursor == seq {
			return os.Remove(marker)
		}
	}
	_ = os.Remove(marker)
	live, err := liveFiles(ctx, st.db, ws.id)
	if err != nil {
		return err
	}
	fsys := ws.client.FS()
	want := map[string]bool{}
	written := 0
	for _, e := range live {
		if !workPath(e.Path) {
			continue
		}
		want[e.Path] = true
		if old, err := fsys.ReadFile(e.Path); err == nil && protocol.Hash(old) == e.Hash {
			continue
		}
		data, err := ws.srv.readBlob(ctx, e.Hash)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Path, err)
		}
		if err := writeRaw(fsys, e.Path, data); err != nil {
			return err
		}
		written++
	}
	// Remove what the log does not have (deleted while the server was
	// down, or left over from an older copy).
	var stale []string
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return nil
		}
		rel := nvault.NFC(p)
		if d.IsDir() {
			if rel == nvault.TrashDir || rel == nvault.MetaDirName+"/base" {
				return fs.SkipDir
			}
			return nil
		}
		if workPath(rel) && !want[rel] {
			stale = append(stale, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range stale {
		if err := fsys.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	ws.log.Info("working copy reconciled", "files", len(want), "written", written, "removed", len(stale), "cursor", seq)
	return nil
}

// writeRaw writes a file below the FS root, creating its folders,
// without any vault rules (ignore files, events).
func writeRaw(fsys nvault.FS, p string, data []byte) error {
	if dir := path.Dir(p); dir != "." {
		if err := fsys.MkdirAll(dir); err != nil {
			return err
		}
	}
	return fsys.WriteFile(p, data)
}

// liveFiles lists the current non-deleted files of a vault.
func liveFiles(ctx context.Context, q querier, vaultID int64) ([]protocol.Entry, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+entryCols+` FROM files WHERE vault_id = ? AND deleted = 0 ORDER BY path`, vaultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Server) readBlob(ctx context.Context, hash string) ([]byte, error) {
	rc, err := s.blobs.Open(ctx, hash)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// onEvent keeps the index in step with every note change, from clients
// and from the server alike.
func (ws *workspace) onEvent(e events.Event) {
	var err error
	switch e.Type {
	case events.NoteMoved:
		if err = ws.ix.Remove(e.OldPath); err == nil {
			err = ws.ix.Update(ws.client, e.Path)
		}
	case events.NoteDeleted:
		err = ws.ix.Remove(e.Path)
	case events.NoteCreated, events.NoteSaved:
		err = ws.ix.Update(ws.client, e.Path)
	}
	if err != nil {
		ws.log.Error("index update failed", "path", e.Path, "err", err)
	}
}

// loadHooks (re)attaches the vault's hooks when server hooks are on.
func (ws *workspace) loadHooks() {
	if ws.detach != nil {
		ws.detach()
		ws.detach = nil
	}
	if ws.hooks != nil {
		ws.hooks.Wait()
		ws.hooks = nil
	}
	if !ws.srv.cfg.Hooks {
		return
	}
	cfg, err := hooks.Load(ws.client)
	if err != nil {
		ws.log.Error("server hooks disabled for this vault", "err", err)
		return
	}
	if len(cfg.Hooks) == 0 {
		return
	}
	// The runner edits through a vault whose writes are committed as
	// versions of the server-hook device.
	hv, err := ws.editor(DeviceServerHook)
	if err != nil {
		ws.log.Error("server hooks disabled for this vault", "err", err)
		return
	}
	ws.hooks = hooks.New(hv, cfg, hooks.Options{Dir: ws.dir, Logger: ws.log.With("component", "hooks")})
	ws.detach = ws.hooks.Attach(ws.bus)
	ws.log.Info("server hooks loaded", "hooks", len(cfg.Hooks))
}

// apply materializes committed client changes. The caller holds ws.mu.
// Notes go through the client vault so the index and the hooks see
// them; other files and paths the vault's ignore rules exclude are
// written as they are.
func (ws *workspace) apply(ctx context.Context, es ...protocol.Entry) {
	for i := 0; i < len(es); i++ {
		e := es[i]
		var err error
		// A move arrives as the source's tombstone plus the target that
		// names it: rename on disk so history and hooks see a move.
		if i+1 < len(es) && e.Deleted && es[i+1].MovedFrom == e.Path {
			err = ws.applyMove(ctx, e.Path, es[i+1])
			i++
		} else {
			err = ws.applyOne(ctx, e)
		}
		if err != nil {
			ws.log.Error("working copy update failed; it is rebuilt on the next start", "path", e.Path, "version", e.Version, "err", err)
		}
	}
}

func (ws *workspace) applyOne(ctx context.Context, e protocol.Entry) error {
	if !workPath(e.Path) {
		return nil
	}
	fsys := ws.client.FS()
	if e.Deleted {
		if !ws.client.FileExists(e.Path) {
			_ = fsys.Remove(e.Path) // ignored by the vault, or already gone
			return nil
		}
		if ws.client.Ignored(e.Path, false) {
			return fsys.Remove(e.Path)
		}
		trash, err := ws.client.DeleteFile(e.Path)
		if err != nil {
			return err
		}
		// History is in the log; the working copy keeps no trash.
		return fsys.Remove(trash)
	}
	data, err := ws.srv.readBlob(ctx, e.Hash)
	if err != nil {
		return err
	}
	if old, err := fsys.ReadFile(e.Path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if ws.client.Ignored(e.Path, false) {
		err = writeRaw(fsys, e.Path, data)
	} else {
		_, _, err = ws.client.WriteFile(e.Path, data)
	}
	if err != nil {
		return err
	}
	ws.configChanged(e.Path)
	return nil
}

func (ws *workspace) applyMove(ctx context.Context, from string, to protocol.Entry) error {
	if workPath(from) && workPath(to.Path) && ws.client.FileExists(from) &&
		!ws.client.Ignored(from, false) && !ws.client.Ignored(to.Path, false) {
		if _, err := ws.client.RenameFile(from, to.Path); err == nil {
			return nil
		}
	}
	if err := ws.applyOne(ctx, protocol.Entry{Path: from, Deleted: true}); err != nil {
		return err
	}
	return ws.applyOne(ctx, to)
}

// configChanged reloads what depends on a file that just changed.
func (ws *workspace) configChanged(p string) {
	switch p {
	case nvault.IgnoreFile:
		_ = ws.client.ReloadIgnore()
	case hooks.File:
		ws.loadHooks()
	}
}

// editor returns a view of the working copy whose writes are committed
// to the log as versions of device before they reach the disk. Use it
// with ws.mu held.
func (ws *workspace) editor(device string) (*nvault.Vault, error) {
	cfs := &commitFS{FS: nvault.DirFS(ws.dir), ws: ws, device: device}
	return nvault.OpenFS(cfs, ws.dir, nvault.Options{Bus: ws.bus, Now: ws.srv.cfg.Now})
}

// commitFS is the file system under the editor vault: writes, renames
// and removals of working-copy files become versions in the log first.
type commitFS struct {
	nvault.FS
	ws     *workspace
	device string
}

// OSPath lets the vault find files on disk (hooks need real paths).
func (c *commitFS) OSPath(name string) string { return c.FS.(nvault.OSPather).OSPath(name) }

func (c *commitFS) WriteFile(name string, data []byte) error {
	if p := nvault.NFC(name); workPath(p) {
		if err := c.ws.commitPut(p, data, c.device); err != nil {
			return err
		}
	}
	return c.FS.WriteFile(name, data)
}

func (c *commitFS) Rename(oldname, newname string) error {
	from, to := nvault.NFC(oldname), nvault.NFC(newname)
	switch {
	case workPath(from) && workPath(to):
		if err := c.ws.commitMove(from, to, c.device); err != nil {
			return err
		}
	case workPath(from): // into .trash/: a delete
		if err := c.ws.commitDelete(from, c.device); err != nil {
			return err
		}
	case workPath(to): // out of the trash: a create
		data, err := c.FS.ReadFile(oldname)
		if err != nil {
			return err
		}
		if err := c.ws.commitPut(to, data, c.device); err != nil {
			return err
		}
	}
	return c.FS.Rename(oldname, newname)
}

func (c *commitFS) Remove(name string) error {
	p := nvault.NFC(name)
	if st, err := c.FS.Stat(name); err == nil && !st.IsDir() && workPath(p) {
		if err := c.ws.commitDelete(p, c.device); err != nil {
			return err
		}
	}
	return c.FS.Remove(name)
}

// current returns a path's current entry and whether it is live.
func (ws *workspace) current(ctx context.Context, p string) (protocol.Entry, bool, error) {
	e, ok, err := file(ctx, ws.srv.store.db, ws.id, p)
	return e, ok && !e.Deleted, err
}

// commitPut stores data as the new version of p, based on whatever is
// current (the caller holds ws.mu, so nothing moves underneath).
func (ws *workspace) commitPut(p string, data []byte, device string) error {
	ctx := context.Background()
	s := ws.srv
	if int64(len(data)) > s.cfg.MaxFileSize {
		return ErrTooLarge
	}
	cur, ok, err := file(ctx, s.store.db, ws.id, p)
	if err != nil {
		return err
	}
	var base int64
	if ok {
		base = cur.Version
		if !cur.Deleted && cur.Hash == protocol.Hash(data) {
			return nil
		}
	}
	hash, size, err := s.blobs.Put(ctx, bytes.NewReader(data), s.cfg.MaxFileSize)
	if err != nil {
		return err
	}
	e, err := s.store.Put(ctx, ws.userID, ws.name, base, write{Path: p, Hash: hash, Size: size, Device: device})
	if err != nil {
		return err
	}
	ws.committed(e)
	return nil
}

func (ws *workspace) commitDelete(p, device string) error {
	ctx := context.Background()
	cur, live, err := ws.current(ctx, p)
	if err != nil || !live {
		return err
	}
	e, err := ws.srv.store.Delete(ctx, ws.userID, ws.name, p, cur.Version, device)
	if err != nil {
		return err
	}
	ws.committed(e)
	return nil
}

func (ws *workspace) commitMove(from, to, device string) error {
	ctx := context.Background()
	cur, live, err := ws.current(ctx, from)
	if err != nil {
		return err
	}
	if !live {
		// Not in the log (yet): the target is simply a new file.
		data, err := ws.client.FS().ReadFile(from)
		if err != nil {
			return err
		}
		return ws.commitPut(to, data, device)
	}
	res, err := ws.srv.store.Move(ctx, ws.userID, ws.name, protocol.Move{From: from, To: to, BaseVersion: cur.Version}, device)
	if err != nil {
		return err
	}
	ws.committed(res.To)
	return nil
}

// committed announces a version written on the server.
func (ws *workspace) committed(e protocol.Entry) {
	ws.srv.broker.publish(brokerKey(ws.userID, ws.name), e.Version)
	ws.log.Debug("server write", "path", e.Path, "version", e.Version, "device", e.DeviceID, "deleted", e.Deleted)
}

// closeWorkspaces closes every open working copy.
func (s *Server) closeWorkspaces() {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	for id, ws := range s.workspaces {
		ws.close()
		delete(s.workspaces, id)
	}
}

// RebuildWorkspaces deletes every working copy (and index) of the data
// directory; they are rebuilt from the log on first use. Run it while
// the server is stopped.
func RebuildWorkspaces(dataDir string) error {
	return os.RemoveAll(filepath.Join(dataDir, "vaults"))
}
