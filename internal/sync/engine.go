// Package sync is Nota's client sync engine. It keeps a vault in step
// with a vault on a sync server (internal/server) over the protocol in
// internal/protocol.
//
// The files stay the truth. The engine remembers, per path, the last
// content it synced (the merge base), that content's hash and the
// server version it had, plus the vault's change-feed cursor. A pass:
//
//  1. scans the vault and hashes every file the ignore rules let through;
//  2. pushes what changed locally, each based on the version it came
//     from (a delete plus a create with the same content is a move);
//  3. on a 409 fetches the server copy and merges base/local/remote line
//     by line; overlapping edits (and any edit of an attachment on both
//     sides) produce a conflict copy holding the local version, while the
//     original takes the remote one. A delete never beats an edit;
//  4. pulls the change feed since the cursor and applies it to files that
//     have no local changes, moving remote deletes to .trash/.
//
// State only moves forward after the server confirmed a change and the
// file on disk is in place, so a crash or a network error at any point
// leaves a state the next pass resolves without losing content.
package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"slices"
	"strings"
	gosync "sync"
	"time"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/vault"
)

// Options configure an Engine.
type Options struct {
	// StateDir holds state.db, the merge bases and the lock; the vault's
	// .nota directory when empty (required for file systems without OS
	// paths).
	StateDir string
	// Device names this machine in conflict copies.
	Device string
	// Now is the clock for conflict copy names; time.Now when nil.
	Now func() time.Time
	// Logger gets one event per pass; slog.Default when nil.
	Logger *slog.Logger
	// StaleLock overrides DefaultStaleLock.
	StaleLock time.Duration
	// PageSize is the change-feed page size (server default when 0).
	PageSize int

	// beforePush, in tests, runs before every upload; an error aborts the
	// pass as a network failure would.
	beforePush func(path string) error
}

// Engine syncs one vault with one remote vault. It holds the vault's
// sync lock from Open until Close. Sync may be called from several
// goroutines; passes run one at a time.
type Engine struct {
	v    *vault.Vault
	c    *Client
	opts Options
	st   *state
	lk   *lock
	log  *slog.Logger
	mu   gosync.Mutex
	// closed is set by Close; later calls are no-ops.
	closed bool
}

// Open takes the vault's sync lock and opens the sync state. When the
// state belongs to another remote (URL or vault name changed) it is
// reset, and the next pass reconciles the files by content.
func Open(v *vault.Vault, c *Client, opts Options) (*Engine, error) {
	if opts.StateDir == "" {
		if _, ok := v.FS().(vault.OSPather); !ok {
			return nil, errors.New("sync: Options.StateDir is required for this file system")
		}
		opts.StateDir = v.MetaDir()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.StaleLock <= 0 {
		opts.StaleLock = DefaultStaleLock
	}
	if opts.Device == "" {
		opts.Device = "device"
	}
	st, err := openState(opts.StateDir)
	if err != nil {
		return nil, err
	}
	lk, err := acquireLock(filepath.Join(opts.StateDir, LockFile), opts.StaleLock)
	if err != nil {
		st.close()
		return nil, err
	}
	remote := strings.TrimRight(c.BaseURL, "/") + "#" + c.Vault
	if st.meta("remote") != remote {
		if err := st.reset(remote); err != nil {
			lk.release()
			st.close()
			return nil, err
		}
	}
	return &Engine{v: v.WithOrigin(events.OriginSync), c: c, opts: opts, st: st, lk: lk, log: opts.Logger}, nil
}

// Close releases the lock and closes the state. It is safe to call twice.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	err := e.st.close()
	if lerr := e.lk.release(); err == nil {
		err = lerr
	}
	return err
}

// Client returns the engine's server client.
func (e *Engine) Client() *Client { return e.c }

// Move is a rename.
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Conflict is a file both sides changed in ways that could not be merged.
type Conflict struct {
	// Path kept the remote version.
	Path string `json:"path"`
	// Copy holds the local version.
	Copy string `json:"copy"`
}

// FileError is a problem with one file that did not stop the pass.
type FileError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Result reports what a pass did. Every list holds vault paths.
type Result struct {
	// Pushed are local creates and edits uploaded as they were.
	Pushed []string `json:"pushed"`
	// Deleted are local deletes sent to the server.
	Deleted []string `json:"deleted"`
	// Moved are local renames sent as moves.
	Moved []Move `json:"moved"`
	// Merged are files whose local and remote edits were merged.
	Merged []string `json:"merged"`
	// Conflicts got a conflict copy.
	Conflicts []Conflict `json:"conflicts"`
	// Pulled are remote creates and edits written locally.
	Pulled []string `json:"pulled"`
	// Removed are remote deletes applied locally (moved to .trash/).
	Removed []string `json:"removed"`
	// Renamed are remote moves applied as local renames.
	Renamed []Move `json:"renamed"`
	// Restored are files deleted on one side and edited on the other:
	// the edit won and the file is back.
	Restored []string `json:"restored"`
	// Errors are files that were skipped this time.
	Errors   []FileError   `json:"errors"`
	Cursor   int64         `json:"cursor"`
	Duration time.Duration `json:"duration_ns"`
}

// Changed reports whether the pass changed anything locally or remotely.
func (r *Result) Changed() bool {
	return len(r.Pushed)+len(r.Deleted)+len(r.Moved)+len(r.Merged)+len(r.Conflicts)+
		len(r.Pulled)+len(r.Removed)+len(r.Renamed)+len(r.Restored) > 0
}

func (r *Result) addErr(p string, err error) {
	fe := FileError{Path: p, Code: "error", Message: err.Error()}
	if ae := apiError(err); ae != nil {
		fe.Code, fe.Message = ae.Code(), ae.Body.Message
	} else if errors.Is(err, vault.ErrCaseCollision) {
		fe.Code = protocol.CodeCaseCollision
	} else if errors.Is(err, vault.ErrInvalidPath) || errors.Is(err, protocol.ErrInvalidPath) {
		fe.Code = protocol.CodeInvalidPath
	}
	r.Errors = append(r.Errors, fe)
}

// fileErr reports server refusals that concern one file only: the pass
// goes on without it.
func fileErr(err error) bool {
	ae := apiError(err)
	if ae == nil {
		return false
	}
	switch ae.Code() {
	case protocol.CodeCaseCollision, protocol.CodeTooLarge, protocol.CodeInvalidPath,
		protocol.CodeHashMismatch, protocol.CodeExists:
		return true
	}
	return false
}

// Sync runs one pass. A non-nil error (network, server down, disk) means
// the pass stopped early; whatever it did so far is recorded and the
// next pass continues from there.
func (e *Engine) Sync(ctx context.Context) (*Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, errors.New("sync: engine is closed")
	}
	start := time.Now()
	res := &Result{}
	err := e.pass(ctx, res)
	res.Cursor = e.st.cursor()
	res.Duration = time.Since(start)
	attrs := []any{"vault", e.v.Root(), "remote", e.c.BaseURL, "remote_vault", e.c.Vault,
		"pushed", len(res.Pushed), "deleted", len(res.Deleted), "moved", len(res.Moved),
		"merged", len(res.Merged), "conflicts", len(res.Conflicts), "pulled", len(res.Pulled),
		"removed", len(res.Removed), "renamed", len(res.Renamed), "restored", len(res.Restored),
		"errors", len(res.Errors), "cursor", res.Cursor, "duration", res.Duration.Round(time.Microsecond)}
	if err != nil {
		e.log.Warn("sync pass failed", append(attrs, "err", err)...)
		return res, err
	}
	if gerr := e.st.gcBases(); gerr != nil {
		e.log.Warn("sync: cleaning merge bases", "err", gerr)
	}
	e.log.Debug("sync pass", attrs...)
	e.v.Bus().Emit(events.Event{Type: events.SyncCompleted, Origin: events.OriginSync})
	return res, nil
}

// localFile is a file found by a scan.
type localFile struct {
	hash string
}

func (e *Engine) scan() (map[string]localFile, error) {
	out := map[string]localFile{}
	err := e.v.WalkFiles(func(en vault.Entry) error {
		if _, dup := out[en.Path]; dup {
			return nil // NFC and NFD spellings of one name on a byte-exact FS
		}
		data, err := e.v.ReadEntry(en)
		if err != nil {
			return nil // vanished or unreadable: next pass
		}
		out[en.Path] = localFile{hash: protocol.Hash(data)}
		return nil
	})
	return out, err
}

// readLocal reads a file; ok is false when it does not exist.
func (e *Engine) readLocal(p string) ([]byte, bool, error) {
	data, err := e.v.ReadFile(p)
	if vault.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// unchanged reports whether the file still holds data, so a write over it
// cannot destroy an edit made while the pass was running.
func (e *Engine) unchanged(p string, data []byte) bool {
	cur, ok, err := e.readLocal(p)
	return err == nil && ok && bytes.Equal(cur, data)
}

func (e *Engine) pass(ctx context.Context, res *Result) error {
	if err := e.v.ReloadIgnore(); err != nil {
		return err
	}
	local, err := e.scan()
	if err != nil {
		return err
	}
	rows, err := e.st.all()
	if err != nil {
		return err
	}
	var changed, deleted []string
	created := map[string]bool{}
	for p, lf := range local {
		r, ok := rows[p]
		switch {
		case !ok || r.Deleted:
			changed = append(changed, p)
			created[p] = true
		case lf.hash != r.BaseHash:
			changed = append(changed, p)
		}
	}
	for p, r := range rows {
		if _, ok := local[p]; !ok && !r.Deleted && !e.v.Ignored(p, false) {
			deleted = append(deleted, p)
		}
	}
	slices.Sort(changed)
	slices.Sort(deleted)

	// A delete and a create of the same content is a rename. Only pair
	// them when the content is unambiguous.
	byHash := map[string][]string{}
	for p := range created {
		byHash[local[p].hash] = append(byHash[local[p].hash], p)
	}
	delByHash := map[string][]string{}
	for _, p := range deleted {
		delByHash[rows[p].BaseHash] = append(delByHash[rows[p].BaseHash], p)
	}
	moved := map[string]bool{}
	for _, from := range deleted {
		h := rows[from].BaseHash
		if len(delByHash[h]) != 1 || len(byHash[h]) != 1 {
			continue
		}
		to := byHash[h][0]
		moved[from], moved[to] = true, true
		toRow, hasTo := rows[to]
		if err := e.pushMove(ctx, from, to, rows[from], toRow, hasTo, res); err != nil {
			return err
		}
	}
	for _, p := range changed {
		if moved[p] {
			continue
		}
		r, ok := rows[p]
		if err := e.pushFile(ctx, p, r, ok, res); err != nil {
			return err
		}
	}
	for _, p := range deleted {
		if moved[p] {
			continue
		}
		if err := e.pushDelete(ctx, p, rows[p], res); err != nil {
			return err
		}
	}
	return e.pull(ctx, res)
}

// record stores content as synced at a version: the merge base first, so
// a row never points at a base that is not there.
func (e *Engine) record(p string, data []byte, ent protocol.Entry) error {
	if err := e.st.saveBase(p, data); err != nil {
		return err
	}
	return e.st.synced(p, ent.Hash, ent.Version)
}

func (e *Engine) upload(ctx context.Context, p string, base int64, data []byte) (protocol.Entry, error) {
	if e.opts.beforePush != nil {
		if err := e.opts.beforePush(p); err != nil {
			return protocol.Entry{}, err
		}
	}
	return e.c.Put(ctx, p, base, data, e.opts.Now())
}

// pushFile uploads a local create or edit. r is the path's state when
// hasRow.
func (e *Engine) pushFile(ctx context.Context, p string, r fileState, hasRow bool, res *Result) error {
	data, ok, err := e.readLocal(p)
	if err != nil || !ok {
		return err // gone since the scan: the next pass sees the delete
	}
	var base int64
	var baseHash string
	if hasRow {
		base = r.Version // a tombstone's version recreates the path
		if !r.Deleted {
			baseHash = r.BaseHash
		}
	}
	ent, err := e.upload(ctx, p, base, data)
	switch {
	case err == nil:
		res.Pushed = append(res.Pushed, p)
		return e.record(p, data, ent)
	case IsCode(err, protocol.CodeVersionMismatch):
		return e.resolve(ctx, p, data, baseHash, apiError(err).Current(), res)
	case fileErr(err):
		res.addErr(p, err)
		return nil
	}
	return err
}

// resolve handles a push the server refused because the file changed
// there: merge, or keep both versions.
func (e *Engine) resolve(ctx context.Context, p string, local []byte, baseHash string, cur *protocol.Entry, res *Result) error {
	merged := false
	for range 5 {
		if cur == nil || cur.Deleted {
			// Gone on the server: the local edit wins over the delete.
			var base int64
			if cur != nil {
				base = cur.Version
			}
			ent, err := e.upload(ctx, p, base, local)
			switch {
			case err == nil:
				if merged {
					res.Merged = append(res.Merged, p)
				} else if cur != nil {
					res.Restored = append(res.Restored, p)
				} else {
					res.Pushed = append(res.Pushed, p)
				}
				return e.record(p, local, ent)
			case IsCode(err, protocol.CodeVersionMismatch):
				cur = apiError(err).Current()
				continue
			case fileErr(err):
				res.addErr(p, err)
				return nil
			}
			return err
		}
		rent, remote, err := e.c.Get(ctx, p)
		if ae := apiError(err); ae != nil && (ae.Code() == protocol.CodeDeleted || ae.Code() == protocol.CodeNotFound) {
			cur = ae.Current()
			continue
		}
		if err != nil {
			return err
		}
		if rent.Hash == protocol.Hash(local) {
			if merged {
				res.Merged = append(res.Merged, p)
			}
			return e.record(p, local, rent) // both sides ended up equal
		}
		var out string
		ok := false
		if protocol.KindOf(p) == protocol.KindMD {
			base, _ := e.st.base(baseHash) // a lost base merges nothing silently
			out, ok = Merge3(string(base), string(local), string(remote))
		}
		if !ok {
			return e.conflict(ctx, p, local, rent, remote, res)
		}
		if !e.unchanged(p, local) {
			return nil // edited again meanwhile: the next pass merges that
		}
		if out != string(local) {
			if _, _, err := e.v.WriteFile(p, []byte(out)); err != nil {
				return err
			}
		}
		ent, err := e.upload(ctx, p, rent.Version, []byte(out))
		switch {
		case err == nil:
			res.Merged = append(res.Merged, p)
			return e.record(p, []byte(out), ent)
		case IsCode(err, protocol.CodeVersionMismatch):
			// Changed again on the server: merge once more, with what we
			// just merged against as the base.
			if err := e.st.saveBase(p, remote); err != nil {
				return err
			}
			local, baseHash, cur, merged = []byte(out), rent.Hash, apiError(err).Current(), true
			continue
		case fileErr(err):
			res.addErr(p, err)
			return nil
		}
		return err
	}
	res.addErr(p, errors.New("the file keeps changing on the server; trying again next time"))
	return nil
}

// conflictPath names the conflict copy of p:
// "Name (conflict 2026-11-22 1042 laptop).md".
func (e *Engine) conflictPath(p string) (string, error) {
	ext := path.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	name := fmt.Sprintf("%s (conflict %s %s)%s", stem, e.opts.Now().Format("2006-01-02 1504"), vault.Filename(e.opts.Device), ext)
	return e.v.FreeName(name)
}

// conflict keeps both versions: the local one in a new conflict copy, the
// remote one in the original path.
func (e *Engine) conflict(ctx context.Context, p string, local []byte, rent protocol.Entry, remote []byte, res *Result) error {
	if !e.unchanged(p, local) {
		return nil
	}
	cp, err := e.conflictPath(p)
	if err != nil {
		return err
	}
	// The copy is written first: if anything fails after this, the local
	// version is safe.
	if _, _, err := e.v.WriteFile(cp, local); err != nil {
		return err
	}
	if _, _, err := e.v.WriteFile(p, remote); err != nil {
		return err
	}
	if err := e.record(p, remote, rent); err != nil {
		return err
	}
	res.Conflicts = append(res.Conflicts, Conflict{Path: p, Copy: cp})
	e.log.Info("sync conflict: kept both versions", "path", p, "copy", cp)
	if err := e.pushFile(ctx, cp, fileState{}, false, res); err != nil {
		return err
	}
	// The copy is the local side of a conflict, not a plain push.
	if i := slices.Index(res.Pushed, cp); i >= 0 {
		res.Pushed = slices.Delete(res.Pushed, i, i+1)
	}
	return nil
}

// pushDelete sends a local delete.
func (e *Engine) pushDelete(ctx context.Context, p string, r fileState, res *Result) error {
	if _, ok, err := e.readLocal(p); err != nil || ok {
		return err // back since the scan
	}
	if e.opts.beforePush != nil {
		if err := e.opts.beforePush(p); err != nil {
			return err
		}
	}
	ent, err := e.c.Delete(ctx, p, r.Version)
	ae := apiError(err)
	switch {
	case err == nil:
		res.Deleted = append(res.Deleted, p)
		return e.st.tombstone(p, r.BaseHash, ent.Version)
	case ae != nil && ae.Code() == protocol.CodeVersionMismatch:
		// Edited on the server since: the edit wins, bring it back.
		return e.restore(ctx, p, res)
	case ae != nil && ae.Code() == protocol.CodeDeleted && ae.Current() != nil:
		return e.st.tombstone(p, r.BaseHash, ae.Current().Version)
	case ae != nil && ae.Code() == protocol.CodeNotFound:
		return e.st.tombstone(p, r.BaseHash, 0)
	case fileErr(err):
		res.addErr(p, err)
		return nil
	}
	return err
}

// restore writes the server's current version of a file that was deleted
// locally.
func (e *Engine) restore(ctx context.Context, p string, res *Result) error {
	rent, data, err := e.c.Get(ctx, p)
	if ae := apiError(err); ae != nil && ae.Code() == protocol.CodeDeleted && ae.Current() != nil {
		return e.st.tombstone(p, ae.Current().Hash, ae.Current().Version)
	}
	if err != nil {
		return err
	}
	if _, ok, err := e.readLocal(p); err != nil || ok {
		return err // something new is there: the next pass reconciles
	}
	if _, _, err := e.v.WriteFile(p, data); err != nil {
		if errors.Is(err, vault.ErrCaseCollision) {
			res.addErr(p, err)
			return nil
		}
		return err
	}
	res.Restored = append(res.Restored, p)
	return e.record(p, data, rent)
}

// pushMove sends a local rename, falling back to a create and a delete
// when the server refuses the move.
func (e *Engine) pushMove(ctx context.Context, from, to string, r, toRow fileState, hasTo bool, res *Result) error {
	data, ok, err := e.readLocal(to)
	if err != nil || !ok {
		return err
	}
	if protocol.Hash(data) == r.BaseHash {
		if e.opts.beforePush != nil {
			if err := e.opts.beforePush(to); err != nil {
				return err
			}
		}
		mr, err := e.c.Move(ctx, from, to, r.Version)
		if err == nil {
			res.Moved = append(res.Moved, Move{From: from, To: to})
			if err := e.st.tombstone(from, r.BaseHash, mr.From.Version); err != nil {
				return err
			}
			return e.record(to, data, mr.To)
		}
		if apiError(err) == nil {
			return err
		}
	}
	if err := e.pushFile(ctx, to, toRow, hasTo, res); err != nil {
		return err
	}
	return e.pushDelete(ctx, from, r, res)
}

// errHold marks an entry that could not be applied for a reason the user
// has to fix (a case collision on disk). The cursor stays before it so it
// is tried again.
var errHold = errors.New("hold")

func (e *Engine) pull(ctx context.Context, res *Result) error {
	cursor := e.st.cursor()
	hold := int64(-1)
	save := func() error {
		if hold >= 0 {
			return e.st.setCursor(hold)
		}
		return e.st.setCursor(cursor)
	}
	for {
		ch, err := e.c.Changes(ctx, cursor, e.opts.PageSize)
		if err != nil {
			return err
		}
		movedTo := map[string]protocol.Entry{}
		for _, en := range ch.Entries {
			if en.MovedFrom != "" && !en.Deleted {
				movedTo[en.MovedFrom] = en
			}
		}
		for _, en := range ch.Entries {
			err := e.apply(ctx, en, movedTo, res)
			if errors.Is(err, errHold) {
				if hold < 0 {
					hold = en.Version - 1
				}
				err = nil
			}
			if err != nil {
				if serr := save(); serr != nil {
					e.log.Warn("sync: saving the cursor", "err", serr)
				}
				return err
			}
			cursor = en.Version
		}
		cursor = max(cursor, ch.Cursor)
		if err := save(); err != nil {
			return err
		}
		if !ch.More {
			return nil
		}
	}
}

// apply brings one entry of the change feed to the local vault, unless
// the file has local changes (those are merged when they are pushed).
func (e *Engine) apply(ctx context.Context, en protocol.Entry, movedTo map[string]protocol.Entry, res *Result) error {
	p, err := protocol.CleanPath(en.Path)
	if err != nil {
		res.addErr(en.Path, err)
		return nil
	}
	if e.v.Ignored(p, false) {
		return nil
	}
	r, hasRow, err := e.st.get(p)
	if err != nil {
		return err
	}
	if hasRow && r.Version >= en.Version {
		return nil // ours, or already applied
	}
	data, exists, err := e.readLocal(p)
	if err != nil {
		return err
	}
	var curHash string
	if exists {
		curHash = protocol.Hash(data)
	}
	switch {
	case exists && !en.Deleted && curHash == en.Hash:
		return e.record(p, data, en) // already the same content
	case !exists && en.Deleted:
		return e.st.tombstone(p, en.Hash, en.Version)
	}
	clean := !exists || (hasRow && !r.Deleted && curHash == r.BaseHash)
	if !clean {
		return nil
	}
	if en.Deleted {
		if to, ok := movedTo[p]; ok && to.Hash == curHash && !e.v.Ignored(to.Path, false) {
			if _, ok, _ := e.readLocal(to.Path); !ok {
				if _, err := e.v.RenameFile(p, to.Path); err == nil {
					res.Renamed = append(res.Renamed, Move{From: p, To: to.Path})
					if err := e.st.tombstone(p, en.Hash, en.Version); err != nil {
						return err
					}
					return e.record(to.Path, data, to)
				}
			}
		}
		if _, err := e.v.DeleteFile(p); err != nil && !vault.IsNotFound(err) {
			return err
		}
		res.Removed = append(res.Removed, p)
		return e.st.tombstone(p, en.Hash, en.Version)
	}
	rent, rdata, err := e.c.Get(ctx, p)
	if ae := apiError(err); ae != nil && (ae.Code() == protocol.CodeDeleted || ae.Code() == protocol.CodeNotFound) {
		return nil // changed again: a later entry carries it
	}
	if err != nil {
		return err
	}
	if exists && !e.unchanged(p, data) {
		return nil
	}
	if !exists {
		if _, ok, _ := e.readLocal(p); ok {
			return nil // created meanwhile: the next pass reconciles
		}
	}
	if _, _, err := e.v.WriteFile(p, rdata); err != nil {
		if errors.Is(err, vault.ErrCaseCollision) || errors.Is(err, vault.ErrInvalidPath) {
			res.addErr(p, err)
			return errHold
		}
		return err
	}
	if !exists && hasRow && !r.Deleted {
		res.Restored = append(res.Restored, p)
	} else {
		res.Pulled = append(res.Pulled, p)
	}
	return e.record(p, rdata, rent)
}
