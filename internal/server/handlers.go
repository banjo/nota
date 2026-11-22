package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

func badRequest(format string, args ...any) error {
	return &apiErr{http.StatusBadRequest, protocol.Error{Code: protocol.CodeBadRequest, Message: fmt.Sprintf(format, args...)}}
}

// filePath reads and checks the {path...} wildcard.
func filePath(r *http.Request) (string, error) {
	p, err := protocol.CleanPath(r.PathValue("path"))
	if err != nil {
		return "", &apiErr{http.StatusBadRequest, protocol.Error{Code: protocol.CodeInvalidPath, Message: err.Error()}}
	}
	return p, nil
}

func baseVersion(r *http.Request) (int64, error) {
	h := r.Header.Get(protocol.HeaderBaseVersion)
	if h == "" {
		return 0, badRequest("missing %s header (0 to create)", protocol.HeaderBaseVersion)
	}
	n, err := strconv.ParseInt(h, 10, 64)
	if err != nil || n < 0 {
		return 0, badRequest("bad %s %q", protocol.HeaderBaseVersion, h)
	}
	return n, nil
}

func (s *Server) listVaults(w http.ResponseWriter, r *http.Request, p Principal) error {
	vs, err := s.store.Vaults(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, protocol.VaultList{Vaults: vs})
	return nil
}

func (s *Server) createVault(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req protocol.CreateVault
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		return badRequest("bad JSON: %v", err)
	}
	info(r).vault = req.Name
	v, err := s.store.CreateVault(r.Context(), p.UserID, req.Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, v)
	return nil
}

func (s *Server) changes(w http.ResponseWriter, r *http.Request, p Principal) error {
	q := r.URL.Query()
	var since int64
	if v := q.Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return badRequest("bad since %q", v)
		}
		since = n
	}
	limit := protocol.DefaultChangesLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return badRequest("bad limit %q", v)
		}
		limit = min(n, protocol.MaxChangesLimit)
	}
	var out protocol.Changes
	err := s.store.read(r.Context(), p.UserID, r.PathValue("vault"), func(q querier, v vault) error {
		es, err := changes(r.Context(), q, v.ID, since, limit+1)
		if err != nil {
			return err
		}
		if len(es) > limit {
			es, out.More = es[:limit], true
		}
		out.Entries = es
		// Every version is the current version of some path, so the last
		// entry is the vault's latest version when the page is not full.
		out.Cursor = since
		if len(es) > 0 {
			out.Cursor = es[len(es)-1].Version
		}
		return nil
	})
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "since", since, "entries", len(out.Entries))
	writeJSON(w, http.StatusOK, out)
	return nil
}

func setEntryHeaders(h http.Header, e protocol.Entry) {
	h.Set(protocol.HeaderVersion, strconv.FormatInt(e.Version, 10))
	h.Set(protocol.HeaderHash, e.Hash)
	h.Set(protocol.HeaderKind, string(e.Kind))
	h.Set(protocol.HeaderDevice, e.DeviceID)
	h.Set(protocol.HeaderModifiedAt, e.ModifiedAt.Format(time.RFC3339Nano))
	if e.MovedFrom != "" {
		h.Set(protocol.HeaderMovedFrom, protocol.EscapePath(e.MovedFrom))
	}
	h.Set("ETag", strconv.Quote(strconv.FormatInt(e.Version, 10)))
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request, p Principal) error {
	path, err := filePath(r)
	if err != nil {
		return err
	}
	var e protocol.Entry
	err = s.store.read(r.Context(), p.UserID, r.PathValue("vault"), func(q querier, v vault) error {
		if vs := r.URL.Query().Get("version"); vs != "" {
			n, err := strconv.ParseInt(vs, 10, 64)
			if err != nil {
				return badRequest("bad version %q", vs)
			}
			if e, err = versionOf(r.Context(), q, v.ID, path, n); errors.Is(err, ErrNotFound) {
				return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound,
					Message: fmt.Sprintf("%s has no version %d", path, n)}}
			}
			return err
		}
		var ok bool
		e, ok, err = file(r.Context(), q, v.ID, path)
		if err == nil && !ok {
			return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: path + " does not exist"}}
		}
		return err
	})
	if err != nil {
		return err
	}
	if e.Deleted {
		return &apiErr{http.StatusGone, protocol.Error{Code: protocol.CodeDeleted, Message: path + " is deleted", Current: &e}}
	}
	rc, err := s.blobs.Open(r.Context(), e.Hash)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("blob %s of %s is missing", e.Hash, path)
	}
	if err != nil {
		return err
	}
	defer rc.Close()
	setEntryHeaders(w.Header(), e)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(e.Size, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
	return nil
}

func (s *Server) putFile(w http.ResponseWriter, r *http.Request, p Principal) error {
	path, err := filePath(r)
	if err != nil {
		return err
	}
	base, err := baseVersion(r)
	if err != nil {
		return err
	}
	if r.ContentLength > s.cfg.MaxFileSize {
		return ErrTooLarge
	}
	var mod time.Time
	if h := r.Header.Get(protocol.HeaderModifiedAt); h != "" {
		if mod, err = time.Parse(time.RFC3339Nano, h); err != nil {
			return badRequest("bad %s %q", protocol.HeaderModifiedAt, h)
		}
	}
	name := r.PathValue("vault")
	// Check the vault before storing anything.
	if err := s.store.read(r.Context(), p.UserID, name, func(querier, vault) error { return nil }); err != nil {
		return err
	}
	hash, size, err := s.blobs.Put(r.Context(), r.Body, s.cfg.MaxFileSize)
	if err != nil {
		return err
	}
	if want := r.Header.Get(protocol.HeaderHash); want != "" && want != hash {
		return &apiErr{http.StatusBadRequest, protocol.Error{Code: protocol.CodeHashMismatch,
			Message: fmt.Sprintf("body hash %s does not match %s %s", hash, protocol.HeaderHash, want)}}
	}
	e, err := s.store.Put(r.Context(), p.UserID, name, base, write{Path: path, Hash: hash, Size: size,
		Device: p.Device, ModifiedAt: mod})
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", path, "base", base, "version", e.Version)
	s.broker.publish(brokerKey(p.UserID, name), e.Version)
	writeJSON(w, http.StatusOK, e)
	return nil
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request, p Principal) error {
	path, err := filePath(r)
	if err != nil {
		return err
	}
	base, err := baseVersion(r)
	if err != nil {
		return err
	}
	name := r.PathValue("vault")
	e, err := s.store.Delete(r.Context(), p.UserID, name, path, base, p.Device)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", path, "base", base, "version", e.Version)
	s.broker.publish(brokerKey(p.UserID, name), e.Version)
	writeJSON(w, http.StatusOK, e)
	return nil
}

func (s *Server) move(w http.ResponseWriter, r *http.Request, p Principal) error {
	var m protocol.Move
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&m); err != nil {
		return badRequest("bad JSON: %v", err)
	}
	var err error
	for _, x := range []*string{&m.From, &m.To} {
		if *x, err = protocol.CleanPath(*x); err != nil {
			return &apiErr{http.StatusBadRequest, protocol.Error{Code: protocol.CodeInvalidPath, Message: err.Error()}}
		}
	}
	if m.From == m.To {
		return badRequest("from and to are the same path")
	}
	name := r.PathValue("vault")
	res, err := s.store.Move(r.Context(), p.UserID, name, m, p.Device)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "file", m.To, "from", m.From, "version", res.To.Version)
	s.broker.publish(brokerKey(p.UserID, name), res.To.Version)
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) history(w http.ResponseWriter, r *http.Request, p Principal) error {
	path, err := filePath(r)
	if err != nil {
		return err
	}
	out := protocol.History{Path: path}
	err = s.store.read(r.Context(), p.UserID, r.PathValue("vault"), func(q querier, v vault) error {
		out.Versions, err = history(r.Context(), q, v.ID, path)
		return err
	})
	if err != nil {
		return err
	}
	if len(out.Versions) == 0 {
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: path + " has no history"}}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) events(w http.ResponseWriter, r *http.Request, p Principal) error {
	name := r.PathValue("vault")
	var cursor int64
	if err := s.store.read(r.Context(), p.UserID, name, func(_ querier, v vault) error {
		cursor = v.Seq
		return nil
	}); err != nil {
		return err
	}
	ch, cancel := s.broker.subscribe(brokerKey(p.UserID, name))
	defer cancel()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // a stream has no deadline
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // no proxy buffering (nginx, Traefik)
	w.WriteHeader(http.StatusOK)
	send := func(c int64) error {
		data, _ := json.Marshal(protocol.ChangeEvent{Cursor: c})
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", protocol.EventChange, data); err != nil {
			return err
		}
		return rc.Flush()
	}
	if err := send(cursor); err != nil {
		return nil
	}
	tick := time.NewTicker(s.cfg.KeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-s.broker.done:
			return nil
		case c := <-ch:
			if send(c) != nil {
				return nil
			}
		case <-tick.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil || rc.Flush() != nil {
				return nil
			}
		}
	}
}
