-- +goose Up
-- Accounts: email + passkeys (WebAuthn) + recovery codes, browser
-- sessions, invites and the auth requests of the desktop (PKCE) and CLI
-- (device code) sign-in flows. Every secret (session ids, recovery
-- codes, invite codes, one-time codes, device codes) is stored as its
-- SHA-256 only.

ALTER TABLE users ADD COLUMN email_verified INTEGER NOT NULL DEFAULT 0; -- placeholder: no email is sent yet
ALTER TABLE users ADD COLUMN webauthn_id BLOB; -- random user handle, set on first use
CREATE UNIQUE INDEX users_webauthn_id ON users(webauthn_id) WHERE webauthn_id IS NOT NULL;

CREATE TABLE credentials (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BLOB NOT NULL UNIQUE,
    data          TEXT NOT NULL, -- webauthn.Credential as JSON (public key, sign count, flags)
    name          TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER
);
CREATE INDEX credentials_user ON credentials(user_id);

CREATE TABLE sessions (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_hash  TEXT NOT NULL UNIQUE,
    needs_passkey INTEGER NOT NULL DEFAULT 0, -- signed in with a recovery code: must add a passkey first
    user_agent    TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    last_seen_at  INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE recovery_codes (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    used_at    INTEGER
);
CREATE INDEX recovery_codes_user ON recovery_codes(user_id, code_hash);

CREATE TABLE invites (
    id         INTEGER PRIMARY KEY,
    code_hash  TEXT NOT NULL UNIQUE,
    email      TEXT NOT NULL DEFAULT '', -- when set, only this email can use it
    note       TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at    INTEGER,
    used_by    INTEGER REFERENCES users(id) ON DELETE SET NULL
);

-- Data a WebAuthn ceremony carries from begin to finish.
CREATE TABLE ceremonies (
    id_hash    TEXT PRIMARY KEY,
    kind       TEXT NOT NULL, -- register | login | passkey
    user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
    data       TEXT NOT NULL,
    expires_at INTEGER NOT NULL
) WITHOUT ROWID;

-- Sign-in requests of devices: kind 'pkce' (desktop, one-time code bound
-- to a code challenge) or 'device' (CLI, device code + user code).
CREATE TABLE auth_requests (
    id             INTEGER PRIMARY KEY,
    kind           TEXT NOT NULL,
    code_hash      TEXT NOT NULL UNIQUE, -- one-time code (pkce) or device code (device)
    user_code_hash TEXT UNIQUE,          -- device only
    challenge      TEXT NOT NULL DEFAULT '',
    device         TEXT NOT NULL,
    user_id        INTEGER REFERENCES users(id) ON DELETE CASCADE,
    status         TEXT NOT NULL, -- pending | approved | denied | used
    interval_s     INTEGER NOT NULL DEFAULT 5,
    last_poll_at   INTEGER,
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL
);

-- +goose Down
DROP TABLE auth_requests;
DROP TABLE ceremonies;
DROP TABLE invites;
DROP TABLE recovery_codes;
DROP TABLE sessions;
DROP TABLE credentials;
DROP INDEX users_webauthn_id;
ALTER TABLE users DROP COLUMN webauthn_id;
ALTER TABLE users DROP COLUMN email_verified;
