-- +goose Up
-- Every row that holds user data is reachable only through a user_id:
-- vaults belong to a user, files and versions to a vault, and blob
-- references are per user (the bytes on disk are shared by hash).

CREATE TABLE users (
    id         INTEGER PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE, -- lower case
    created_at INTEGER NOT NULL      -- unix milliseconds
);

CREATE TABLE tokens (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE, -- hex SHA-256 of the token
    device       TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER,
    revoked_at   INTEGER
);
CREATE INDEX tokens_user ON tokens(user_id);

CREATE TABLE vaults (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    seq        INTEGER NOT NULL DEFAULT 0, -- latest version = the cursor
    created_at INTEGER NOT NULL,
    UNIQUE (user_id, name)
);

-- The current state of every path that ever existed (tombstones included).
CREATE TABLE files (
    vault_id    INTEGER NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    fold        TEXT NOT NULL, -- lower-case NFC path, for case collisions
    version     INTEGER NOT NULL,
    hash        TEXT NOT NULL,
    size        INTEGER NOT NULL,
    deleted     INTEGER NOT NULL DEFAULT 0,
    kind        TEXT NOT NULL,
    device      TEXT NOT NULL,
    modified_at INTEGER NOT NULL,
    moved_from  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (vault_id, path)
) WITHOUT ROWID;
CREATE UNIQUE INDEX files_version ON files(vault_id, version);
CREATE INDEX files_fold ON files(vault_id, fold);

-- The log: one row per version.
CREATE TABLE versions (
    vault_id    INTEGER NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    path        TEXT NOT NULL,
    hash        TEXT NOT NULL,
    size        INTEGER NOT NULL,
    deleted     INTEGER NOT NULL DEFAULT 0,
    kind        TEXT NOT NULL,
    device      TEXT NOT NULL,
    modified_at INTEGER NOT NULL,
    moved_from  TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (vault_id, version)
) WITHOUT ROWID;
CREATE INDEX versions_path ON versions(vault_id, path, version);

-- Which user references which content. The bytes live in the blob store,
-- deduplicated by hash.
CREATE TABLE blobs (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hash       TEXT NOT NULL,
    size       INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, hash)
) WITHOUT ROWID;

-- +goose Down
DROP TABLE blobs;
DROP TABLE versions;
DROP TABLE files;
DROP TABLE vaults;
DROP TABLE tokens;
DROP TABLE users;
