-- +goose Up
-- Shared notes: a link (/s/<id>) that shows one note, live, as a web
-- page. A share follows its note through moves (Store.commit), and may
-- be behind a password the owner picks.
CREATE TABLE shares (
    id            TEXT PRIMARY KEY,   -- random, in the link
    vault_id      INTEGER NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    path          TEXT NOT NULL,
    password_hash TEXT NOT NULL DEFAULT '', -- pbkdf2-sha256$<iterations>$<salt>$<key>, '' for none
    theme         TEXT NOT NULL DEFAULT '', -- ShareTheme as JSON, '' for Nota's own
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    UNIQUE (vault_id, path)
);

-- Keys the server makes once and keeps, e.g. the one that signs the
-- cookie of an unlocked share.
CREATE TABLE server_secrets (
    name  TEXT PRIMARY KEY,
    value BLOB NOT NULL
);

-- +goose Down
DROP TABLE server_secrets;
DROP TABLE shares;
