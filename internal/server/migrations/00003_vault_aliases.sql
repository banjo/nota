-- +goose Up
-- The earlier names of renamed vaults. Sync clients (.nota/sync.toml,
-- `nota remote`, the desktop app) and /mcp address a vault by name, so a
-- name once given to a vault keeps leading to it until the vault is
-- deleted: renaming never breaks a device, and no other vault of the
-- user can take the old name.
CREATE TABLE vault_aliases (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    vault_id INTEGER NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, name)
) WITHOUT ROWID;
CREATE INDEX vault_aliases_vault ON vault_aliases(vault_id);

-- +goose Down
DROP TABLE vault_aliases;
