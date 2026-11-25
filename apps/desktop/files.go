package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/banjo/nota/internal/vault"
)

// fileScheme serves the vault's images to the window and takes pasted
// ones, streamed by Go rather than sent through the bindings:
//
//	GET  notafile://vault/<path>              the image at a vault path
//	GET  notafile://vault/<name>?from=<note>  an embed's target (![[name]])
//	POST notafile://attach/<name>             saves the body to Attachments/
//
// WebView2 (Windows) loads them from http://vault.notafile.localhost/ and
// http://attach.notafile.localhost/ (packages/client's mygo.ts).
const fileScheme = "notafile"

// maxAttachment caps a pasted or dropped file, as the server does.
const maxAttachment = 100 << 20

// files is the http.Handler of fileScheme.
type files struct{ s *Vault }

func (f files) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.s.v == nil {
		http.Error(w, ErrNoVault.Error(), http.StatusServiceUnavailable)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	switch {
	case r.URL.Host == "vault" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		f.serve(w, r, name)
	case r.URL.Host == "attach" && r.Method == http.MethodPost:
		f.attach(w, r, name)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (f files) serve(w http.ResponseWriter, r *http.Request, name string) {
	s := f.s
	s.mu.Lock()
	p, err := name, error(nil)
	if from := r.URL.Query().Get("from"); from != "" {
		p, err = s.v.FindFile(name, from)
	} else if !s.v.HasFile(name) {
		err = vault.ErrNotFound
	}
	var data []byte
	if err == nil {
		data, err = s.v.ReadFile(p)
	}
	s.mu.Unlock()
	ct := vault.ImageType(p)
	if err != nil || ct == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func (f files) attach(w http.ResponseWriter, r *http.Request, name string) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxAttachment+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(data) > maxAttachment {
		http.Error(w, "the file is larger than 100 MB", http.StatusRequestEntityTooLarge)
		return
	}
	s := f.s
	s.mu.Lock()
	p, err := s.v.SaveAttachment(name, data)
	s.mu.Unlock()
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, vault.ErrNotImage) || errors.Is(err, vault.ErrInvalidPath) {
			status = http.StatusBadRequest
		}
		http.Error(w, strings.TrimPrefix(err.Error(), "vault: "), status)
		return
	}
	s.changes.add(p)
	h := w.Header()
	h.Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"path": p})
}
