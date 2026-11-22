package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	wap "github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/banjo/nota/internal/protocol"
)

// SignupMode says who may create an account.
type SignupMode string

// Sign-up modes. Invite is the default: only people with an invite code
// (nota serve invite create) can sign up.
const (
	SignupInvite SignupMode = "invite"
	SignupOpen   SignupMode = "open"
	SignupClosed SignupMode = "closed"
)

// ParseSignupMode reads a sign-up mode; "" is SignupInvite.
func ParseSignupMode(s string) (SignupMode, error) {
	switch m := SignupMode(strings.ToLower(strings.TrimSpace(s))); m {
	case "":
		return SignupInvite, nil
	case SignupInvite, SignupOpen, SignupClosed:
		return m, nil
	}
	return "", fmt.Errorf("server: sign-up mode %q: want invite, open or closed", s)
}

// SessionCookie is the name of the web session cookie.
const SessionCookie = "nota_session"

// DefaultSessionTTL is how long a session lives without being used.
const DefaultSessionTTL = 30 * 24 * time.Hour

// Principal is who a request is authenticated as: a device token
// (Via "token") or a browser session (Via "session").
type Principal struct {
	UserID  int64
	Email   string
	TokenID int64
	Device  string
	// Via is "token" or "session".
	Via string
	// SessionID is set for sessions.
	SessionID int64
	// NeedsPasskey marks a session signed in with a recovery code: it
	// may only add a passkey (and sign out) until it has done so.
	NeedsPasskey bool
}

// DeviceWeb is the device name of changes made through the web app.
const DeviceWeb = "web"

// authMode is what a route requires.
type authMode int

const (
	authNone    authMode = iota // anyone
	authAny                     // a device token or a session
	authSession                 // a browser session only
)

// routeOpts tune one route of the account and web API.
type routeOpts struct {
	auth authMode
	// csrf applies the cookie CSRF rules to unauthenticated requests
	// too: the sign-in calls set the session cookie.
	csrf bool
	// limited takes a token from the client IP's rate-limit bucket.
	limited bool
	// pending lets a NeedsPasskey session through.
	pending bool
}

// route registers an account or web API handler: protocol version,
// rate limit, authentication, CSRF, then h.
func (s *Server) route(pattern string, o routeOpts, h handlerFunc) {
	s.mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(protocol.HeaderProtocol, strconv.Itoa(protocol.Version))
		w.Header().Set("Cache-Control", "no-store")
		if err := s.serveRoute(w, r, o, h); err != nil {
			s.writeErr(w, r, err)
		}
	}))
}

func (s *Server) serveRoute(w http.ResponseWriter, r *http.Request, o routeOpts, h handlerFunc) error {
	if err := checkProtocol(r); err != nil {
		return err
	}
	if o.limited {
		if err := s.rateLimit(r); err != nil {
			return err
		}
	}
	p, err := s.principal(w, r)
	if err != nil {
		return err
	}
	ri := info(r)
	if p.UserID != 0 {
		ri.user, ri.device = p.Email, p.Device
		ri.attrs = append(ri.attrs, "auth", p.Via)
	}
	ri.vault = r.PathValue("vault")
	switch {
	case p.Via == "session" || (p.Via == "" && o.csrf):
		if err := s.checkCSRF(r); err != nil {
			return err
		}
	}
	switch o.auth {
	case authAny, authSession:
		if p.UserID == 0 {
			return &apiErr{http.StatusUnauthorized, protocol.Error{Code: protocol.CodeUnauthorized, Message: "sign in first"}}
		}
		if o.auth == authSession && p.Via != "session" {
			return &apiErr{http.StatusForbidden, protocol.Error{Code: CodeForbidden, Message: "this needs a signed-in browser session"}}
		}
		if p.NeedsPasskey && !o.pending {
			return &apiErr{http.StatusForbidden, protocol.Error{Code: CodeNeedsPasskey,
				Message: "you signed in with a recovery code: add a new passkey first"}}
		}
	}
	return h(w, r, p)
}

// principal authenticates a request by bearer token, else by session
// cookie. No credentials is the zero Principal and no error; a bad token
// or cookie is 401 (a stale cookie is cleared).
func (s *Server) principal(w http.ResponseWriter, r *http.Request) (Principal, error) {
	if r.Header.Get("Authorization") != "" {
		p, err := s.authenticate(r)
		if err != nil {
			return Principal{}, err
		}
		p.Via = "token"
		return p, nil
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return Principal{}, nil
	}
	ss, touched, err := s.store.Session(r.Context(), c.Value, s.cfg.SessionTTL)
	if errors.Is(err, ErrNotFound) {
		s.clearCookie(w)
		return Principal{}, &apiErr{http.StatusUnauthorized, protocol.Error{Code: protocol.CodeUnauthorized,
			Message: "your session has expired; sign in again"}}
	}
	if err != nil {
		return Principal{}, err
	}
	if touched {
		s.setCookie(w, c.Value, ss.ExpiresAt)
	}
	return Principal{UserID: ss.UserID, Email: ss.Email, Device: DeviceWeb, Via: "session",
		SessionID: ss.ID, NeedsPasskey: ss.NeedsPasskey}, nil
}

func (s *Server) setCookie(w http.ResponseWriter, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: value, Path: "/", Domain: s.cfg.CookieDomain,
		Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true,
		Secure: !s.cfg.CookieInsecure, SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", Domain: s.cfg.CookieDomain,
		MaxAge: -1, HttpOnly: true, Secure: !s.cfg.CookieInsecure, SameSite: http.SameSiteLaxMode})
}

// startSession signs the browser in.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64, needsPasskey bool) error {
	secret, ss, err := s.store.CreateSession(r.Context(), userID, s.cfg.SessionTTL, needsPasskey, r.UserAgent())
	if err != nil {
		return err
	}
	s.setCookie(w, secret, ss.ExpiresAt)
	return nil
}

// decodeJSON reads a small JSON request body.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return badRequest("bad JSON: %v", err)
	}
	return nil
}

// waUser adapts an account to go-webauthn.
type waUser struct {
	handle []byte
	email  string
	creds  []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.handle }
func (u *waUser) WebAuthnName() string                       { return u.email }
func (u *waUser) WebAuthnDisplayName() string                { return u.email }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *Server) loadWAUser(ctx context.Context, u User) (*waUser, error) {
	handle, err := s.store.webAuthnID(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	creds, err := s.store.credentials(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return &waUser{handle: handle, email: u.Email, creds: creds}, nil
}

func (s *Server) needWebAuthn() error {
	if s.wa == nil {
		return &apiErr{http.StatusServiceUnavailable, protocol.Error{Code: CodeForbidden,
			Message: "passkeys are not configured on this server (set NOTA_SERVER_RP_ID and NOTA_SERVER_WEB_ORIGIN)"}}
	}
	return nil
}

// webAuthnErr turns a failed ceremony into a 400 with the library's
// reason (no secrets are in it).
func webAuthnErr(r *http.Request, err error) error {
	msg := err.Error()
	var pe *wap.Error
	if errors.As(err, &pe) && pe.Details != "" {
		msg = pe.Details
		info(r).attrs = append(info(r).attrs, "webauthn_error", pe.Type, "webauthn_info", pe.DevInfo)
	} else {
		info(r).attrs = append(info(r).attrs, "webauthn_error", msg)
	}
	return &apiErr{http.StatusBadRequest, protocol.Error{Code: CodeWebAuthn, Message: "passkey check failed: " + msg}}
}

func expired(what string) error {
	return &apiErr{http.StatusBadRequest, protocol.Error{Code: CodeExpired, Message: what + " expired or was already used; start again"}}
}

func (s *Server) authRoutes() {
	pub := routeOpts{csrf: true, limited: true}
	s.route("GET /v1/auth/config", routeOpts{}, s.authConfig)
	s.route("POST /v1/auth/register/begin", pub, s.registerBegin)
	s.route("POST /v1/auth/register/finish", pub, s.registerFinish)
	s.route("POST /v1/auth/login/begin", pub, s.loginBegin)
	s.route("POST /v1/auth/login/finish", pub, s.loginFinish)
	s.route("POST /v1/auth/recovery", pub, s.recoveryLogin)
	s.route("POST /v1/auth/logout", routeOpts{auth: authSession, pending: true}, s.logout)
	s.route("GET /v1/auth/me", routeOpts{auth: authAny, pending: true}, s.me)
	s.route("GET /v1/auth/passkeys", routeOpts{auth: authAny}, s.listPasskeys)
	s.route("POST /v1/auth/passkeys/begin", routeOpts{auth: authSession, pending: true, limited: true}, s.passkeyBegin)
	s.route("POST /v1/auth/passkeys/finish", routeOpts{auth: authSession, pending: true, limited: true}, s.passkeyFinish)
	s.route("DELETE /v1/auth/passkeys/{id}", routeOpts{auth: authSession}, s.deletePasskey)
	s.route("POST /v1/auth/recovery-codes", routeOpts{auth: authSession, limited: true}, s.regenerateCodes)
}

// AuthConfig is the body of GET /v1/auth/config, for the web app.
type AuthConfig struct {
	Signup   SignupMode `json:"signup"`
	RPID     string     `json:"rp_id"`
	Passkeys bool       `json:"passkeys"`
}

func (s *Server) authConfig(w http.ResponseWriter, _ *http.Request, _ Principal) error {
	writeJSON(w, http.StatusOK, AuthConfig{Signup: s.cfg.Signup, RPID: s.cfg.RPID, Passkeys: s.wa != nil})
	return nil
}

// UserInfo is the account as the API shows it.
type UserInfo struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func userInfo(u User) UserInfo { return UserInfo{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt} }

// BeginRequest starts a registration or login ceremony.
type BeginRequest struct {
	Email  string `json:"email"`
	Invite string `json:"invite,omitempty"`
}

// BeginResponse carries the WebAuthn options for the browser
// (navigator.credentials.create/get) and the ceremony id to send back.
type BeginResponse struct {
	Ceremony string `json:"ceremony"`
	Options  any    `json:"options"`
}

// FinishRequest completes a ceremony with the browser's credential
// (PublicKeyCredential.toJSON()).
type FinishRequest struct {
	Ceremony   string          `json:"ceremony"`
	Credential json.RawMessage `json:"credential"`
	// Name labels a new passkey ("MacBook", "iPhone").
	Name string `json:"name,omitempty"`
}

// registerData is what a registration ceremony keeps.
type registerData struct {
	Email    string               `json:"email"`
	UserID   int64                `json:"user_id"` // an existing user without passkeys, else 0
	InviteID int64                `json:"invite_id"`
	Handle   []byte               `json:"handle"`
	Session  webauthn.SessionData `json:"session"`
}

var selection = wap.AuthenticatorSelection{
	ResidentKey:      wap.ResidentKeyRequirementRequired,
	UserVerification: wap.VerificationRequired,
}

func (s *Server) registerBegin(w http.ResponseWriter, r *http.Request, _ Principal) error {
	if err := s.needWebAuthn(); err != nil {
		return err
	}
	var req BeginRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	email, err := NormalizeEmail(req.Email)
	if err != nil {
		return badRequest("%v", err)
	}
	ctx := r.Context()
	d := registerData{Email: email}
	u, err := s.store.UserByEmail(ctx, email)
	switch {
	case err == nil:
		// A user made by `nota serve user add` claims the account with
		// the invite bound to their email, whatever the sign-up mode.
		creds, err := s.store.credentials(ctx, u.ID)
		if err != nil {
			return err
		}
		if len(creds) > 0 {
			return &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeExists, Message: "this email already has an account; sign in instead"}}
		}
		if d.InviteID, err = s.inviteFor(ctx, req.Invite, email, true); err != nil {
			return err
		}
		d.UserID = u.ID
		if d.Handle, err = s.store.webAuthnID(ctx, u.ID); err != nil {
			return err
		}
	case errors.Is(err, ErrNotFound):
		switch s.cfg.Signup {
		case SignupClosed:
			return &apiErr{http.StatusForbidden, protocol.Error{Code: CodeSignupClosed, Message: "sign-up is closed on this server"}}
		case SignupInvite:
			if d.InviteID, err = s.inviteFor(ctx, req.Invite, email, true); err != nil {
				return err
			}
		case SignupOpen:
			if req.Invite != "" {
				if d.InviteID, err = s.inviteFor(ctx, req.Invite, email, false); err != nil {
					return err
				}
			}
		}
		d.Handle = randomBytes(32)
	default:
		return err
	}
	opts, sess, err := s.wa.BeginRegistration(&waUser{handle: d.Handle, email: email},
		webauthn.WithAuthenticatorSelection(selection), webauthn.WithConveyancePreference(wap.PreferNoAttestation))
	if err != nil {
		return err
	}
	d.Session = *sess
	id, err := s.store.saveCeremony(ctx, "register", 0, d)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "email", email, "claim", d.UserID != 0)
	writeJSON(w, http.StatusOK, BeginResponse{Ceremony: id, Options: opts})
	return nil
}

// inviteFor checks an invite code for email. required makes a missing
// code an error.
func (s *Server) inviteFor(ctx context.Context, code, email string, required bool) (int64, error) {
	if strings.TrimSpace(code) == "" {
		if !required {
			return 0, nil
		}
		return 0, &apiErr{http.StatusForbidden, protocol.Error{Code: CodeInviteNeeded, Message: "sign-up needs an invite code"}}
	}
	id, err := s.store.checkInvite(ctx, s.store.db, code, email)
	switch {
	case errors.Is(err, ErrNotFound):
		return 0, &apiErr{http.StatusForbidden, protocol.Error{Code: CodeBadInvite, Message: "the invite code is not valid for this email"}}
	case errors.Is(err, ErrExpired):
		return 0, &apiErr{http.StatusForbidden, protocol.Error{Code: CodeBadInvite, Message: "the invite has expired or was already used"}}
	}
	return id, err
}

// RegisterResponse is the result of a sign-up: the account and its
// recovery codes, shown this one time only.
type RegisterResponse struct {
	User          UserInfo `json:"user"`
	RecoveryCodes []string `json:"recovery_codes"`
}

func (s *Server) registerFinish(w http.ResponseWriter, r *http.Request, _ Principal) error {
	if err := s.needWebAuthn(); err != nil {
		return err
	}
	var req FinishRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var d registerData
	if err := s.store.takeCeremony(ctx, req.Ceremony, "register", 0, &d); err != nil {
		if errors.Is(err, ErrExpired) {
			return expired("the sign-up")
		}
		return err
	}
	parsed, err := wap.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		return webAuthnErr(r, err)
	}
	cred, err := s.wa.CreateCredential(&waUser{handle: d.Handle, email: d.Email}, d.Session, parsed)
	if err != nil {
		return webAuthnErr(r, err)
	}
	u, codes, err := s.store.createAccount(ctx, d, cred, passkeyName(req.Name, r))
	if err != nil {
		return err
	}
	if err := s.startSession(w, r, u.ID, false); err != nil {
		return err
	}
	ri := info(r)
	ri.user = u.Email
	ri.attrs = append(ri.attrs, "signup", true, "claim", d.UserID != 0)
	writeJSON(w, http.StatusOK, RegisterResponse{User: userInfo(u), RecoveryCodes: codes})
	return nil
}

// createAccount finishes a sign-up in one transaction: the user (or the
// claimed one), the spent invite, the passkey and the recovery codes.
func (s *Store) createAccount(ctx context.Context, d registerData, cred *webauthn.Credential, name string) (User, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, nil, err
	}
	defer tx.Rollback()
	now := fromMS(ms(s.now()))
	u := User{ID: d.UserID, Email: d.Email, CreatedAt: now}
	if u.ID == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO users (email, created_at, webauthn_id) VALUES (?, ?, ?)
			ON CONFLICT (email) DO NOTHING RETURNING id`, d.Email, ms(now), d.Handle).Scan(&u.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, nil, &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeExists, Message: "this email already has an account"}}
		}
		if err != nil {
			return User{}, nil, err
		}
	} else {
		var n int
		var created int64
		if err := tx.QueryRowContext(ctx, `SELECT created_at, (SELECT count(*) FROM credentials WHERE user_id = ?) FROM users WHERE id = ?`,
			u.ID, u.ID).Scan(&created, &n); err != nil {
			return User{}, nil, err
		}
		if n > 0 {
			return User{}, nil, &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeExists, Message: "this account already has a passkey"}}
		}
		u.CreatedAt = fromMS(created)
	}
	if d.InviteID != 0 {
		res, err := tx.ExecContext(ctx, `UPDATE invites SET used_at = ?, used_by = ? WHERE id = ? AND used_at IS NULL AND expires_at > ?`,
			ms(now), u.ID, d.InviteID, ms(now))
		if err != nil {
			return User{}, nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return User{}, nil, &apiErr{http.StatusForbidden, protocol.Error{Code: CodeBadInvite, Message: "the invite has expired or was already used"}}
		}
	}
	if err := addCredential(ctx, tx, u.ID, cred, name, now); err != nil {
		return User{}, nil, err
	}
	codes, err := newRecoveryCodes(ctx, tx, u.ID, now)
	if err != nil {
		return User{}, nil, err
	}
	return u, codes, tx.Commit()
}

func passkeyName(name string, r *http.Request) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "passkey"
		if ua := r.UserAgent(); ua != "" {
			name = "passkey (" + firstWord(ua) + ")"
		}
	}
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " /"); i > 0 {
		return s[:i]
	}
	return s
}

// loginData is what a login ceremony keeps.
type loginData struct {
	UserID  int64                `json:"user_id"` // 0 for a discoverable (usernameless) login
	Session webauthn.SessionData `json:"session"`
}

func (s *Server) loginBegin(w http.ResponseWriter, r *http.Request, _ Principal) error {
	if err := s.needWebAuthn(); err != nil {
		return err
	}
	var req BeginRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			return err
		}
	}
	ctx := r.Context()
	var d loginData
	var opts *wap.CredentialAssertion
	var sess *webauthn.SessionData
	var err error
	uv := webauthn.WithUserVerification(wap.VerificationRequired)
	// With a known email the browser is told which passkeys fit; an
	// unknown one gets the same usernameless options as no email, so the
	// answer does not reveal whether an account exists.
	var wu *waUser
	if req.Email != "" {
		if u, err := s.store.UserByEmail(ctx, req.Email); err == nil {
			if wu, err = s.loadWAUser(ctx, u); err != nil {
				return err
			}
			if len(wu.creds) == 0 {
				wu = nil
			} else {
				d.UserID = u.ID
			}
		}
	}
	if wu != nil {
		opts, sess, err = s.wa.BeginLogin(wu, uv)
	} else {
		opts, sess, err = s.wa.BeginDiscoverableLogin(uv)
	}
	if err != nil {
		return err
	}
	d.Session = *sess
	id, err := s.store.saveCeremony(ctx, "login", 0, d)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, BeginResponse{Ceremony: id, Options: opts})
	return nil
}

// LoginResponse is the result of a sign-in.
type LoginResponse struct {
	User UserInfo `json:"user"`
	// NeedsPasskey is set after a recovery-code sign-in: add a passkey
	// (POST /v1/auth/passkeys/begin) before anything else.
	NeedsPasskey bool `json:"needs_passkey,omitempty"`
	// RecoveryCodesLeft counts the unused recovery codes.
	RecoveryCodesLeft int `json:"recovery_codes_left"`
}

func (s *Server) loginFinish(w http.ResponseWriter, r *http.Request, _ Principal) error {
	if err := s.needWebAuthn(); err != nil {
		return err
	}
	var req FinishRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var d loginData
	if err := s.store.takeCeremony(ctx, req.Ceremony, "login", 0, &d); err != nil {
		if errors.Is(err, ErrExpired) {
			return expired("the sign-in")
		}
		return err
	}
	parsed, err := wap.ParseCredentialRequestResponseBytes(req.Credential)
	if err != nil {
		return webAuthnErr(r, err)
	}
	var u User
	var cred *webauthn.Credential
	if d.UserID != 0 {
		if u, err = s.store.UserByID(ctx, d.UserID); err != nil {
			return err
		}
		wu, err := s.loadWAUser(ctx, u)
		if err != nil {
			return err
		}
		if cred, err = s.wa.ValidateLogin(wu, d.Session, parsed); err != nil {
			return webAuthnErr(r, err)
		}
	} else {
		_, cred, err = s.wa.ValidatePasskeyLogin(func(_, handle []byte) (webauthn.User, error) {
			found, err := s.store.userByWebAuthnID(ctx, handle)
			if err != nil {
				return nil, fmt.Errorf("unknown passkey")
			}
			u = found
			return s.loadWAUser(ctx, found)
		}, d.Session, parsed)
		if err != nil {
			return webAuthnErr(r, err)
		}
	}
	if cred.Authenticator.CloneWarning {
		// The signature counter went backwards: the key may have been
		// copied. Refuse and leave a trace.
		info(r).attrs = append(info(r).attrs, "clone_warning", true)
		return &apiErr{http.StatusForbidden, protocol.Error{Code: CodeWebAuthn,
			Message: "this passkey's signature counter went backwards, which can mean it was cloned; use another passkey or a recovery code"}}
	}
	if err := s.store.usedCredential(ctx, u.ID, cred); err != nil {
		return err
	}
	if err := s.startSession(w, r, u.ID, false); err != nil {
		return err
	}
	left, err := s.store.RecoveryCodesLeft(ctx, u.ID)
	if err != nil {
		return err
	}
	info(r).user = u.Email
	writeJSON(w, http.StatusOK, LoginResponse{User: userInfo(u), RecoveryCodesLeft: left})
	return nil
}

// RecoveryRequest signs in with a recovery code.
type RecoveryRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (s *Server) recoveryLogin(w http.ResponseWriter, r *http.Request, _ Principal) error {
	var req RecoveryRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	u, err := s.store.UseRecoveryCode(ctx, req.Email, req.Code)
	if errors.Is(err, ErrNotFound) {
		return &apiErr{http.StatusUnauthorized, protocol.Error{Code: CodeBadCredential, Message: "wrong email or recovery code"}}
	}
	if err != nil {
		return err
	}
	if err := s.startSession(w, r, u.ID, true); err != nil {
		return err
	}
	left, err := s.store.RecoveryCodesLeft(ctx, u.ID)
	if err != nil {
		return err
	}
	ri := info(r)
	ri.user = u.Email
	ri.attrs = append(ri.attrs, "recovery_code", true, "recovery_codes_left", left)
	writeJSON(w, http.StatusOK, LoginResponse{User: userInfo(u), NeedsPasskey: true, RecoveryCodesLeft: left})
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := s.store.DeleteSession(r.Context(), p.SessionID); err != nil {
		return err
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// MeResponse is the body of GET /v1/auth/me.
type MeResponse struct {
	User              UserInfo `json:"user"`
	Via               string   `json:"via"`
	Device            string   `json:"device,omitempty"`
	NeedsPasskey      bool     `json:"needs_passkey"`
	Passkeys          int      `json:"passkeys"`
	RecoveryCodesLeft int      `json:"recovery_codes_left"`
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, p Principal) error {
	ctx := r.Context()
	u, err := s.store.UserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	pks, err := s.store.Passkeys(ctx, p.UserID)
	if err != nil {
		return err
	}
	left, err := s.store.RecoveryCodesLeft(ctx, p.UserID)
	if err != nil {
		return err
	}
	out := MeResponse{User: userInfo(u), Via: p.Via, NeedsPasskey: p.NeedsPasskey, Passkeys: len(pks), RecoveryCodesLeft: left}
	if p.Via == "token" {
		out.Device = p.Device
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request, p Principal) error {
	pks, err := s.store.Passkeys(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": pks})
	return nil
}

func (s *Server) passkeyBegin(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := s.needWebAuthn(); err != nil {
		return err
	}
	ctx := r.Context()
	u, err := s.store.UserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	wu, err := s.loadWAUser(ctx, u)
	if err != nil {
		return err
	}
	excl := make([]wap.CredentialDescriptor, len(wu.creds))
	for i := range wu.creds {
		excl[i] = wu.creds[i].Descriptor()
	}
	opts, sess, err := s.wa.BeginRegistration(wu, webauthn.WithAuthenticatorSelection(selection),
		webauthn.WithExclusions(excl), webauthn.WithConveyancePreference(wap.PreferNoAttestation))
	if err != nil {
		return err
	}
	id, err := s.store.saveCeremony(ctx, "passkey", p.UserID, sess)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, BeginResponse{Ceremony: id, Options: opts})
	return nil
}

func (s *Server) passkeyFinish(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := s.needWebAuthn(); err != nil {
		return err
	}
	var req FinishRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var sess webauthn.SessionData
	if err := s.store.takeCeremony(ctx, req.Ceremony, "passkey", p.UserID, &sess); err != nil {
		if errors.Is(err, ErrExpired) {
			return expired("adding the passkey")
		}
		return err
	}
	u, err := s.store.UserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	wu, err := s.loadWAUser(ctx, u)
	if err != nil {
		return err
	}
	parsed, err := wap.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		return webAuthnErr(r, err)
	}
	cred, err := s.wa.CreateCredential(wu, sess, parsed)
	if err != nil {
		return webAuthnErr(r, err)
	}
	if err := s.store.AddPasskey(ctx, p.UserID, cred, passkeyName(req.Name, r)); err != nil {
		return err
	}
	if p.NeedsPasskey {
		if err := s.store.passkeyAdded(ctx, p.SessionID); err != nil {
			return err
		}
	}
	pks, err := s.store.Passkeys(ctx, p.UserID)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "passkeys", len(pks))
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": pks})
	return nil
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return badRequest("bad passkey id %q", r.PathValue("id"))
	}
	err = s.store.DeletePasskey(r.Context(), p.UserID, id)
	if errors.Is(err, errLastPasskey) {
		return &apiErr{http.StatusConflict, protocol.Error{Code: CodeLastPasskey, Message: err.Error()}}
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) regenerateCodes(w http.ResponseWriter, r *http.Request, p Principal) error {
	codes, err := s.store.RegenerateRecoveryCodes(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
	return nil
}
