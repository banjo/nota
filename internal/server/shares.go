package server

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/publish"
	nvault "github.com/banjo/nota/internal/vault"
)

// Shared notes (docs/api.md, "Sharing"): a note of a vault on the server
// shown at /s/<id> as a web page, live, to anyone with the link and, when
// the owner set one, the password. The page is static HTML from
// internal/publish; links to notes that aren't shared are plain text,
// and only the images the note shows are served.

// MaxSharePassword is the longest password a share takes, in bytes.
const MaxSharePassword = 256

// shareIterations is the PBKDF2 cost of a share password (OWASP's
// SHA-256 figure); tests lower it.
var shareIterations = 600_000

type share struct {
	ID, Path, PasswordHash string
	Theme                  *protocol.ShareTheme
	VaultID, UserID        int64
	VaultName              string
	Created, Updated       time.Time
}

const shareCols = `s.id, s.path, s.password_hash, s.theme, s.created_at, s.updated_at, v.id, v.user_id, v.name`

func scanShare(sc interface{ Scan(...any) error }) (share, error) {
	var sh share
	var theme string
	var created, updated int64
	if err := sc.Scan(&sh.ID, &sh.Path, &sh.PasswordHash, &theme, &created, &updated, &sh.VaultID, &sh.UserID, &sh.VaultName); err != nil {
		return sh, err
	}
	if theme != "" {
		sh.Theme = &protocol.ShareTheme{}
		if err := json.Unmarshal([]byte(theme), sh.Theme); err != nil {
			return sh, fmt.Errorf("server: theme of share %s: %w", sh.ID, err)
		}
	}
	sh.Created, sh.Updated = fromMS(created), fromMS(updated)
	return sh, nil
}

// Shares lists the shared notes of a vault, by path.
func (s *Store) Shares(ctx context.Context, userID int64, vaultName string) ([]share, error) {
	v, err := vaultOf(ctx, s.db, userID, vaultName)
	if err != nil {
		return nil, err
	}
	return s.vaultShares(ctx, v.ID)
}

func (s *Store) vaultShares(ctx context.Context, vaultID int64) ([]share, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+shareCols+` FROM shares s JOIN vaults v ON v.id = s.vault_id
		WHERE s.vault_id = ? ORDER BY s.path`, vaultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []share{}
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// shareByID finds a share by the id in its link.
func (s *Store) shareByID(ctx context.Context, id string) (share, error) {
	sh, err := scanShare(s.db.QueryRowContext(ctx, `SELECT `+shareCols+` FROM shares s JOIN vaults v ON v.id = s.vault_id
		WHERE s.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sh, fmt.Errorf("share %s: %w", id, ErrNotFound)
	}
	return sh, err
}

// PutShare shares the note at path, or changes its share (see
// protocol.ShareRequest). The caller checks that the note exists.
func (s *Store) PutShare(ctx context.Context, userID int64, vaultName, path string, req protocol.ShareRequest) (share, error) {
	var hash string
	if req.Password != nil && *req.Password != "" {
		var err error
		if hash, err = hashSharePassword(*req.Password); err != nil {
			return share{}, err
		}
	}
	var theme string
	if req.Theme != nil && (req.Theme.Mode != "" || len(req.Theme.Tokens) > 0) {
		b, err := json.Marshal(req.Theme)
		if err != nil {
			return share{}, err
		}
		theme = string(b)
	}
	var out share
	err := s.inTx(ctx, userID, vaultName, func(tx *sql.Tx, v vault) error {
		now := ms(s.now())
		id, err := newShareID()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO shares (id, vault_id, path, password_hash, theme, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (vault_id, path) DO NOTHING`,
			id, v.ID, path, hash, theme, now, now); err != nil {
			return err
		}
		if req.Password != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE shares SET password_hash = ?, updated_at = ? WHERE vault_id = ? AND path = ?`,
				hash, now, v.ID, path); err != nil {
				return err
			}
		}
		if req.Theme != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE shares SET theme = ?, updated_at = ? WHERE vault_id = ? AND path = ?`,
				theme, now, v.ID, path); err != nil {
				return err
			}
		}
		out, err = scanShare(tx.QueryRowContext(ctx, `SELECT `+shareCols+` FROM shares s JOIN vaults v ON v.id = s.vault_id
			WHERE s.vault_id = ? AND s.path = ?`, v.ID, path))
		return err
	})
	return out, err
}

// DeleteShare stops sharing the note at path; its link leads nowhere
// from then on. Not shared is ErrNotFound.
func (s *Store) DeleteShare(ctx context.Context, userID int64, vaultName, path string) error {
	return s.inTx(ctx, userID, vaultName, func(tx *sql.Tx, v vault) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM shares WHERE vault_id = ? AND path = ?`, v.ID, path)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("%s is not shared: %w", path, ErrNotFound)
		}
		return nil
	})
}

// secret returns the server's key called name, making it on first use.
func (s *Store) secret(ctx context.Context, name string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO server_secrets (name, value) VALUES (?, ?) ON CONFLICT DO NOTHING`, name, key); err != nil {
		return nil, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT value FROM server_secrets WHERE name = ?`, name).Scan(&key)
	return key, err
}

func newShareID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashSharePassword(pw string) (string, error) {
	if len(pw) > MaxSharePassword {
		return "", badRequest("the password is longer than %d bytes", MaxSharePassword)
	}
	if !utf8.ValidString(pw) {
		return "", badRequest("the password is not UTF-8")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, shareIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", shareIterations, hex.EncodeToString(salt), hex.EncodeToString(key)), nil
}

func checkSharePassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || len(pw) > MaxSharePassword {
		return false
	}
	n, err1 := strconv.Atoi(parts[1])
	salt, err2 := hex.DecodeString(parts[2])
	want, err3 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || n <= 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, n, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// The web API.

func (s *Server) shareRoutes() {
	ro := routeOpts{auth: authAny}
	s.route("GET /v1/api/vaults/{vault}/shares", ro, s.apiShares)
	s.route("PUT /v1/api/vaults/{vault}/shares/{path...}", ro, s.apiPutShare)
	s.route("DELETE /v1/api/vaults/{vault}/shares/{path...}", ro, s.apiDeleteShare)
	s.mux.HandleFunc("GET /s/{id}", s.public(s.sharePage))
	s.mux.HandleFunc("POST /s/{id}", s.public(s.shareUnlock))
	s.mux.HandleFunc("GET /s/{id}/f/{path...}", s.public(s.shareFile))
}

func (s *Server) shareOut(r *http.Request, sh share) protocol.Share {
	base := strings.TrimRight(s.cfg.PublicURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return protocol.Share{ID: sh.ID, Path: sh.Path, URL: base + "/s/" + sh.ID, Password: sh.PasswordHash != "",
		Theme: sh.Theme, Created: sh.Created, Updated: sh.Updated}
}

func (s *Server) apiShares(w http.ResponseWriter, r *http.Request, p Principal) error {
	list, err := s.store.Shares(r.Context(), p.UserID, r.PathValue("vault"))
	if err != nil {
		return err
	}
	out := make([]protocol.Share, len(list))
	for i, sh := range list {
		out[i] = s.shareOut(r, sh)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) apiPutShare(w http.ResponseWriter, r *http.Request, p Principal) error {
	path, err := notePath(r)
	if err != nil {
		return err
	}
	var req protocol.ShareRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
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
	if _, live, err := ws.current(r.Context(), path); err != nil {
		return err
	} else if !live {
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: path + " does not exist"}}
	}
	sh, err := s.store.PutShare(r.Context(), p.UserID, r.PathValue("vault"), path, req)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s.shareOut(r, sh))
	return nil
}

func (s *Server) apiDeleteShare(w http.ResponseWriter, r *http.Request, p Principal) error {
	path, err := notePath(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteShare(r.Context(), p.UserID, r.PathValue("vault"), path); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// The public pages.

// public wraps a page of /s/: headers that keep it to itself (no
// scripts, no framing, no referrer, not indexed) and errors as pages.
func (s *Server) public(h func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; "+
			"form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Robots-Tag", "noindex, nofollow")
		hd.Set("Cache-Control", "private, no-cache")
		err := h(w, r)
		if err == nil {
			return
		}
		info(r).attrs = append(info(r).attrs, "err", err.Error())
		if errors.Is(err, ErrNotFound) || errors.Is(err, nvault.ErrNotFound) {
			info(r).code = protocol.CodeNotFound
			hd.Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_ = publish.WriteMissing(w, publish.Missing{Home: s.cfg.WebURL})
			return
		}
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

func shareCookie(id string) string { return "nota_share_" + id }

// unlockValue is the cookie of an unlocked share: it is only good for
// the password it was made for.
func (s *Server) unlockValue(ctx context.Context, sh share) (string, error) {
	key, err := s.store.secret(ctx, "share-cookie")
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(sh.ID + "\x00" + sh.PasswordHash))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

func (s *Server) unlocked(r *http.Request, sh share) (bool, error) {
	if sh.PasswordHash == "" {
		return true, nil
	}
	c, err := r.Cookie(shareCookie(sh.ID))
	if err != nil {
		return false, nil
	}
	want, err := s.unlockValue(r.Context(), sh)
	if err != nil {
		return false, err
	}
	return hmac.Equal([]byte(c.Value), []byte(want)), nil
}

func (s *Server) writeLock(w http.ResponseWriter, sh share, status int, msg string) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return publish.WriteLock(w, publish.Lock{Theme: sh.Theme, Error: msg, Home: s.cfg.WebURL})
}

// shareResolver resolves the links and images of a shared note: notes
// shared from the same vault are links, images of the vault are served
// under the share. The caller holds ws.mu.
type shareResolver struct {
	ws     *workspace
	sh     share
	shared map[string]string // path → share id
	images map[string]bool   // the vault paths of the images shown
}

func (sr *shareResolver) Note(l markdown.Link) (string, bool) {
	p, err := sr.ws.ix.ResolveLink(l.Target, sr.sh.Path, l.Wiki)
	if err != nil || p == "" {
		return "", false
	}
	id, ok := sr.shared[p]
	if !ok {
		return "", false
	}
	return "/s/" + id, true
}

func (sr *shareResolver) Image(target string, wiki bool) (string, bool) {
	p := target
	if wiki {
		var err error
		if p, err = sr.ws.client.FindFile(target, sr.sh.Path); err != nil {
			return "", false
		}
	} else if !sr.ws.client.HasFile(p) {
		return "", false
	}
	if nvault.ImageType(p) == "" {
		return "", false
	}
	sr.images[p] = true
	return "/s/" + sr.sh.ID + "/f/" + protocol.EscapePath(p), true
}

// render reads the shared note and renders it. The caller holds ws.mu.
func (s *Server) render(ctx context.Context, ws *workspace, sh share) (*nvault.Note, *shareResolver, template.HTML, error) {
	n, err := ws.client.Read(sh.Path)
	if err != nil {
		return nil, nil, "", err
	}
	list, err := s.store.vaultShares(ctx, sh.VaultID)
	if err != nil {
		return nil, nil, "", err
	}
	sr := &shareResolver{ws: ws, sh: sh, shared: map[string]string{}, images: map[string]bool{}}
	for _, o := range list {
		sr.shared[o.Path] = o.ID
	}
	title, body := publish.RenderNote(n.Raw, publish.Options{Path: sh.Path, Resolver: sr})
	n.Title = title
	return n, sr, body, nil
}

func (s *Server) openShare(r *http.Request) (share, *workspace, error) {
	sh, err := s.store.shareByID(r.Context(), r.PathValue("id"))
	if err != nil {
		return sh, nil, err
	}
	info(r).vault = sh.VaultName
	ws, err := s.workspace(r.Context(), sh.UserID, sh.VaultName)
	return sh, ws, err
}

func (s *Server) sharePage(w http.ResponseWriter, r *http.Request) error {
	sh, ws, err := s.openShare(r)
	if err != nil {
		return err
	}
	if ok, err := s.unlocked(r, sh); err != nil {
		return err
	} else if !ok {
		return s.writeLock(w, sh, http.StatusUnauthorized, "")
	}
	ws.mu.Lock()
	n, _, body, err := s.render(r.Context(), ws, sh)
	ws.mu.Unlock()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return publish.WritePage(w, publish.Page{
		Title:    n.Title,
		Body:     body,
		Modified: n.ModTime.Format("2 January 2006"),
		Theme:    sh.Theme,
		Home:     s.cfg.WebURL,
	})
}

// shareUnlock checks a password sent from the lock page, and on success
// sets the share's cookie and goes back to the note.
func (s *Server) shareUnlock(w http.ResponseWriter, r *http.Request) error {
	sh, err := s.store.shareByID(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	if sh.PasswordHash == "" {
		http.Redirect(w, r, "/s/"+sh.ID, http.StatusSeeOther)
		return nil
	}
	if s.shareLimit != nil {
		if ok, wait := s.shareLimit.allow(s.clientIP(r) + " " + sh.ID); !ok {
			info(r).attrs = append(info(r).attrs, "rate_limited", true)
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			return s.writeLock(w, sh, http.StatusTooManyRequests,
				fmt.Sprintf("Too many tries. Wait %d seconds and try again.", int(wait.Seconds())+1))
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		return s.writeLock(w, sh, http.StatusBadRequest, "Enter the password.")
	}
	if !checkSharePassword(sh.PasswordHash, r.PostForm.Get("password")) {
		info(r).attrs = append(info(r).attrs, "share_unlock", false)
		return s.writeLock(w, sh, http.StatusUnauthorized, "That password isn't right.")
	}
	v, err := s.unlockValue(r.Context(), sh)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "share_unlock", true)
	http.SetCookie(w, &http.Cookie{Name: shareCookie(sh.ID), Value: v, Path: "/s/" + sh.ID,
		MaxAge: int((30 * 24 * time.Hour).Seconds()), HttpOnly: true, Secure: !s.cfg.CookieInsecure,
		SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/s/"+sh.ID, http.StatusSeeOther)
	return nil
}

// shareFile serves an image the shared note shows, and nothing else.
func (s *Server) shareFile(w http.ResponseWriter, r *http.Request) error {
	sh, ws, err := s.openShare(r)
	if err != nil {
		return err
	}
	if ok, err := s.unlocked(r, sh); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("locked: %w", ErrNotFound)
	}
	p, err := protocol.CleanPath(r.PathValue("path"))
	if err != nil {
		return fmt.Errorf("%v: %w", err, ErrNotFound)
	}
	ws.mu.Lock()
	_, sr, _, err := s.render(r.Context(), ws, sh)
	ws.mu.Unlock()
	if err != nil {
		return err
	}
	if !sr.images[p] {
		return fmt.Errorf("%s is not shown by share %s: %w", p, sh.ID, ErrNotFound)
	}
	e, live, err := ws.current(r.Context(), p)
	if err != nil {
		return err
	}
	if !live {
		return fmt.Errorf("%s: %w", p, ErrNotFound)
	}
	if r.Header.Get("If-None-Match") == strconv.Quote(strconv.FormatInt(e.Version, 10)) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	rc, err := s.blobs.Open(r.Context(), e.Hash)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("blob %s of %s is missing", e.Hash, p)
	}
	if err != nil {
		return err
	}
	defer rc.Close()
	h := w.Header()
	h.Set("ETag", strconv.Quote(strconv.FormatInt(e.Version, 10)))
	h.Set("Content-Type", nvault.ImageType(p))
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("Content-Length", strconv.FormatInt(e.Size, 10))
	_, err = io.Copy(w, rc)
	return err
}
