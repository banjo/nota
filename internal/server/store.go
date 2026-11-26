package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/banjo/nota/internal/protocol"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the server's SQLite database: users, device tokens, vaults
// and the version log. Every query that touches user data is scoped by a
// user id (vaults) or by a vault id that was looked up by user id.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Errors returned by Store methods.
var (
	ErrExists   = errors.New("already exists")
	ErrNotFound = errors.New("not found")
)

// OpenStore opens (creating if needed) the database at path and runs the
// embedded goose migrations.
func OpenStore(ctx context.Context, path string) (*Store, error) {
	// WAL lets readers run next to the writer, also across processes (the
	// admin commands open the same file); _txlock=immediate takes the
	// write lock at BEGIN so concurrent writers wait instead of failing.
	dsn := "file:" + path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("server: open database: %w", err)
	}
	db.SetMaxOpenConns(4)
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		db.Close()
		return nil, err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, sub)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("server: migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("server: migrate %s: %w", path, err)
	}
	return &Store{db: db, now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func ms(t time.Time) int64 { return t.UnixMilli() }

func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }

// User is an account.
type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// NormalizeEmail trims and lower-cases an email address and checks that
// it looks like one.
func NormalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndexByte(e, '@')
	if at < 1 || at == len(e)-1 || strings.ContainsAny(e, " \t\r\n") {
		return "", fmt.Errorf("server: %q is not an email address", email)
	}
	return e, nil
}

// AddUser creates a user. It fails with ErrExists for a known email.
func (s *Store) AddUser(ctx context.Context, email string) (User, error) {
	e, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	u := User{Email: e, CreatedAt: fromMS(ms(s.now()))}
	err = s.db.QueryRowContext(ctx, `INSERT INTO users (email, created_at) VALUES (?, ?)
		ON CONFLICT (email) DO NOTHING RETURNING id`, e, ms(u.CreatedAt)).Scan(&u.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", e, ErrExists)
	}
	return u, err
}

// UserByEmail finds a user.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	e, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	u := User{Email: e}
	var created int64
	err = s.db.QueryRowContext(ctx, `SELECT id, created_at FROM users WHERE email = ?`, e).Scan(&u.ID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", e, ErrNotFound)
	}
	u.CreatedAt = fromMS(created)
	return u, err
}

// Users lists every user by email.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, created_at FROM users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var created int64
		if err := rows.Scan(&u.ID, &u.Email, &created); err != nil {
			return nil, err
		}
		u.CreatedAt = fromMS(created)
		out = append(out, u)
	}
	return out, rows.Err()
}

// Token is a device token's metadata. The secret itself is only known
// when it is created; the database keeps its SHA-256.
type Token struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Email      string     `json:"email"`
	Device     string     `json:"device"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// TokenPrefix starts every device token, so they are easy to spot in
// config files and secret scanners.
const TokenPrefix = "nota_"

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// CreateToken makes a new device token for a user and returns the
// secret, which is not stored and cannot be shown again.
func (s *Store) CreateToken(ctx context.Context, userID int64, device string) (string, Token, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return "", Token{}, errors.New("server: a token needs a device name")
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", Token{}, err
	}
	secret := TokenPrefix + base64.RawURLEncoding.EncodeToString(b[:])
	t := Token{UserID: userID, Device: device, CreatedAt: fromMS(ms(s.now()))}
	err := s.db.QueryRowContext(ctx, `INSERT INTO tokens (user_id, token_hash, device, created_at)
		VALUES (?, ?, ?, ?) RETURNING id`, userID, hashToken(secret), device, ms(t.CreatedAt)).Scan(&t.ID)
	if err != nil {
		return "", Token{}, err
	}
	return secret, t, nil
}

// Tokens lists the tokens of one user, or of everyone when userID is 0.
func (s *Store) Tokens(ctx context.Context, userID int64) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.user_id, u.email, t.device, t.created_at,
		t.last_used_at, t.revoked_at FROM tokens t JOIN users u ON u.id = t.user_id
		WHERE ? = 0 OR t.user_id = ? ORDER BY t.id`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		var created int64
		var used, revoked sql.NullInt64
		if err := rows.Scan(&t.ID, &t.UserID, &t.Email, &t.Device, &created, &used, &revoked); err != nil {
			return nil, err
		}
		t.CreatedAt = fromMS(created)
		if used.Valid {
			x := fromMS(used.Int64)
			t.LastUsedAt = &x
		}
		if revoked.Valid {
			x := fromMS(revoked.Int64)
			t.RevokedAt = &x
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken revokes a token by id. Revoking twice is not an error.
func (s *Store) RevokeToken(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tokens SET revoked_at = coalesce(revoked_at, ?) WHERE id = ?`, ms(s.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("token %d: %w", id, ErrNotFound)
	}
	return nil
}

// Authenticate looks up a device token. Unknown and revoked tokens fail
// with ErrNotFound.
func (s *Store) Authenticate(ctx context.Context, secret string) (Principal, error) {
	var p Principal
	var used sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT t.id, t.user_id, u.email, t.device, t.last_used_at
		FROM tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL`, hashToken(secret)).
		Scan(&p.TokenID, &p.UserID, &p.Email, &p.Device, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrNotFound
	}
	if err != nil {
		return Principal{}, err
	}
	// last_used_at is informational; write it at most once a minute.
	now := ms(s.now())
	if !used.Valid || now-used.Int64 > 60_000 {
		_, _ = s.db.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE id = ?`, now, p.TokenID)
	}
	return p, nil
}

// vault is a vault row.
type vault struct {
	ID        int64
	UserID    int64
	Name      string
	Seq       int64
	CreatedAt time.Time
}

func (v vault) info() protocol.VaultInfo {
	return protocol.VaultInfo{Name: v.Name, Cursor: v.Seq, CreatedAt: v.CreatedAt}
}

// Vaults lists a user's vaults by name.
func (s *Store) Vaults(ctx context.Context, userID int64) ([]protocol.VaultInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, seq, created_at FROM vaults WHERE user_id = ? ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.VaultInfo{}
	for rows.Next() {
		var v protocol.VaultInfo
		var created int64
		if err := rows.Scan(&v.Name, &v.Cursor, &created); err != nil {
			return nil, err
		}
		v.CreatedAt = fromMS(created)
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreateVault creates a vault for a user. It fails with ErrExists when
// the user already has one with that name, or had one: an earlier name
// of a renamed vault still leads to it (see vault_aliases).
func (s *Store) CreateVault(ctx context.Context, userID int64, name string) (protocol.VaultInfo, error) {
	if err := checkVaultName(name); err != nil {
		return protocol.VaultInfo{}, err
	}
	now := fromMS(ms(s.now()))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.VaultInfo{}, err
	}
	defer tx.Rollback()
	if err := nameFree(ctx, tx, userID, name, 0); err != nil {
		return protocol.VaultInfo{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO vaults (user_id, name, created_at) VALUES (?, ?, ?)`,
		userID, name, ms(now)); err != nil {
		return protocol.VaultInfo{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.VaultInfo{}, err
	}
	return protocol.VaultInfo{Name: name, CreatedAt: now}, nil
}

func checkVaultName(name string) error {
	if !protocol.ValidVaultName(name) {
		return &protocol.Error{Code: protocol.CodeBadRequest,
			Message: fmt.Sprintf("bad vault name %q: use 1-64 letters, digits, '.', '_' or '-'", name)}
	}
	return nil
}

// nameFree fails with ErrExists when name is taken among the user's
// vaults: the name of a vault, or an earlier name of a vault other than
// self (0 for none).
func nameFree(ctx context.Context, q querier, userID int64, name string, self int64) error {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM vaults WHERE user_id = ? AND name = ?`, userID, name).Scan(&id)
	if err == nil {
		return fmt.Errorf("vault %s: %w", name, ErrExists)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var current string
	err = q.QueryRowContext(ctx, `SELECT v.id, v.name FROM vault_aliases a JOIN vaults v ON v.id = a.vault_id
		WHERE a.user_id = ? AND a.name = ?`, userID, name).Scan(&id, &current)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return err
	case id == self:
		return nil
	}
	return fmt.Errorf("vault %s: %w: it is an earlier name of %s, and devices may still sync with it", name, ErrExists, current)
}

// RenameVault gives a user's vault a new name. The vault keeps its id,
// history, working copy and devices: the old name stays an alias, so
// sync clients that still use it keep reaching the same vault. Renaming
// back to an earlier name is allowed; another vault's name or earlier
// name is ErrExists.
func (s *Store) RenameVault(ctx context.Context, userID int64, name, to string) (protocol.VaultInfo, error) {
	if err := checkVaultName(to); err != nil {
		return protocol.VaultInfo{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.VaultInfo{}, err
	}
	defer tx.Rollback()
	v, err := vaultOf(ctx, tx, userID, name)
	if err != nil {
		return protocol.VaultInfo{}, err
	}
	if v.Name == to {
		return v.info(), nil
	}
	if err := nameFree(ctx, tx, userID, to, v.ID); err != nil {
		return protocol.VaultInfo{}, err
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM vault_aliases WHERE user_id = ? AND name = ?`, []any{userID, to}},
		{`INSERT INTO vault_aliases (user_id, name, vault_id) VALUES (?, ?, ?)`, []any{userID, v.Name, v.ID}},
		{`UPDATE vaults SET name = ? WHERE id = ?`, []any{to, v.ID}},
	} {
		if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
			return protocol.VaultInfo{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return protocol.VaultInfo{}, err
	}
	v.Name = to
	return v.info(), nil
}

// VaultRemoval is what deleting a vault removed.
type VaultRemoval struct {
	Vault RemovedVault `json:"vault"`
	// Files is the number of live files the vault had.
	Files int `json:"files"`
	// Blobs is the number of the user's blob rows that went with it
	// (content no other vault of the user has).
	Blobs int `json:"blobs"`

	// blobHashes are the hashes that lost their last row: their files
	// go in RemoveVaultData unless something references them again.
	blobHashes []string
}

// DeleteVault deletes a user's vault with its whole history and earlier
// names, in one write transaction, plus the user's blob rows no other
// vault of theirs references. The disk is left alone: the caller removes
// the working copy and the unreferenced blob files with RemoveVaultData,
// so the database stays the truth if that fails.
func (s *Store) DeleteVault(ctx context.Context, userID int64, name string) (VaultRemoval, error) {
	return s.deleteVault(ctx, userID, name, nil)
}

// deleteVault is DeleteVault with a check of the vault (the typed
// confirmation of the web app) inside the transaction.
func (s *Store) deleteVault(ctx context.Context, userID int64, name string, check func(vault) error) (VaultRemoval, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return VaultRemoval{}, err
	}
	defer tx.Rollback()
	v, err := vaultOf(ctx, tx, userID, name)
	if err != nil {
		return VaultRemoval{}, err
	}
	if check != nil {
		if err := check(v); err != nil {
			return VaultRemoval{}, err
		}
	}
	rm := VaultRemoval{Vault: RemovedVault{ID: v.ID, Name: v.Name, Cursor: v.Seq}}
	if rm.Files, err = countRows(ctx, tx, `SELECT count(*) FROM files WHERE vault_id = ? AND deleted = 0`, v.ID); err != nil {
		return VaultRemoval{}, err
	}
	var hashes []string
	rows, err := tx.QueryContext(ctx, `SELECT hash FROM versions WHERE vault_id = ? AND hash <> ''
		UNION SELECT hash FROM files WHERE vault_id = ? AND hash <> ''`, v.ID, v.ID)
	if err != nil {
		return VaultRemoval{}, err
	}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return VaultRemoval{}, err
		}
		hashes = append(hashes, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return VaultRemoval{}, err
	}
	// The vault's files, versions and aliases cascade.
	if _, err := tx.ExecContext(ctx, `DELETE FROM vaults WHERE id = ?`, v.ID); err != nil {
		return VaultRemoval{}, err
	}
	for _, h := range hashes {
		// Blob rows are per user: the row stays while another vault of
		// the user still has the content in a version or a file.
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM versions x JOIN vaults w ON w.id = x.vault_id
			WHERE w.user_id = ? AND x.hash = ?
			UNION ALL SELECT 1 FROM files x JOIN vaults w ON w.id = x.vault_id
			WHERE w.user_id = ? AND x.hash = ? LIMIT 1`, userID, h, userID, h).Scan(&one)
		switch {
		case err == nil:
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return VaultRemoval{}, err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM blobs WHERE user_id = ? AND hash = ?`, userID, h)
		if err != nil {
			return VaultRemoval{}, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			rm.Blobs++
		}
		if !blobReferenced(ctx, tx, h) {
			rm.blobHashes = append(rm.blobHashes, h)
		}
	}
	if err := tx.Commit(); err != nil {
		return VaultRemoval{}, err
	}
	return rm, nil
}

// blobReferenced reports whether any user's row or version still
// references a hash (errors count as referenced: the file stays).
func blobReferenced(ctx context.Context, q querier, h string) bool {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM blobs WHERE hash = ?
		UNION ALL SELECT 1 FROM versions WHERE hash = ? LIMIT 1`, h, h).Scan(&one)
	return !errors.Is(err, sql.ErrNoRows)
}

// VaultID returns the id of a user's vault (its working copy is
// <data>/vaults/<id>).
func (s *Store) VaultID(ctx context.Context, userID int64, name string) (int64, error) {
	v, err := vaultOf(ctx, s.db, userID, name)
	return v.ID, err
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// vaultOf finds a vault by name among the user's vaults: its current
// name or an earlier one (vault_aliases); v.Name is the current name.
// Another user's vault is ErrNotFound, exactly like a missing one.
func vaultOf(ctx context.Context, q querier, userID int64, name string) (vault, error) {
	v := vault{UserID: userID}
	var created int64
	err := q.QueryRowContext(ctx, `SELECT id, name, seq, created_at FROM vaults WHERE user_id = ? AND name = ?
		UNION ALL
		SELECT v.id, v.name, v.seq, v.created_at FROM vault_aliases a JOIN vaults v ON v.id = a.vault_id
		WHERE a.user_id = ? AND a.name = ?
		LIMIT 1`, userID, name, userID, name).Scan(&v.ID, &v.Name, &v.Seq, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return vault{}, fmt.Errorf("vault %s: %w", name, ErrNotFound)
	}
	v.CreatedAt = fromMS(created)
	return v, err
}

const entryCols = `path, version, hash, size, deleted, kind, device, modified_at, moved_from`

func scanEntry(sc interface{ Scan(...any) error }) (protocol.Entry, error) {
	var e protocol.Entry
	var mod int64
	var kind string
	err := sc.Scan(&e.Path, &e.Version, &e.Hash, &e.Size, &e.Deleted, &kind, &e.DeviceID, &mod, &e.MovedFrom)
	e.Kind = protocol.Kind(kind)
	e.ModifiedAt = fromMS(mod)
	return e, err
}

// file returns the current state of a path; ok is false when the path
// never existed.
func file(ctx context.Context, q querier, vaultID int64, p string) (protocol.Entry, bool, error) {
	e, err := scanEntry(q.QueryRowContext(ctx, `SELECT `+entryCols+` FROM files WHERE vault_id = ? AND path = ?`, vaultID, p))
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.Entry{}, false, nil
	}
	return e, err == nil, err
}

// changes lists the current state of paths above since, in version order.
func changes(ctx context.Context, q querier, vaultID, since int64, limit int) ([]protocol.Entry, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+entryCols+` FROM files WHERE vault_id = ? AND version > ?
		ORDER BY version LIMIT ?`, vaultID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// versionOf returns one version of a path from the log.
func versionOf(ctx context.Context, q querier, vaultID int64, p string, version int64) (protocol.Entry, error) {
	e, err := scanEntry(q.QueryRowContext(ctx, `SELECT `+entryCols+` FROM versions
		WHERE vault_id = ? AND path = ? AND version = ?`, vaultID, p, version))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// history lists the versions of a path newest first, following
// moved_from back through renames (each hop only up to the version the
// move happened at).
func history(ctx context.Context, q querier, vaultID int64, p string) ([]protocol.Entry, error) {
	out := []protocol.Entry{}
	upTo := int64(1) << 62
	seen := map[string]bool{}
	for p != "" && !seen[p] {
		seen[p] = true
		rows, err := q.QueryContext(ctx, `SELECT `+entryCols+` FROM versions
			WHERE vault_id = ? AND path = ? AND version <= ? ORDER BY version DESC`, vaultID, p, upTo)
		if err != nil {
			return nil, err
		}
		next := ""
		for rows.Next() {
			e, err := scanEntry(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, e)
			if e.MovedFrom != "" {
				// Older versions of this path predate the move: the
				// history continues at the source.
				next, upTo = e.MovedFrom, e.Version
				break
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		p = next
	}
	return out, nil
}

// collision returns a live path in the vault that differs from p only in
// case, in the file name or in any folder above it. except is a path
// that does not count (the source of a case-only rename).
func collision(ctx context.Context, q querier, vaultID int64, p, except string) (string, error) {
	var other string
	err := q.QueryRowContext(ctx, `SELECT path FROM files WHERE vault_id = ? AND fold = ? AND deleted = 0
		AND path <> ? AND path <> ? LIMIT 1`, vaultID, protocol.FoldKey(p), p, except).Scan(&other)
	if err == nil {
		return other, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	const top = "\U0010FFFF" // sorts after every UTF-8 continuation
	segs := strings.Split(p, "/")
	for i := 1; i < len(segs); i++ {
		dir := strings.Join(segs[:i], "/") + "/"
		fold := protocol.FoldKey(dir)
		err := q.QueryRowContext(ctx, `SELECT path FROM files WHERE vault_id = ? AND deleted = 0
			AND fold >= ? AND fold < ? AND NOT (path >= ? AND path < ?) AND path <> ? LIMIT 1`,
			vaultID, fold, fold+top, dir, dir+top, except).Scan(&other)
		if err == nil {
			return other, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	return "", nil
}

// write is one new version to commit.
type write struct {
	Path       string
	Hash       string
	Size       int64
	Deleted    bool
	Device     string
	ModifiedAt time.Time
	MovedFrom  string
}

// commit appends w to the vault's log with the next version and makes it
// the path's current state. It runs inside tx.
func (s *Store) commit(ctx context.Context, tx *sql.Tx, v vault, w write) (protocol.Entry, error) {
	var version int64
	if err := tx.QueryRowContext(ctx, `UPDATE vaults SET seq = seq + 1 WHERE id = ? RETURNING seq`, v.ID).Scan(&version); err != nil {
		return protocol.Entry{}, err
	}
	if w.ModifiedAt.IsZero() {
		w.ModifiedAt = s.now()
	}
	e := protocol.Entry{Path: w.Path, Version: version, Hash: w.Hash, Size: w.Size, Deleted: w.Deleted,
		Kind: protocol.KindOf(w.Path), DeviceID: w.Device, ModifiedAt: fromMS(ms(w.ModifiedAt)), MovedFrom: w.MovedFrom}
	args := []any{v.ID, e.Path, e.Version, e.Hash, e.Size, e.Deleted, string(e.Kind), e.DeviceID, ms(e.ModifiedAt), e.MovedFrom}
	if _, err := tx.ExecContext(ctx, `INSERT INTO versions (vault_id, path, version, hash, size, deleted, kind,
		device, modified_at, moved_from, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		append(args, ms(s.now()))...); err != nil {
		return protocol.Entry{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO files (vault_id, path, version, hash, size, deleted, kind,
		device, modified_at, moved_from, fold) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (vault_id, path) DO UPDATE SET version = excluded.version, hash = excluded.hash,
		size = excluded.size, deleted = excluded.deleted, kind = excluded.kind, device = excluded.device,
		modified_at = excluded.modified_at, moved_from = excluded.moved_from`,
		append(args, protocol.FoldKey(e.Path))...); err != nil {
		return protocol.Entry{}, err
	}
	if w.Deleted {
		// A deleted note is no longer shared: a note made later at its
		// path must not show up behind the old link. Move keeps the link.
		if _, err := tx.ExecContext(ctx, `DELETE FROM shares WHERE vault_id = ? AND path = ?`, v.ID, w.Path); err != nil {
			return protocol.Entry{}, err
		}
	}
	if !w.Deleted {
		if _, err := tx.ExecContext(ctx, `INSERT INTO blobs (user_id, hash, size, created_at) VALUES (?, ?, ?, ?)
			ON CONFLICT DO NOTHING`, v.UserID, e.Hash, e.Size, ms(s.now())); err != nil {
			return protocol.Entry{}, err
		}
	}
	return e, nil
}

// inTx runs fn in a write transaction on the user's vault.
func (s *Store) inTx(ctx context.Context, userID int64, name string, fn func(*sql.Tx, vault) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := vaultOf(ctx, tx, userID, name)
	if err != nil {
		return err
	}
	if err := fn(tx, v); err != nil {
		return err
	}
	return tx.Commit()
}

// apiErr builds a protocol error with an HTTP status.
type apiErr struct {
	status int
	body   protocol.Error
}

func (e *apiErr) Error() string { return e.body.Error() }

func mismatch(cur *protocol.Entry, base int64) error {
	msg := fmt.Sprintf("base version %d is not the current version", base)
	if cur != nil {
		msg = fmt.Sprintf("base version %d is not the current version %d", base, cur.Version)
	}
	return &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeVersionMismatch, Message: msg, Current: cur}}
}

func caseCollision(p, existing string) error {
	return &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeCaseCollision, Existing: existing,
		Message: fmt.Sprintf("%s differs only in case from %s", p, existing)}}
}

// Put commits new content for a path (see the PUT endpoint). The blob
// must already be in the blob store.
func (s *Store) Put(ctx context.Context, userID int64, vaultName string, base int64, w write) (protocol.Entry, error) {
	var out protocol.Entry
	err := s.inTx(ctx, userID, vaultName, func(tx *sql.Tx, v vault) error {
		cur, ok, err := file(ctx, tx, v.ID, w.Path)
		if err != nil {
			return err
		}
		switch {
		case ok && !cur.Deleted:
			if base != cur.Version {
				return mismatch(&cur, base)
			}
			if cur.Hash == w.Hash {
				out = cur // nothing changed: an idempotent retry
				return nil
			}
		case ok && cur.Deleted:
			if base != 0 && base != cur.Version {
				return mismatch(&cur, base)
			}
		case base != 0:
			return mismatch(nil, base)
		}
		if !ok || cur.Deleted {
			other, err := collision(ctx, tx, v.ID, w.Path, "")
			if err != nil {
				return err
			}
			if other != "" {
				return caseCollision(w.Path, other)
			}
		}
		out, err = s.commit(ctx, tx, v, w)
		return err
	})
	return out, err
}

// Delete writes a tombstone for a path based on version base.
func (s *Store) Delete(ctx context.Context, userID int64, vaultName, p string, base int64, device string) (protocol.Entry, error) {
	var out protocol.Entry
	err := s.inTx(ctx, userID, vaultName, func(tx *sql.Tx, v vault) error {
		cur, ok, err := file(ctx, tx, v.ID, p)
		switch {
		case err != nil:
			return err
		case !ok:
			return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: p + " does not exist"}}
		case cur.Deleted:
			return &apiErr{http.StatusGone, protocol.Error{Code: protocol.CodeDeleted, Message: p + " is already deleted", Current: &cur}}
		case base != cur.Version:
			return mismatch(&cur, base)
		}
		out, err = s.commit(ctx, tx, v, write{Path: p, Hash: cur.Hash, Size: cur.Size, Deleted: true, Device: device})
		return err
	})
	return out, err
}

// Move renames a file atomically: a tombstone for from and a create of to
// that continues its history.
func (s *Store) Move(ctx context.Context, userID int64, vaultName string, m protocol.Move, device string) (protocol.MoveResult, error) {
	var out protocol.MoveResult
	err := s.inTx(ctx, userID, vaultName, func(tx *sql.Tx, v vault) error {
		cur, ok, err := file(ctx, tx, v.ID, m.From)
		switch {
		case err != nil:
			return err
		case !ok || cur.Deleted:
			return &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound, Message: m.From + " does not exist"}}
		case m.BaseVersion != cur.Version:
			return mismatch(&cur, m.BaseVersion)
		}
		dst, ok, err := file(ctx, tx, v.ID, m.To)
		if err != nil {
			return err
		}
		if ok && !dst.Deleted {
			return &apiErr{http.StatusConflict, protocol.Error{Code: protocol.CodeExists, Message: m.To + " already exists", Current: &dst}}
		}
		other, err := collision(ctx, tx, v.ID, m.To, m.From)
		if err != nil {
			return err
		}
		if other != "" {
			return caseCollision(m.To, other)
		}
		// A shared note keeps its link when it moves.
		if _, err := tx.ExecContext(ctx, `UPDATE shares SET path = ? WHERE vault_id = ? AND path = ?`, m.To, v.ID, m.From); err != nil {
			return err
		}
		if out.From, err = s.commit(ctx, tx, v, write{Path: m.From, Hash: cur.Hash, Size: cur.Size, Deleted: true, Device: device}); err != nil {
			return err
		}
		out.To, err = s.commit(ctx, tx, v, write{Path: m.To, Hash: cur.Hash, Size: cur.Size, Device: device,
			ModifiedAt: cur.ModifiedAt, MovedFrom: m.From})
		return err
	})
	return out, err
}

// read runs fn on the user's vault outside a transaction. Writers commit
// in version order (one write transaction at a time), so a reader never
// sees version N+1 without N; that is all the change feed needs.
func (s *Store) read(ctx context.Context, userID int64, name string, fn func(querier, vault) error) error {
	v, err := vaultOf(ctx, s.db, userID, name)
	if err != nil {
		return err
	}
	return fn(s.db, v)
}
