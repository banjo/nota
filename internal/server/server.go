// Package server is the Nota sync server behind `nota serve`: a
// multi-user store of vaults with a version log (SQLite), content
// addressed blobs, device-token auth and the HTTP API of the sync
// protocol (internal/protocol, docs/sync-protocol.md).
//
// The server keeps no state outside its data directory, so it runs the
// same on a laptop and in a container. The HTTP layer is a plain
// http.ServeMux: later phases mount their routes (/auth, /api, /mcp) on
// it with Handle.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/banjo/nota/internal/protocol"
)

// DefaultMaxFileSize is the default size limit of one file (100 MB).
const DefaultMaxFileSize = 100 << 20

// Config configures a Server.
type Config struct {
	// DataDir holds the database (nota.db) and the blob store (blobs/).
	DataDir string
	// MaxFileSize caps the size of one file; DefaultMaxFileSize if 0.
	MaxFileSize int64
	// Logger receives one wide event per request; slog.Default if nil.
	Logger *slog.Logger
	// Blobs overrides the blob store (default: DiskBlobs in DataDir/blobs).
	Blobs BlobStore
	// KeepAlive is the SSE keepalive interval; 25 s if 0.
	KeepAlive time.Duration

	// PublicURL is where the API is reached from outside, e.g.
	// https://nota.app.banjoanton.com.
	PublicURL string
	// WebURL is the web app's base URL (e.g. https://nota.banjoanton.com):
	// sign-in links point at WebURL/app/connect. The first WebOrigins
	// entry when empty.
	WebURL string
	// WebOrigins are the browser origins of the web app: CORS with
	// credentials is allowed for them and passkeys are accepted from them.
	WebOrigins []string
	// RPID is the WebAuthn relying party id (the web app's domain, e.g.
	// nota.banjoanton.com). Passkeys are off when empty.
	RPID string
	// CookieDomain is the session cookie's Domain (e.g. banjoanton.com,
	// so the cookie set by the API also covers the web app's site). Host
	// only when empty.
	CookieDomain string
	// CookieInsecure drops the Secure flag of the session cookie, for
	// local development over plain http only.
	CookieInsecure bool
	// Signup is who may create an account (default SignupInvite).
	Signup SignupMode
	// SessionTTL is how long an unused browser session lives
	// (DefaultSessionTTL if 0); each use extends it.
	SessionTTL time.Duration
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is
	// believed (see ParseProxies).
	TrustedProxies []netip.Prefix
	// AuthRateLimit is how many sign-in attempts one IP may make per
	// minute (default 30, burst 20); negative turns the limit off.
	AuthRateLimit int
	// PollInterval is the minimum interval between device-code polls
	// (DefaultPollInterval if 0).
	PollInterval time.Duration
	// Hooks runs the hooks in each vault's .nota/hooks.toml on changes
	// that arrive from clients (NOTA_SERVER_HOOKS=on). Off by default:
	// hooks are shell commands from the synced vault, run by the server
	// process with its permissions.
	Hooks bool
	// Now is the clock (time.Now if nil); tests move it.
	Now func() time.Time
}

// Server is the sync server. Create it with New and serve Handler.
type Server struct {
	cfg    Config
	store  *Store
	blobs  BlobStore
	log    *slog.Logger
	mux    *http.ServeMux
	broker *broker
	wa     *webauthn.WebAuthn
	limit  *limiter

	wsMu       gosync.Mutex
	workspaces map[int64]*workspace // by vault id
}

// DBFile is the database file name inside the data directory.
const DBFile = "nota.db"

// OpenData opens the store in a data directory, creating it if needed.
// The admin commands use it directly, next to a running server.
func OpenData(ctx context.Context, dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("server: no data directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	return OpenStore(ctx, filepath.Join(dir, DBFile))
}

// New opens the data directory and returns a server ready to serve.
func New(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = DefaultMaxFileSize
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 25 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Signup == "" {
		cfg.Signup = SignupInvite
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	cfg.PollInterval = max(cfg.PollInterval.Round(time.Second), time.Second)
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = DefaultSessionTTL
	}
	if cfg.WebURL == "" && len(cfg.WebOrigins) > 0 {
		cfg.WebURL = cfg.WebOrigins[0]
	}
	cfg.WebURL = strings.TrimRight(cfg.WebURL, "/")
	var wa *webauthn.WebAuthn
	if cfg.RPID != "" {
		if len(cfg.WebOrigins) == 0 {
			return nil, errors.New("server: passkeys need at least one web origin (NOTA_SERVER_WEB_ORIGIN)")
		}
		var err error
		wa, err = webauthn.New(&webauthn.Config{RPID: cfg.RPID, RPDisplayName: "Nota", RPOrigins: cfg.WebOrigins})
		if err != nil {
			return nil, fmt.Errorf("server: passkeys: %w", err)
		}
	}
	st, err := OpenData(ctx, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	st.now = cfg.Now
	blobs := cfg.Blobs
	if blobs == nil {
		if blobs, err = NewDiskBlobs(filepath.Join(cfg.DataDir, "blobs")); err != nil {
			st.Close()
			return nil, err
		}
	}
	s := &Server{cfg: cfg, store: st, blobs: blobs, log: cfg.Logger, mux: http.NewServeMux(), broker: newBroker(), wa: wa,
		workspaces: map[int64]*workspace{}}
	if cfg.AuthRateLimit >= 0 {
		n := cfg.AuthRateLimit
		if n == 0 {
			n = 30
		}
		s.limit = newLimiter(n, 20, cfg.Now)
	}
	s.routes()
	return s, nil
}

// Store returns the server's database.
func (s *Server) Store() *Store { return s.store }

// Close closes the database. Call it after the HTTP server has shut down.
func (s *Server) Close() error {
	s.broker.close()
	s.closeWorkspaces()
	return s.store.Close()
}

// Handle registers a handler on the server's mux, for routes added by
// other packages (web API, auth, HTTP MCP). It gets the request logging
// but no authentication: wrap it with RequireToken when needed.
func (s *Server) Handle(pattern string, h http.Handler) { s.mux.Handle(pattern, h) }

// Handler returns the HTTP handler of the whole server.
func (s *Server) Handler() http.Handler { return s.logged(s.cors(s.mux)) }

// handlerFunc is an API handler: it returns an error to send instead of
// writing one itself.
type handlerFunc func(w http.ResponseWriter, r *http.Request, p Principal) error

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	v1 := func(pattern string, h handlerFunc) { s.mux.Handle(pattern, s.v1(h)) }
	v1("GET /v1/vaults", s.listVaults)
	v1("POST /v1/vaults", s.createVault)
	v1("GET /v1/vaults/{vault}/changes", s.changes)
	v1("GET /v1/vaults/{vault}/files/{path...}", s.getFile)
	v1("PUT /v1/vaults/{vault}/files/{path...}", s.putFile)
	v1("DELETE /v1/vaults/{vault}/files/{path...}", s.deleteFile)
	v1("POST /v1/vaults/{vault}/moves", s.move)
	v1("GET /v1/vaults/{vault}/history/{path...}", s.history)
	v1("GET /v1/vaults/{vault}/events", s.events)
	s.authRoutes()
	s.flowRoutes()
	s.mux.Handle("/v1/", s.v1(func(http.ResponseWriter, *http.Request, Principal) error {
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: "no such route"}}
	}))
}

// reqInfo collects the fields of a request's wide log event; handlers
// add to it through the request context.
type reqInfo struct {
	user, vault, device string
	code                string
	attrs               []any
}

type ctxKey struct{}

func info(r *http.Request) *reqInfo {
	if ri, ok := r.Context().Value(ctxKey{}).(*reqInfo); ok {
		return ri
	}
	return &reqInfo{}
}

// statusWriter records the status and size of a response.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Flush lets SSE flush through the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the original writer to http.ResponseController.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// countingBody counts request bytes read.
type countingBody struct {
	r interface {
		Read([]byte) (int, error)
		Close() error
	}
	n atomic.Int64
}

func (c *countingBody) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n.Add(int64(n))
	return n, err
}

func (c *countingBody) Close() error { return c.r.Close() }

// logged wraps h with one wide log event per request: method, route,
// user, vault, status, duration and bytes in both directions.
func (s *Server) logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ri := &reqInfo{}
		body := &countingBody{r: r.Body}
		r.Body = body
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, ri))
		sw := &statusWriter{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		attrs := []any{"method", r.Method, "route", route, "path", r.URL.Path, "status", status,
			"duration", time.Since(start).Round(time.Microsecond), "bytes_in", body.n.Load(), "bytes_out", sw.bytes}
		if ri.user != "" {
			attrs = append(attrs, "user", ri.user, "device", ri.device)
		}
		if ri.vault != "" {
			attrs = append(attrs, "vault", ri.vault)
		}
		if ri.code != "" {
			attrs = append(attrs, "code", ri.code)
		}
		attrs = append(attrs, ri.attrs...)
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		}
		s.log.Log(r.Context(), level, "request", attrs...)
	})
}

// v1 checks the protocol version and the device token, then runs h and
// turns its error into a protocol error response.
func (s *Server) v1(h handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(protocol.HeaderProtocol, strconv.Itoa(protocol.Version))
		if err := checkProtocol(r); err != nil {
			s.writeErr(w, r, err)
			return
		}
		p, err := s.authenticate(r)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		ri := info(r)
		ri.user, ri.device = p.Email, p.Device
		ri.vault = r.PathValue("vault")
		if err := h(w, r, p); err != nil {
			s.writeErr(w, r, err)
		}
	})
}

func checkProtocol(r *http.Request) error {
	v := r.Header.Get(protocol.HeaderProtocol)
	if v == "" {
		v = r.URL.Query().Get(protocol.QueryProtocol)
	}
	supported := []int{protocol.Version}
	if v == "" {
		return &apiErr{http.StatusUpgradeRequired, protocol.Error{Code: protocol.CodeUnsupportedProtocol, Supported: supported,
			Message: fmt.Sprintf("missing %s header; this server speaks sync protocol %d", protocol.HeaderProtocol, protocol.Version)}}
	}
	major, err := protocol.ParseVersion(v)
	if err != nil || major != protocol.Version {
		return &apiErr{http.StatusUpgradeRequired, protocol.Error{Code: protocol.CodeUnsupportedProtocol, Supported: supported,
			Message: fmt.Sprintf("sync protocol %q is not supported by this server (it speaks %d); update Nota", v, protocol.Version)}}
	}
	return nil
}

func (s *Server) authenticate(r *http.Request) (Principal, error) {
	unauthorized := func(msg string) error {
		return &apiErr{http.StatusUnauthorized, protocol.Error{Code: protocol.CodeUnauthorized, Message: msg}}
	}
	auth := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || strings.TrimSpace(tok) == "" {
		return Principal{}, unauthorized("missing bearer token")
	}
	p, err := s.store.Authenticate(r.Context(), strings.TrimSpace(tok))
	if errors.Is(err, ErrNotFound) {
		return Principal{}, unauthorized("unknown or revoked token")
	}
	return p, err
}

// RequireToken wraps h so that it only runs for requests with a valid
// device token, for routes mounted with Handle.
func (s *Server) RequireToken(h func(http.ResponseWriter, *http.Request, Principal)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.authenticate(r)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		info(r).user = p.Email
		h(w, r, p)
	})
}

func (s *Server) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiErr
	var pe *protocol.Error
	switch {
	case errors.As(err, &ae):
	case errors.As(err, &pe):
		ae = &apiErr{http.StatusBadRequest, *pe}
	case errors.Is(err, ErrNotFound):
		ae = &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: err.Error()}}
	case errors.Is(err, ErrExists):
		ae = &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeExists, Message: err.Error()}}
	case errors.Is(err, ErrTooLarge):
		ae = &apiErr{http.StatusRequestEntityTooLarge, protocol.Error{Code: protocol.CodeTooLarge,
			Message: fmt.Sprintf("files are limited to %d bytes", s.cfg.MaxFileSize)}}
	case errors.Is(err, context.Canceled):
		ae = &apiErr{499, protocol.Error{Code: "canceled", Message: "client went away"}}
	default:
		info(r).attrs = append(info(r).attrs, "err", err.Error())
		ae = &apiErr{http.StatusInternalServerError, protocol.Error{Code: protocol.CodeInternal, Message: "internal error"}}
	}
	info(r).code = ae.body.Code
	writeJSON(w, ae.status, ae.body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// ListenAndServe serves on addr until ctx is done, then shuts down
// gracefully: new connections are refused, running requests get up to
// grace to finish and SSE streams are closed. ready, when not nil, is
// called with the bound address (useful with port 0).
func (s *Server) ListenAndServe(ctx context.Context, addr string, grace time.Duration, ready func(net.Addr)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(ctx) }}
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	if ready != nil {
		ready(ln.Addr())
	}
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	s.broker.close() // ends SSE streams so Shutdown does not wait on them
	sctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
