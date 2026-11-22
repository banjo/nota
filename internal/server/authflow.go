package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

// This file has the device sign-in flows (see protocol/auth.go): the
// desktop's one-time code bound to a PKCE challenge, the CLI's device
// code with polling, and the devices list.

// Lifetimes of sign-in requests.
const (
	// ConnectCodeTTL is how long a desktop one-time code is valid.
	ConnectCodeTTL = 2 * time.Minute
	// DeviceCodeTTL is how long a CLI device code waits for approval.
	DeviceCodeTTL = 10 * time.Minute
	// DefaultPollInterval is the device-code poll interval.
	DefaultPollInterval = 5 * time.Second
)

// DefaultRedirectURI is the desktop app's deep link.
const DefaultRedirectURI = "nota://auth"

// tokenErr is an OAuth-style error of the token endpoint.
func tokenErr(code, msg string, interval int) error {
	return &apiErr{http.StatusBadRequest, protocol.Error{Code: code, Message: msg, Interval: interval}}
}

// s256 is the PKCE S256 transform of a verifier.
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func validVerifier(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
			return false
		}
	}
	return true
}

func cleanDevice(d string) (string, error) {
	d = strings.TrimSpace(d)
	if d == "" || len(d) > 64 || strings.ContainsAny(d, "\r\n\t") {
		return "", badRequest("give a device name of 1-64 characters")
	}
	return d, nil
}

// --- store -----------------------------------------------------------

// createConnectCode stores a desktop one-time code for a user, bound to
// a PKCE challenge, and returns it.
func (s *Store) createConnectCode(ctx context.Context, userID int64, challenge, device string) (string, error) {
	code := randomSecret("ncode_", 32)
	now := s.now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_requests (kind, code_hash, challenge, device, user_id, status, created_at, expires_at)
		VALUES ('pkce', ?, ?, ?, ?, 'approved', ?, ?)`, sha(code), challenge, device, userID, ms(now), ms(now.Add(ConnectCodeTTL)))
	return code, err
}

// exchangeCode spends a one-time code with its verifier and issues a
// device token. A code works once, also when the verifier was wrong.
func (s *Store) exchangeCode(ctx context.Context, code, verifier string) (protocol.TokenResponse, error) {
	var id, userID, expires int64
	var challenge, device string
	err := s.db.QueryRowContext(ctx, `UPDATE auth_requests SET status = 'used'
		WHERE code_hash = ? AND kind = 'pkce' AND status = 'approved'
		RETURNING id, user_id, challenge, device, expires_at`, sha(code)).Scan(&id, &userID, &challenge, &device, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.TokenResponse{}, tokenErr(protocol.CodeInvalidGrant, "the sign-in code is unknown or was already used", 0)
	}
	if err != nil {
		return protocol.TokenResponse{}, err
	}
	if !s.now().Before(fromMS(expires)) {
		return protocol.TokenResponse{}, tokenErr(protocol.CodeInvalidGrant, "the sign-in code has expired; sign in again", 0)
	}
	if subtle.ConstantTimeCompare([]byte(s256(verifier)), []byte(challenge)) != 1 {
		return protocol.TokenResponse{}, tokenErr(protocol.CodeInvalidGrant, "the code verifier does not match the challenge", 0)
	}
	return s.issue(ctx, userID, device)
}

// issue creates a device token for a user.
func (s *Store) issue(ctx context.Context, userID int64, device string) (protocol.TokenResponse, error) {
	u, err := s.UserByID(ctx, userID)
	if err != nil {
		return protocol.TokenResponse{}, err
	}
	secret, t, err := s.CreateToken(ctx, userID, device)
	if err != nil {
		return protocol.TokenResponse{}, err
	}
	return protocol.TokenResponse{Token: secret, TokenType: "bearer", TokenID: t.ID, Device: t.Device, Email: u.Email}, nil
}

// createDeviceAuth starts a CLI sign-in and returns the device code and
// the user code.
func (s *Store) createDeviceAuth(ctx context.Context, device string, interval time.Duration) (deviceCode, userCode string, err error) {
	now := s.now()
	for range 5 { // user codes are random; retry the rare collision
		deviceCode, userCode = randomSecret("ndev_", 32), humanCode(8)
		_, err = s.db.ExecContext(ctx, `INSERT INTO auth_requests (kind, code_hash, user_code_hash, device, status, interval_s,
			created_at, expires_at) VALUES ('device', ?, ?, ?, 'pending', ?, ?, ?)`,
			sha(deviceCode), sha(normalizeCode(userCode)), device, int(interval.Seconds()), ms(now), ms(now.Add(DeviceCodeTTL)))
		if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
			return deviceCode, userCode, err
		}
	}
	return "", "", err
}

// DeviceAuth is a pending CLI sign-in, as the approval page shows it.
type DeviceAuth struct {
	ID        int64     `json:"id"`
	Device    string    `json:"device"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// pendingDeviceAuth finds an unexpired pending sign-in by user code.
func (s *Store) pendingDeviceAuth(ctx context.Context, userCode string) (DeviceAuth, error) {
	var d DeviceAuth
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT id, device, created_at, expires_at FROM auth_requests
		WHERE user_code_hash = ? AND kind = 'device' AND status = 'pending' AND expires_at > ?`,
		sha(normalizeCode(userCode)), ms(s.now())).Scan(&d.ID, &d.Device, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return DeviceAuth{}, ErrNotFound
	}
	d.CreatedAt, d.ExpiresAt = fromMS(created), fromMS(expires)
	return d, err
}

// PendingDeviceAuths lists the unexpired pending CLI sign-ins, newest
// first (for `nota serve auth approve`).
func (s *Store) PendingDeviceAuths(ctx context.Context) ([]DeviceAuth, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, device, created_at, expires_at FROM auth_requests
		WHERE kind = 'device' AND status = 'pending' AND expires_at > ? ORDER BY id DESC`, ms(s.now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceAuth{}
	for rows.Next() {
		var d DeviceAuth
		var created, expires int64
		if err := rows.Scan(&d.ID, &d.Device, &created, &expires); err != nil {
			return nil, err
		}
		d.CreatedAt, d.ExpiresAt = fromMS(created), fromMS(expires)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DecideDeviceAuth approves (or denies) a pending CLI sign-in for a
// user, by request id.
func (s *Store) DecideDeviceAuth(ctx context.Context, id, userID int64, approve bool) error {
	status := "denied"
	if approve {
		status = "approved"
	}
	res, err := s.db.ExecContext(ctx, `UPDATE auth_requests SET status = ?, user_id = ?
		WHERE id = ? AND kind = 'device' AND status = 'pending' AND expires_at > ?`, status, userID, id, ms(s.now()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrExpired
	}
	return nil
}

// pollDeviceAuth is one poll of a device code: pending (slowing down
// clients that poll too fast), denied, expired, or a fresh token.
func (s *Store) pollDeviceAuth(ctx context.Context, deviceCode string) (protocol.TokenResponse, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.TokenResponse{}, err
	}
	defer tx.Rollback()
	var id, expires int64
	var userID, lastPoll sql.NullInt64
	var status, device string
	var interval int
	err = tx.QueryRowContext(ctx, `SELECT id, user_id, status, device, interval_s, last_poll_at, expires_at
		FROM auth_requests WHERE code_hash = ? AND kind = 'device'`, sha(deviceCode)).
		Scan(&id, &userID, &status, &device, &interval, &lastPoll, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.TokenResponse{}, tokenErr(protocol.CodeInvalidGrant, "unknown device code", 0)
	}
	if err != nil {
		return protocol.TokenResponse{}, err
	}
	now := s.now()
	if !now.Before(fromMS(expires)) || status == "used" {
		return protocol.TokenResponse{}, tokenErr(protocol.CodeExpiredToken, "the sign-in request expired; run nota login again", 0)
	}
	switch status {
	case "denied":
		_, _ = tx.ExecContext(ctx, `UPDATE auth_requests SET status = 'used' WHERE id = ?`, id)
		_ = tx.Commit()
		return protocol.TokenResponse{}, tokenErr(protocol.CodeAccessDenied, "the sign-in was denied in the browser", 0)
	case "pending":
		code, msg := protocol.CodeAuthorizationPending, "waiting for approval in the browser"
		// RFC 8628 §3.5: polling faster than the interval earns a
		// slow_down and a longer interval.
		if lastPoll.Valid && now.Sub(fromMS(lastPoll.Int64)) < time.Duration(interval)*time.Second-200*time.Millisecond {
			interval += 5
			code, msg = protocol.CodeSlowDown, fmt.Sprintf("polling too fast; wait %d s between polls", interval)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE auth_requests SET last_poll_at = ?, interval_s = ? WHERE id = ?`,
			ms(now), interval, id); err != nil {
			return protocol.TokenResponse{}, err
		}
		if err := tx.Commit(); err != nil {
			return protocol.TokenResponse{}, err
		}
		return protocol.TokenResponse{}, tokenErr(code, msg, interval)
	}
	// approved
	if _, err := tx.ExecContext(ctx, `UPDATE auth_requests SET status = 'used' WHERE id = ?`, id); err != nil {
		return protocol.TokenResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.TokenResponse{}, err
	}
	return s.issue(ctx, userID.Int64, device)
}

// RevokeUserToken revokes one of a user's own tokens. Another user's
// token is ErrNotFound, like a missing one.
func (s *Store) RevokeUserToken(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tokens SET revoked_at = coalesce(revoked_at, ?) WHERE id = ? AND user_id = ?`,
		ms(s.now()), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("device %d: %w", id, ErrNotFound)
	}
	return nil
}

// --- handlers --------------------------------------------------------

func (s *Server) flowRoutes() {
	s.route("POST /v1/auth/device", routeOpts{limited: true}, s.deviceStart)
	s.route("POST /v1/auth/device/lookup", routeOpts{auth: authSession, limited: true}, s.deviceLookup)
	s.route("POST /v1/auth/device/approve", routeOpts{auth: authSession, limited: true}, s.deviceApprove)
	s.route("POST /v1/auth/connect", routeOpts{auth: authSession, limited: true}, s.connect)
	s.route("POST /v1/auth/token", routeOpts{}, s.token)
	s.route("GET /v1/devices", routeOpts{auth: authAny}, s.devices)
	s.route("DELETE /v1/devices/current", routeOpts{auth: authAny}, s.revokeCurrent)
	s.route("DELETE /v1/devices/{id}", routeOpts{auth: authAny}, s.revokeDevice)
}

// connectURL is the web app's sign-in page for devices.
func (s *Server) connectURL() string {
	base := s.cfg.WebURL
	if base == "" {
		base = s.cfg.PublicURL
	}
	return base + "/app/connect"
}

func (s *Server) deviceStart(w http.ResponseWriter, r *http.Request, _ Principal) error {
	var req protocol.DeviceAuthRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	device, err := cleanDevice(req.Device)
	if err != nil {
		return err
	}
	interval := s.cfg.PollInterval
	dc, uc, err := s.store.createDeviceAuth(r.Context(), device, interval)
	if err != nil {
		return err
	}
	u := s.connectURL()
	info(r).attrs = append(info(r).attrs, "sign_in_device", device)
	writeJSON(w, http.StatusOK, protocol.DeviceAuthorization{DeviceCode: dc, UserCode: uc, VerificationURI: u,
		VerificationURIComplete: u + "?code=" + url.QueryEscape(uc), ExpiresIn: int(DeviceCodeTTL.Seconds()),
		Interval: int(interval.Seconds())})
	return nil
}

// UserCodeRequest names a CLI sign-in by its user code.
type UserCodeRequest struct {
	UserCode string `json:"user_code"`
	// Approve is false to deny; true when missing.
	Approve *bool `json:"approve,omitempty"`
}

func (s *Server) deviceLookup(w http.ResponseWriter, r *http.Request, _ Principal) error {
	var req UserCodeRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	d, err := s.store.pendingDeviceAuth(r.Context(), req.UserCode)
	if errors.Is(err, ErrNotFound) {
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: "no pending sign-in with that code; it may have expired"}}
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) deviceApprove(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req UserCodeRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	d, err := s.store.pendingDeviceAuth(ctx, req.UserCode)
	if errors.Is(err, ErrNotFound) {
		return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: "no pending sign-in with that code; it may have expired"}}
	}
	if err != nil {
		return err
	}
	approve := req.Approve == nil || *req.Approve
	if err := s.store.DecideDeviceAuth(ctx, d.ID, p.UserID, approve); err != nil {
		if errors.Is(err, ErrExpired) {
			return expired("the sign-in request")
		}
		return err
	}
	info(r).attrs = append(info(r).attrs, "sign_in_device", d.Device, "approved", approve)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// allowedRedirect reports whether the desktop may be sent to u: the
// nota:// deep link or a loopback address (RFC 8252 §7.3).
func allowedRedirect(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.User != nil {
		return nil, false
	}
	switch u.Scheme {
	case "nota":
		return u, u.Host == "auth" && (u.Path == "" || u.Path == "/")
	case "http":
		h := u.Hostname()
		return u, h == "127.0.0.1" || h == "::1" || h == "localhost"
	}
	return nil, false
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req protocol.ConnectRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.CodeChallengeMethod != "S256" {
		return badRequest("code_challenge_method must be S256")
	}
	if b, err := base64.RawURLEncoding.DecodeString(req.CodeChallenge); err != nil || len(b) != sha256.Size {
		return badRequest("code_challenge must be base64url(SHA-256(code_verifier)) without padding")
	}
	device, err := cleanDevice(req.Device)
	if err != nil {
		return err
	}
	if req.RedirectURI == "" {
		req.RedirectURI = DefaultRedirectURI
	}
	u, ok := allowedRedirect(req.RedirectURI)
	if !ok {
		return badRequest("redirect_uri must be %s or a loopback http URL", DefaultRedirectURI)
	}
	code, err := s.store.createConnectCode(r.Context(), p.UserID, req.CodeChallenge, device)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("code", code)
	if req.State != "" {
		q.Set("state", req.State)
	}
	u.RawQuery = q.Encode()
	info(r).attrs = append(info(r).attrs, "sign_in_device", device)
	writeJSON(w, http.StatusOK, protocol.ConnectResponse{Code: code, Redirect: u.String(), ExpiresIn: int(ConnectCodeTTL.Seconds())})
	return nil
}

func (s *Server) token(w http.ResponseWriter, r *http.Request, _ Principal) error {
	var req protocol.TokenRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var res protocol.TokenResponse
	var err error
	switch req.GrantType {
	case protocol.GrantAuthorizationCode:
		if err := s.rateLimit(r); err != nil {
			return err
		}
		if !validVerifier(req.CodeVerifier) {
			return tokenErr(protocol.CodeInvalidGrant, "code_verifier must be 43-128 characters of [A-Za-z0-9-._~]", 0)
		}
		res, err = s.store.exchangeCode(ctx, req.Code, req.CodeVerifier)
	case protocol.GrantDeviceCode:
		// Polling is paced by the interval (slow_down), not the IP bucket.
		res, err = s.store.pollDeviceAuth(ctx, req.DeviceCode)
	default:
		return badRequest("grant_type must be %s or %s", protocol.GrantAuthorizationCode, protocol.GrantDeviceCode)
	}
	ri := info(r)
	ri.attrs = append(ri.attrs, "grant", req.GrantType)
	if err != nil {
		return err
	}
	ri.user, ri.device = res.Email, res.Device
	ri.attrs = append(ri.attrs, "token_id", res.TokenID)
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request, p Principal) error {
	ts, err := s.store.Tokens(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	out := protocol.DeviceList{Devices: []protocol.Device{}}
	for _, t := range ts {
		if t.RevokedAt != nil {
			continue
		}
		out.Devices = append(out.Devices, protocol.Device{ID: t.ID, Device: t.Device, CreatedAt: t.CreatedAt,
			LastUsedAt: t.LastUsedAt, Current: p.Via == "token" && t.ID == p.TokenID})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return badRequest("bad device id %q", r.PathValue("id"))
	}
	if err := s.store.RevokeUserToken(r.Context(), p.UserID, id); err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "revoked_token", id)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) revokeCurrent(w http.ResponseWriter, r *http.Request, p Principal) error {
	if p.Via != "token" {
		return badRequest("DELETE /v1/devices/current needs a device token; sign out a browser with POST /v1/auth/logout")
	}
	if err := s.store.RevokeUserToken(r.Context(), p.UserID, p.TokenID); err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "revoked_token", p.TokenID)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
