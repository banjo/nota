package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// This file holds the account side of the store: passkeys (WebAuthn
// credentials), browser sessions, recovery codes, invites and the data
// WebAuthn ceremonies keep between begin and finish. Secrets are random,
// shown once, and stored as their SHA-256.

// randomBytes returns n random bytes.
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("server: crypto/rand failed: " + err.Error()) // never happens on supported platforms
	}
	return b
}

// randomSecret returns a URL-safe random string with n bytes of entropy.
func randomSecret(prefix string, n int) string {
	return prefix + base64.RawURLEncoding.EncodeToString(randomBytes(n))
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// humanAlphabet has no 0/O, 1/I/L: codes people type or read aloud.
const humanAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// humanCode returns a random code of n characters from humanAlphabet,
// grouped by four with dashes.
func humanCode(n int) string {
	var b strings.Builder
	// Rejection sampling keeps every symbol equally likely: bytes from
	// 248 up would favour the first symbols of the alphabet.
	limit := byte(256 - 256%len(humanAlphabet))
	for i := 0; i < n; {
		for _, x := range randomBytes(n) {
			if x >= limit || i >= n {
				continue
			}
			if i > 0 && i%4 == 0 {
				b.WriteByte('-')
			}
			b.WriteByte(humanAlphabet[int(x)%len(humanAlphabet)])
			i++
		}
	}
	return b.String()
}

// normalizeCode upper-cases a human code and drops spaces and dashes, so
// "abcd efgh" and "ABCD-EFGH" are the same code.
func normalizeCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
}

// Account errors.
var (
	// ErrExpired is returned for an invite, code or ceremony past its
	// expiry, or one that was already used.
	ErrExpired = errors.New("expired or already used")
)

// --- users -----------------------------------------------------------

// UserByID finds a user by id.
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	u := User{ID: id}
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT email, created_at FROM users WHERE id = ?`, id).Scan(&u.Email, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %d: %w", id, ErrNotFound)
	}
	u.CreatedAt = fromMS(created)
	return u, err
}

// webAuthnID returns the user's WebAuthn user handle, creating it on
// first use.
func (s *Store) webAuthnID(ctx context.Context, userID int64) ([]byte, error) {
	var id []byte
	if err := s.db.QueryRowContext(ctx, `SELECT webauthn_id FROM users WHERE id = ?`, userID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(id) > 0 {
		return id, nil
	}
	id = randomBytes(32)
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET webauthn_id = ? WHERE id = ? AND webauthn_id IS NULL`, id, userID); err != nil {
		return nil, err
	}
	// Another request may have won the race: read back what is stored.
	err := s.db.QueryRowContext(ctx, `SELECT webauthn_id FROM users WHERE id = ?`, userID).Scan(&id)
	return id, err
}

// userByWebAuthnID finds the user of a WebAuthn user handle.
func (s *Store) userByWebAuthnID(ctx context.Context, handle []byte) (User, error) {
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, email, created_at FROM users WHERE webauthn_id = ?`, handle).
		Scan(&u.ID, &u.Email, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.CreatedAt = fromMS(created)
	return u, err
}

// --- passkeys --------------------------------------------------------

// Passkey describes a registered WebAuthn credential.
type Passkey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// credentials returns a user's WebAuthn credentials.
func (s *Store) credentials(ctx context.Context, userID int64) ([]webauthn.Credential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM credentials WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webauthn.Credential
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return nil, fmt.Errorf("server: stored credential: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Passkeys lists a user's passkeys.
func (s *Store) Passkeys(ctx context.Context, userID int64) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_used_at FROM credentials
		WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		var created int64
		var used sql.NullInt64
		if err := rows.Scan(&p.ID, &p.Name, &created, &used); err != nil {
			return nil, err
		}
		p.CreatedAt = fromMS(created)
		if used.Valid {
			t := fromMS(used.Int64)
			p.LastUsedAt = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func addCredential(ctx context.Context, tx *sql.Tx, userID int64, c *webauthn.Credential, name string, now time.Time) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credentials (user_id, credential_id, data, name, created_at)
		VALUES (?, ?, ?, ?, ?)`, userID, c.ID, string(data), name, ms(now))
	return err
}

// AddPasskey stores a new credential for a user.
func (s *Store) AddPasskey(ctx context.Context, userID int64, c *webauthn.Credential, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := addCredential(ctx, tx, userID, c, name, s.now()); err != nil {
		return err
	}
	return tx.Commit()
}

// usedCredential records a login with a credential: its new sign count
// and flags.
func (s *Store) usedCredential(ctx context.Context, userID int64, c *webauthn.Credential) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE credentials SET data = ?, last_used_at = ? WHERE user_id = ? AND credential_id = ?`,
		string(data), ms(s.now()), userID, c.ID)
	return err
}

// DeletePasskey removes one of a user's passkeys. The last one cannot be
// removed: the account would only be reachable through recovery codes.
func (s *Store) DeletePasskey(ctx context.Context, userID, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM credentials WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM credentials WHERE user_id = ? AND id = ? AND ? > 1`, userID, id, n)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		if n <= 1 {
			return errLastPasskey
		}
		return fmt.Errorf("passkey %d: %w", id, ErrNotFound)
	}
	return tx.Commit()
}

var errLastPasskey = errors.New("cannot remove the last passkey; add another one first")

// --- recovery codes --------------------------------------------------

// RecoveryCodeCount is how many recovery codes an account gets.
const RecoveryCodeCount = 10

// newRecoveryCodes replaces a user's recovery codes inside tx and
// returns the new ones (shown once).
func newRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64, now time.Time) ([]string, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, RecoveryCodeCount)
	for i := range codes {
		codes[i] = humanCode(12) // 12 chars of 31 symbols: ~59 bits
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, code_hash, created_at) VALUES (?, ?, ?)`,
			userID, sha(normalizeCode(codes[i])), ms(now)); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// RegenerateRecoveryCodes invalidates a user's recovery codes and returns
// a new set.
func (s *Store) RegenerateRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	codes, err := newRecoveryCodes(ctx, tx, userID, s.now())
	if err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

// UseRecoveryCode spends one recovery code of the user with that email.
// Unknown emails, unknown codes and used codes all fail with ErrNotFound,
// so the answer does not tell which one it was.
func (s *Store) UseRecoveryCode(ctx context.Context, email, code string) (User, error) {
	u, err := s.UserByEmail(ctx, email)
	if err != nil {
		return User{}, ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `UPDATE recovery_codes SET used_at = ?
		WHERE id = (SELECT id FROM recovery_codes WHERE user_id = ? AND code_hash = ? AND used_at IS NULL LIMIT 1)`,
		ms(s.now()), u.ID, sha(normalizeCode(code)))
	if err != nil {
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, ErrNotFound
	}
	return u, nil
}

// RecoveryCodesLeft counts a user's unused recovery codes.
func (s *Store) RecoveryCodesLeft(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

// --- sessions --------------------------------------------------------

// Session is a signed-in browser.
type Session struct {
	ID           int64
	UserID       int64
	Email        string
	NeedsPasskey bool
	CreatedAt    time.Time
	LastSeenAt   time.Time
	ExpiresAt    time.Time
}

// SessionPrefix starts every session id.
const SessionPrefix = "nses_"

// CreateSession starts a session for a user and returns its secret id
// (the cookie value) with the session.
func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration, needsPasskey bool, userAgent string) (string, Session, error) {
	secret := randomSecret(SessionPrefix, 32)
	now := fromMS(ms(s.now()))
	ss := Session{UserID: userID, NeedsPasskey: needsPasskey, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(ttl)}
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	err := s.db.QueryRowContext(ctx, `INSERT INTO sessions (user_id, session_hash, needs_passkey, user_agent, created_at,
		last_seen_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		userID, sha(secret), needsPasskey, userAgent, ms(now), ms(now), ms(ss.ExpiresAt)).Scan(&ss.ID)
	return secret, ss, err
}

// sessionTouchEvery bounds how often a session's expiry is pushed out.
const sessionTouchEvery = 5 * time.Minute

// Session looks up a session by its secret id. Expired sessions are
// deleted and fail with ErrNotFound. A live session's expiry rolls
// forward by ttl (at most every few minutes); touched reports that, so
// the caller can refresh the cookie.
func (s *Store) Session(ctx context.Context, secret string, ttl time.Duration) (ss Session, touched bool, err error) {
	if !strings.HasPrefix(secret, SessionPrefix) {
		return Session{}, false, ErrNotFound
	}
	var created, seen, expires int64
	err = s.db.QueryRowContext(ctx, `SELECT s.id, s.user_id, u.email, s.needs_passkey, s.created_at, s.last_seen_at, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.session_hash = ?`, sha(secret)).
		Scan(&ss.ID, &ss.UserID, &ss.Email, &ss.NeedsPasskey, &created, &seen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, ErrNotFound
	}
	if err != nil {
		return Session{}, false, err
	}
	now := s.now()
	ss.CreatedAt, ss.LastSeenAt, ss.ExpiresAt = fromMS(created), fromMS(seen), fromMS(expires)
	if !now.Before(ss.ExpiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, ss.ID)
		return Session{}, false, ErrNotFound
	}
	if now.Sub(ss.LastSeenAt) >= sessionTouchEvery {
		ss.LastSeenAt, ss.ExpiresAt = fromMS(ms(now)), fromMS(ms(now.Add(ttl)))
		if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`,
			ms(ss.LastSeenAt), ms(ss.ExpiresAt), ss.ID); err != nil {
			return Session{}, false, err
		}
		touched = true
	}
	return ss, touched, nil
}

// DeleteSession ends a session.
func (s *Store) DeleteSession(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// passkeyAdded clears the "add a passkey first" flag of a session.
func (s *Store) passkeyAdded(ctx context.Context, sessionID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET needs_passkey = 0 WHERE id = ?`, sessionID)
	return err
}

// --- invites ---------------------------------------------------------

// Invite is an invite's metadata; the code is only known on creation.
type Invite struct {
	ID        int64      `json:"id"`
	Email     string     `json:"email,omitempty"`
	Note      string     `json:"note,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

// InvitePrefix starts every invite code.
const InvitePrefix = "ninv_"

// DefaultInviteTTL is how long an invite stays valid.
const DefaultInviteTTL = 7 * 24 * time.Hour

// CreateInvite makes an invite code. With email set only that address
// can sign up with it; for a user that already exists without a passkey
// (created by `nota serve user add`) it is how they register their first
// passkey.
func (s *Store) CreateInvite(ctx context.Context, email, note string, ttl time.Duration) (string, Invite, error) {
	if email != "" {
		e, err := NormalizeEmail(email)
		if err != nil {
			return "", Invite{}, err
		}
		email = e
	}
	if ttl <= 0 {
		ttl = DefaultInviteTTL
	}
	code := randomSecret(InvitePrefix, 18)
	now := fromMS(ms(s.now()))
	inv := Invite{Email: email, Note: note, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	err := s.db.QueryRowContext(ctx, `INSERT INTO invites (code_hash, email, note, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?) RETURNING id`, sha(code), email, note, ms(now), ms(inv.ExpiresAt)).Scan(&inv.ID)
	return code, inv, err
}

// Invites lists every invite, newest first.
func (s *Store) Invites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, note, created_at, expires_at, used_at FROM invites ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var inv Invite
		var created, expires int64
		var used sql.NullInt64
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Note, &created, &expires, &used); err != nil {
			return nil, err
		}
		inv.CreatedAt, inv.ExpiresAt = fromMS(created), fromMS(expires)
		if used.Valid {
			t := fromMS(used.Int64)
			inv.UsedAt = &t
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RevokeInvite deletes an unused invite.
func (s *Store) RevokeInvite(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM invites WHERE id = ? AND used_at IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("unused invite %d: %w", id, ErrNotFound)
	}
	return nil
}

// checkInvite finds a usable invite for email. It does not spend it.
func (s *Store) checkInvite(ctx context.Context, q querier, code, email string) (int64, error) {
	var id, expires int64
	var bound string
	var used sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id, email, expires_at, used_at FROM invites WHERE code_hash = ?`, sha(strings.TrimSpace(code))).
		Scan(&id, &bound, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if used.Valid || !s.now().Before(fromMS(expires)) {
		return 0, ErrExpired
	}
	if bound != "" && bound != email {
		return 0, ErrNotFound // an invite for someone else looks like a wrong code
	}
	return id, nil
}

// --- ceremonies ------------------------------------------------------

// ceremonyTTL bounds a WebAuthn ceremony (the browser's own timeout is
// shorter).
const ceremonyTTL = 10 * time.Minute

// saveCeremony stores the data of a WebAuthn ceremony and returns the
// opaque id the client sends back with the finish call.
func (s *Store) saveCeremony(ctx context.Context, kind string, userID int64, data any) (string, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	id := randomSecret("ncer_", 24)
	var uid any
	if userID != 0 {
		uid = userID
	}
	now := s.now()
	// Clean up old ceremonies on the way.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM ceremonies WHERE expires_at <= ?`, ms(now)); err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO ceremonies (id_hash, kind, user_id, data, expires_at) VALUES (?, ?, ?, ?, ?)`,
		sha(id), kind, uid, string(b), ms(now.Add(ceremonyTTL)))
	return id, err
}

// takeCeremony loads and deletes a ceremony: each one can be finished
// once. A ceremony of another kind or another user is ErrExpired.
func (s *Store) takeCeremony(ctx context.Context, id, kind string, userID int64, out any) error {
	var data string
	var uid sql.NullInt64
	var expires int64
	var k string
	err := s.db.QueryRowContext(ctx, `DELETE FROM ceremonies WHERE id_hash = ? RETURNING kind, user_id, data, expires_at`, sha(id)).
		Scan(&k, &uid, &data, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExpired
	}
	if err != nil {
		return err
	}
	if k != kind || uid.Int64 != userID || !s.now().Before(fromMS(expires)) {
		return ErrExpired
	}
	return json.Unmarshal([]byte(data), out)
}
