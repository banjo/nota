package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/banjo/nota/internal/edit"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/plugins"
	"github.com/banjo/nota/internal/protocol"
	nvault "github.com/banjo/nota/internal/vault"
	"github.com/banjo/nota/internal/views"
)

// The web API (docs/api.md): what the web app's NotesClient needs, on
// top of each vault's working copy and index. It takes a session cookie
// or a device token. Every write goes through the vault's editor, so it
// is a new version in the log that sync clients pull like any other.

func (s *Server) apiRoutes() {
	ro := routeOpts{auth: authAny}
	s.route("GET /v1/api/vaults", ro, s.listVaults)
	s.route("POST /v1/api/vaults", ro, s.createVault)
	s.route("PATCH /v1/api/vaults/{vault}", routeOpts{auth: authSession}, s.renameVault)
	s.route("DELETE /v1/api/vaults/{vault}", routeOpts{auth: authSession}, s.deleteVault)
	s.route("GET /v1/api/vaults/{vault}/tree", ro, s.apiTree)
	s.route("GET /v1/api/vaults/{vault}/notes", ro, s.apiList)
	s.route("POST /v1/api/vaults/{vault}/notes", ro, s.apiCreate)
	s.route("GET /v1/api/vaults/{vault}/notes/{path...}", ro, s.apiGet)
	s.route("PUT /v1/api/vaults/{vault}/notes/{path...}", ro, s.apiSave)
	s.route("PATCH /v1/api/vaults/{vault}/notes/{path...}", ro, s.apiPatch)
	s.route("DELETE /v1/api/vaults/{vault}/notes/{path...}", ro, s.apiDelete)
	s.route("POST /v1/api/vaults/{vault}/move", ro, s.apiMove)
	s.route("POST /v1/api/vaults/{vault}/folders", ro, s.apiCreateFolder)
	s.route("POST /v1/api/vaults/{vault}/folders/rename", ro, s.apiRenameFolder)
	s.route("GET /v1/api/vaults/{vault}/search", ro, s.apiSearch)
	s.route("GET /v1/api/vaults/{vault}/tags", ro, s.apiTags)
	s.route("GET /v1/api/vaults/{vault}/views", ro, s.apiViews)
	s.route("GET /v1/api/vaults/{vault}/plugins", ro, s.apiPlugins)
	s.route("GET /v1/api/vaults/{vault}/inbox", ro, s.apiInbox)
	s.route("GET /v1/api/vaults/{vault}/history/{path...}", ro, s.apiHistory)
	s.route("POST /v1/api/vaults/{vault}/history/{path...}", ro, s.apiRestore)
	s.route("GET /v1/api/vaults/{vault}/files/{path...}", ro, s.getFile)
	s.route("GET /v1/api/vaults/{vault}/changes", ro, s.changes)
	s.route("GET /v1/api/vaults/{vault}/events", ro, s.events)
}

// NoteInfo is a note in lists: its metadata without the content.
type NoteInfo struct {
	Path       string         `json:"path"`
	Folder     string         `json:"folder"`
	Title      string         `json:"title"`
	Tags       []string       `json:"tags"`
	Properties map[string]any `json:"properties"`
	Created    *time.Time     `json:"created,omitempty"`
	Updated    *time.Time     `json:"updated,omitempty"`
	Modified   time.Time      `json:"modified"`
	Size       int64          `json:"size"`
	Snippet    string         `json:"snippet,omitempty"`
}

// Note is one note with its content, version and links.
type Note struct {
	NoteInfo
	// Version is the note's version in the log: send it as Base-Version
	// when saving.
	Version int64 `json:"version"`
	// Hash is the content hash (protocol.Hash).
	Hash string `json:"hash"`
	// Body is the markdown after the frontmatter; Content the whole file.
	Body      string       `json:"body"`
	Content   string       `json:"content"`
	Backlinks []index.Link `json:"backlinks"`
	Links     []index.Link `json:"links"`
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

func noteInfo(e index.Entry) NoteInfo {
	if e.Tags == nil {
		e.Tags = []string{}
	}
	if e.Props == nil {
		e.Props = map[string]any{}
	}
	return NoteInfo{Path: e.Path, Folder: e.Folder, Title: e.Title, Tags: e.Tags, Properties: e.Props,
		Created: optTime(e.Created), Updated: optTime(e.Updated), Modified: e.Modified.UTC(), Size: e.Size, Snippet: e.Snippet}
}

func noteInfos(es []index.Entry) []NoteInfo {
	out := make([]NoteInfo, len(es))
	for i, e := range es {
		out[i] = noteInfo(e)
	}
	return out
}

// vaultErr maps vault, index and edit errors onto API errors.
func vaultErr(err error) error {
	if err == nil {
		return nil
	}
	var pe *nvault.PathError
	existing := ""
	if errors.As(err, &pe) {
		existing = pe.Existing
	}
	msg := strings.TrimPrefix(err.Error(), "vault: ")
	switch {
	case errors.Is(err, nvault.ErrNotFound):
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: msg}}
	case errors.Is(err, nvault.ErrCaseCollision):
		return &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeCaseCollision, Message: msg, Existing: existing}}
	case errors.Is(err, nvault.ErrExists):
		return &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeExists, Message: msg, Existing: existing}}
	case errors.Is(err, nvault.ErrInvalidPath), errors.Is(err, nvault.ErrNotNote):
		return &apiErr{http.StatusBadRequest, protocol.Error{Code: protocol.CodeInvalidPath, Message: msg}}
	case errors.Is(err, edit.ErrInvalidTag), errors.Is(err, index.ErrEmptyQuery):
		return badRequest("%s", msg)
	}
	return err
}

// openWS is the working copy of the request's vault.
func (s *Server) openWS(r *http.Request, p Principal) (*workspace, error) {
	return s.workspace(r.Context(), p.UserID, r.PathValue("vault"))
}

// notePath reads the {path...} of a note route (".md" may be left out).
func notePath(r *http.Request) (string, error) {
	p, err := nvault.NotePath(r.PathValue("path"))
	if err != nil {
		return "", vaultErr(err)
	}
	return p, nil
}

// optBase reads an optional Base-Version header (-1 when missing).
func optBase(r *http.Request) (int64, error) {
	if r.Header.Get(protocol.HeaderBaseVersion) == "" {
		return -1, nil
	}
	return baseVersion(r)
}

// checkBase refuses a write based on another version than the current
// one, with the current entry like the sync protocol. The caller holds
// ws.mu. base -1 skips the check.
func (ws *workspace) checkBase(ctx context.Context, p string, base int64) (protocol.Entry, bool, error) {
	cur, live, err := ws.current(ctx, p)
	if err != nil || base < 0 {
		return cur, live, err
	}
	switch {
	case live && base != cur.Version:
		return cur, live, mismatch(&cur, base)
	case !live && base != 0:
		if cur.Version != 0 {
			return cur, live, mismatch(&cur, base)
		}
		return cur, live, mismatch(nil, base)
	}
	return cur, live, nil
}

// note builds the full Note of a path. The caller holds ws.mu.
func (ws *workspace) note(ctx context.Context, p string) (Note, error) {
	n, err := ws.client.Read(p)
	if err != nil {
		return Note{}, vaultErr(err)
	}
	e, err := ws.ix.Get(n.Path)
	if err != nil { // ignored notes are not indexed
		e = index.Entry{Path: n.Path, Folder: n.Folder(), Title: n.Title, Props: n.Props, Modified: n.ModTime, Size: n.Size}
	}
	out := Note{NoteInfo: noteInfo(e), Hash: protocol.Hash(n.Raw), Body: n.Body, Content: string(n.Raw)}
	if cur, live, err := ws.current(ctx, n.Path); err != nil {
		return Note{}, err
	} else if live {
		out.Version = cur.Version
	}
	if out.Backlinks, err = ws.ix.Backlinks(n.Path); err != nil && !errors.Is(err, nvault.ErrNotFound) {
		return Note{}, err
	}
	if out.Links, err = ws.ix.Links(n.Path); err != nil && !errors.Is(err, nvault.ErrNotFound) {
		return Note{}, err
	}
	if out.Backlinks == nil {
		out.Backlinks = []index.Link{}
	}
	if out.Links == nil {
		out.Links = []index.Link{}
	}
	return out, nil
}

// TreeFolder is a folder of the tree with its note count (subfolders
// included).
type TreeFolder struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Notes int    `json:"notes"`
}

// Tree is the body of GET …/tree: every folder and every note.
type Tree struct {
	Inbox   string       `json:"inbox"`
	Folders []TreeFolder `json:"folders"`
	Notes   []NoteInfo   `json:"notes"`
	// Cursor is the vault's latest version when the tree was read.
	Cursor int64 `json:"cursor"`
}

func (s *Server) apiTree(w http.ResponseWriter, r *http.Request, p Principal) error {
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	es, err := ws.ix.Query(index.Query{Sort: index.Sort{Field: "path"}})
	ws.mu.Unlock()
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, e := range es {
		for f := e.Folder; f != ""; f = nvault.Folder(f) {
			counts[f]++
		}
	}
	// Empty folders are on disk only; add them with a count of 0.
	ws.mu.Lock()
	dirs, err := ws.client.Folders()
	ws.mu.Unlock()
	if err != nil {
		return err
	}
	for _, d := range dirs {
		for f := d; f != ""; f = nvault.Folder(f) {
			if _, ok := counts[f]; !ok {
				counts[f] = 0
			}
		}
	}
	out := Tree{Inbox: ws.client.Inbox(), Folders: []TreeFolder{}, Notes: noteInfos(es)}
	for f, n := range counts {
		out.Folders = append(out.Folders, TreeFolder{Path: f, Name: path.Base(f), Notes: n})
	}
	slices.SortFunc(out.Folders, func(a, b TreeFolder) int { return strings.Compare(a.Path, b.Path) })
	if err := s.store.db.QueryRowContext(r.Context(), `SELECT seq FROM vaults WHERE id = ?`, ws.id).Scan(&out.Cursor); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// NotesList is the body of list, search and inbox.
type NotesList struct {
	Notes []NoteInfo `json:"notes"`
	Count int        `json:"count"`
}

// listQuery reads folder, tag, where (repeated k=v or k!=v), view, sort
// and limit from the query string.
func listQuery(ws *workspace, r *http.Request) (index.Query, error) {
	qs := r.URL.Query()
	var q index.Query
	if id := qs.Get("view"); id != "" {
		vs, err := views.Load(ws.client)
		if err != nil {
			return q, err
		}
		vw, ok := views.Find(vs, id)
		if !ok {
			return q, &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: "no view " + id}}
		}
		if q, err = vw.Query(); err != nil {
			return q, badRequest("view %s: %v", id, err)
		}
	}
	if f := qs.Get("folder"); f != "" {
		c, err := nvault.CleanPath(f)
		if err != nil {
			return q, vaultErr(err)
		}
		q.Folder = c
	}
	if t := qs.Get("tag"); t != "" {
		q.Tag = t
	}
	for _, w := range qs["where"] {
		c, err := index.ParseCond(w)
		if err != nil {
			return q, badRequest("%v", err)
		}
		q.Where = append(q.Where, c)
	}
	if srt := qs.Get("sort"); srt != "" || q.Sort.Field == "" {
		so, err := index.ParseSort(srt)
		if err != nil {
			return q, badRequest("%v", err)
		}
		q.Sort = so
	}
	if l := qs.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 0 {
			return q, badRequest("bad limit %q", l)
		}
		q.Limit = n
	}
	return q, nil
}

func (s *Server) apiList(w http.ResponseWriter, r *http.Request, p Principal) error {
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	q, err := listQuery(ws, r)
	if err != nil {
		return err
	}
	es, err := ws.ix.Query(q)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, NotesList{Notes: noteInfos(es), Count: len(es)})
	return nil
}

func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request, p Principal) error {
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	q, err := listQuery(ws, r)
	if err != nil {
		return err
	}
	es, err := ws.ix.Search(r.URL.Query().Get("q"), q)
	if err != nil {
		return vaultErr(err)
	}
	writeJSON(w, http.StatusOK, NotesList{Notes: noteInfos(es), Count: len(es)})
	return nil
}

func (s *Server) apiInbox(w http.ResponseWriter, r *http.Request, p Principal) error {
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	es, err := ws.ix.Query(index.Query{Folder: ws.client.Inbox(), Sort: index.Sort{Field: "created", Desc: true}})
	ws.mu.Unlock()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, NotesList{Notes: noteInfos(es), Count: len(es)})
	return nil
}

func (s *Server) apiTags(w http.ResponseWriter, r *http.Request, p Principal) error {
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	ts, err := ws.ix.Tags()
	ws.mu.Unlock()
	if err != nil {
		return err
	}
	if ts == nil {
		ts = []index.TagCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": ts})
	return nil
}

// apiPlugins lists the vault's plugins (.nota/plugins), broken ones
// with their error.
func (s *Server) apiPlugins(w http.ResponseWriter, r *http.Request, p Principal) error {
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	ps, err := plugins.Load(ws.client)
	ws.mu.Unlock()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": ps})
	return nil
}

// ViewInfo is a saved view with its note count.
type ViewInfo struct {
	views.View
	Count int `json:"count"`
}

func (s *Server) apiViews(w http.ResponseWriter, r *http.Request, p Principal) error {
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	vs, err := views.Load(ws.client)
	if err != nil {
		return badRequest("%v", err)
	}
	out := []ViewInfo{}
	for _, vw := range vs {
		es, err := views.Eval(ws.ix, vw)
		if err != nil {
			return badRequest("view %s: %v", vw.ID, err)
		}
		out = append(out, ViewInfo{View: vw, Count: len(es)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"views": out})
	return nil
}

func (s *Server) apiGet(w http.ResponseWriter, r *http.Request, p Principal) error {
	np, err := notePath(r)
	if err != nil {
		return err
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	n, err := ws.note(r.Context(), np)
	ws.mu.Unlock()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, n)
	return nil
}

// CreateRequest is the body of POST …/notes.
type CreateRequest struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	// Folder defaults to the inbox; "/" is the vault root.
	Folder     string         `json:"folder,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

func (s *Server) apiCreate(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req CreateRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	title := strings.TrimSpace(req.Title)
	text := req.Body
	if title != "" {
		text = "# " + title + "\n"
		if req.Body != "" {
			text += "\n" + req.Body
		}
	}
	if strings.TrimSpace(text) == "" {
		return badRequest("give a title or a body")
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	content, err := edit.New(text, req.Tags, req.Properties, s.cfg.Now())
	if err != nil {
		return vaultErr(err)
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	folder := ws.client.Inbox()
	if req.Folder != "" {
		folder = req.Folder
	}
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	// Hooks may file the note away while it is created: follow it.
	where, stop := ws.bus.Follow()
	defer stop()
	n, err := ed.Create(folder, content)
	if err != nil {
		return vaultErr(err)
	}
	out, err := ws.note(r.Context(), where(n.Path))
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", out.Path, "version", out.Version)
	writeJSON(w, http.StatusCreated, out)
	return nil
}

// SaveRequest is the body of PUT …/notes/{path}: the whole file, or
// only the body (the frontmatter is kept).
type SaveRequest struct {
	Content *string `json:"content,omitempty"`
	Body    *string `json:"body,omitempty"`
}

func (s *Server) apiSave(w http.ResponseWriter, r *http.Request, p Principal) error {
	np, err := notePath(r)
	if err != nil {
		return err
	}
	base, err := baseVersion(r)
	if err != nil {
		return err
	}
	var req SaveRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if (req.Content == nil) == (req.Body == nil) {
		return badRequest("send either content or body")
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ctx := r.Context()
	_, live, err := ws.checkBase(ctx, np, base)
	if err != nil {
		return err
	}
	var data []byte
	switch {
	case req.Content != nil:
		data = []byte(*req.Content)
	case live:
		n, err := ws.client.Read(np)
		if err != nil {
			return vaultErr(err)
		}
		data = edit.ReplaceBody(n.Raw, *req.Body)
	default:
		data = []byte(*req.Body)
	}
	if int64(len(data)) > s.cfg.MaxFileSize {
		return ErrTooLarge
	}
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	if _, _, err := ed.Save(np, data); err != nil {
		return vaultErr(err)
	}
	out, err := ws.note(ctx, np)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", np, "base", base, "version", out.Version)
	writeJSON(w, http.StatusOK, out)
	return nil
}

// PatchRequest is the body of PATCH …/notes/{path}: frontmatter edits.
// Only the touched keys' lines change.
type PatchRequest struct {
	Set   map[string]any `json:"set,omitempty"`
	Unset []string       `json:"unset,omitempty"`
	Tags  struct {
		Add    []string `json:"add,omitempty"`
		Remove []string `json:"remove,omitempty"`
	} `json:"tags,omitzero"`
}

func (s *Server) apiPatch(w http.ResponseWriter, r *http.Request, p Principal) error {
	np, err := notePath(r)
	if err != nil {
		return err
	}
	base, err := optBase(r)
	if err != nil {
		return err
	}
	var req PatchRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Set) == 0 && len(req.Unset) == 0 && len(req.Tags.Add) == 0 && len(req.Tags.Remove) == 0 {
		return badRequest("nothing to set, unset or tag")
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ctx := r.Context()
	if _, _, err := ws.checkBase(ctx, np, base); err != nil {
		return err
	}
	n, err := ws.client.Read(np)
	if err != nil {
		return vaultErr(err)
	}
	src := n.Raw
	if len(req.Set) > 0 || len(req.Unset) > 0 {
		if src, err = edit.Props(src, req.Set, req.Unset); err != nil {
			return badRequest("%v", err)
		}
	}
	if len(req.Tags.Add) > 0 || len(req.Tags.Remove) > 0 {
		if src, _, err = edit.Tags(src, req.Tags.Add, req.Tags.Remove); err != nil {
			return vaultErr(err)
		}
	}
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	if _, _, err := ed.Save(np, src); err != nil {
		return vaultErr(err)
	}
	out, err := ws.note(ctx, np)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", np, "version", out.Version)
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) apiDelete(w http.ResponseWriter, r *http.Request, p Principal) error {
	np, err := notePath(r)
	if err != nil {
		return err
	}
	base, err := optBase(r)
	if err != nil {
		return err
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ctx := r.Context()
	if _, _, err := ws.checkBase(ctx, np, base); err != nil {
		return err
	}
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	trash, err := ed.Delete(np)
	if err != nil {
		return vaultErr(err)
	}
	_ = ws.client.FS().Remove(trash) // the log keeps the history
	cur, _, err := ws.current(ctx, np)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", np, "version", cur.Version)
	writeJSON(w, http.StatusOK, cur)
	return nil
}

// MoveRequest is the body of POST …/move: move a note into Folder
// (keeping its name) or rename it to To.
type MoveRequest struct {
	Path        string `json:"path"`
	Folder      string `json:"folder,omitempty"`
	To          string `json:"to,omitempty"`
	BaseVersion *int64 `json:"base_version,omitempty"`
}

// MoveResponse is where the note went.
type MoveResponse struct {
	From    string `json:"from"`
	Path    string `json:"path"`
	Version int64  `json:"version"`
}

func (s *Server) apiMove(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req MoveRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	from, err := nvault.NotePath(req.Path)
	if err != nil {
		return vaultErr(err)
	}
	if (req.Folder == "") == (req.To == "") {
		return badRequest("give folder or to")
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ctx := r.Context()
	base := int64(-1)
	if req.BaseVersion != nil {
		base = *req.BaseVersion
	}
	if _, _, err := ws.checkBase(ctx, from, base); err != nil {
		return err
	}
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	where, stop := ws.bus.Follow()
	defer stop()
	var to string
	if req.Folder != "" {
		folder := req.Folder
		if folder == "/" {
			folder = ""
		}
		to, err = ed.Move(from, folder)
	} else {
		to, err = ed.Rename(from, req.To)
	}
	if err != nil {
		return vaultErr(err)
	}
	to = where(to)
	cur, _, err := ws.current(ctx, to)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", to, "from", from, "version", cur.Version)
	writeJSON(w, http.StatusOK, MoveResponse{From: from, Path: to, Version: cur.Version})
	return nil
}

// FolderRequest is the body of POST …/folders.
type FolderRequest struct {
	Path string `json:"path"`
}

// FolderResponse is the folder that was made.
type FolderResponse struct {
	Path string `json:"path"`
}

// apiCreateFolder makes an empty folder (POST …/folders). Empty folders
// are not versions, so they stay on this server until a note lands in
// them, and no event is sent.
func (s *Server) apiCreateFolder(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req FolderRequest
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
	np, err := ed.Mkdir(req.Path)
	if err != nil {
		return vaultErr(err)
	}
	info(r).attrs = append(info(r).attrs, "folder", np)
	writeJSON(w, http.StatusCreated, FolderResponse{Path: np})
	return nil
}

// HistoryEntry is one version in a note's history; with ?version=N the
// response is that version with its content.
type HistoryEntry struct {
	protocol.Entry
	Content *string `json:"content,omitempty"`
}

func (s *Server) apiHistory(w http.ResponseWriter, r *http.Request, p Principal) error {
	np, err := filePath(r)
	if err != nil {
		return err
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	vs, err := history(r.Context(), s.store.db, ws.id, np)
	if err != nil {
		return err
	}
	if len(vs) == 0 {
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: np + " has no history"}}
	}
	if v := r.URL.Query().Get("version"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return badRequest("bad version %q", v)
		}
		e, ok := findVersion(vs, n)
		if !ok {
			return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: fmt.Sprintf("%s has no version %d", np, n)}}
		}
		out := HistoryEntry{Entry: e}
		if !e.Deleted {
			data, err := s.readBlob(r.Context(), e.Hash)
			if err != nil {
				return err
			}
			c := string(data)
			out.Content = &c
		}
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	writeJSON(w, http.StatusOK, protocol.History{Path: np, Versions: vs})
	return nil
}

func findVersion(vs []protocol.Entry, n int64) (protocol.Entry, bool) {
	for _, e := range vs {
		if e.Version == n {
			return e, true
		}
	}
	return protocol.Entry{}, false
}

// RestoreRequest is the body of POST …/history/{path}/restore.
type RestoreRequest struct {
	Version int64 `json:"version"`
}

// apiRestore writes an old version's content as the note's newest
// version (POST …/history/{path}/restore). History is never rewritten.
func (s *Server) apiRestore(w http.ResponseWriter, r *http.Request, p Principal) error {
	raw, ok := strings.CutSuffix(r.PathValue("path"), "/restore")
	if !ok {
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: "POST …/history/{path}/restore restores a version"}}
	}
	np, err := protocol.CleanPath(raw)
	if err != nil {
		return &apiErr{http.StatusBadRequest, protocol.Error{Code: protocol.CodeInvalidPath, Message: err.Error()}}
	}
	base, err := optBase(r)
	if err != nil {
		return err
	}
	var req RestoreRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ws, err := s.openWS(r, p)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ctx := r.Context()
	vs, err := history(ctx, s.store.db, ws.id, np)
	if err != nil {
		return err
	}
	e, ok := findVersion(vs, req.Version)
	if !ok {
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: fmt.Sprintf("%s has no version %d", np, req.Version)}}
	}
	if e.Deleted {
		return badRequest("version %d is a delete; restore the version before it", req.Version)
	}
	if _, _, err := ws.checkBase(ctx, np, base); err != nil {
		return err
	}
	data, err := s.readBlob(ctx, e.Hash)
	if err != nil {
		return err
	}
	ed, err := ws.editor(p.Device)
	if err != nil {
		return err
	}
	// The exact old bytes: a restore is not an edit of the content.
	if _, _, err := ed.WriteFile(np, data); err != nil {
		return vaultErr(err)
	}
	cur, _, err := ws.current(ctx, np)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", np, "restored", req.Version, "version", cur.Version)
	writeJSON(w, http.StatusOK, cur)
	return nil
}

// RenameFolderRequest is the body of POST …/folders/rename: To is the
// folder's new vault path (a new name, a new parent, or both).
type RenameFolderRequest struct {
	Path string `json:"path"`
	To   string `json:"to"`
}

// RenameFolderResponse is where the folder went.
type RenameFolderResponse struct {
	From string `json:"from"`
	Path string `json:"path"`
}

// apiRenameFolder renames a folder by moving the files in it one by one,
// as versions of the calling device, so sync sees plain note moves.
func (s *Server) apiRenameFolder(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req RenameFolderRequest
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
	to, err := ed.RenameFolder(req.Path, req.To)
	if err != nil {
		return vaultErr(err)
	}
	info(r).attrs = append(info(r).attrs, "folder", to, "from", req.Path)
	writeJSON(w, http.StatusOK, RenameFolderResponse{From: req.Path, Path: to})
	return nil
}
