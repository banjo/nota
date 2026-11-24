-- +goose Up
-- The account layer of the user's settings (internal/settings): a JSON
-- object of the keys the account sets, and a revision that grows with
-- every change so clients can tell when to fetch again.
CREATE TABLE user_settings (
    user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    data       TEXT NOT NULL,   -- {"theme": "dark", …}
    rev        INTEGER NOT NULL,
    updated_at INTEGER NOT NULL -- unix milliseconds
);

-- +goose Down
DROP TABLE user_settings;
