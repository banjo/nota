# Nota sync protocol, version 1

This is the wire protocol between `nota serve` and the sync clients (the
CLI, the desktop app, later the phones). The Go types live in
`internal/protocol`; this document is the prose version.

The model is file-level sync with optimistic concurrency and a 3-way
merge on the client. The server never merges. It stores versions and
refuses writes that are based on a stale version.

## Basics

- **Base URL.** Everything lives under `/v1/vaults/…`. One server holds
  many users, and every user can have several vaults.
- **Protocol version.** Every request sends `Nota-Protocol: 1` (a
  `1.x` value counts as major 1). Clients that can't set headers, such as
  a browser `EventSource`, may send `?protocol=1` instead. A request
  without a version, or with a major the server doesn't speak, gets
  `426 Upgrade Required` with code `unsupported_protocol`, a message
  telling the user to update the app, and `supported: [1]`. Every
  response carries `Nota-Protocol: 1`.
- **Auth.** `Authorization: Bearer <device token>`. Tokens belong to one
  user and one device. A missing, unknown or revoked token gets `401`
  with code `unauthorized`. A token only ever sees its own user's vaults:
  another user's vault answers `404`, exactly like a vault that doesn't
  exist.
- **Paths.** Vault-relative, slash-separated, always Unicode **NFC**.
  No leading or trailing slash, no empty, `.` or `..` segments, no
  backslashes or control characters, at most 1024 bytes. The server
  normalises to NFC and refuses anything else with `400 invalid_path`.
  In URLs each segment is percent-encoded (`Möten/Plan #1.md` →
  `M%C3%B6ten/Plan%20%231.md`).
- **Case.** Two paths that differ only in letter case can't both exist
  (APFS and NTFS are case-insensitive). Creating one gets `409` with
  code `case_collision` and `existing` set to the path that is already
  there. Folder names count too: `projekt/b.md` collides with an existing
  `Projekt/a.md`.
- **Hashes.** Lower-case hex SHA-256 of the file bytes.
- **Kinds.** `md` for `*.md` (merged line by line by the client),
  `blob` for everything else (attachments: never merged, a conflict copy
  when both sides change).
- **Versions.** Each vault has one sequence. Every change to any path
  (create, edit, delete, each half of a move) takes the next number, so
  a file's version is unique within its vault and only goes up. The
  latest version is the vault's **cursor**. Ordering comes only from
  versions, never from device clocks; `modified_at` is for display.
- **Size limit.** Bodies above the server's limit (default 100 MB,
  `--max-file-size`) get `413 too_large`.
- **Errors.** Every error is JSON:
  `{"code": "...", "message": "...", "current": {...}?, "existing": "..."?}`.

### Entry

```json
{
  "path": "Projekt/Plan.md",
  "version": 42,
  "hash": "9f86d0…",
  "size": 1234,
  "deleted": false,
  "kind": "md",
  "device_id": "laptop",
  "modified_at": "2026-11-22T10:42:00Z",
  "moved_from": "Inbox/Plan.md"
}
```

A tombstone (`deleted: true`) keeps the hash and size of what it
deleted. `moved_from` is only set on the version a move created.

## Endpoints

### `GET /v1/vaults`

The user's vaults: `{"vaults": [{"name", "cursor", "created_at"}]}`.

### `POST /v1/vaults`

`{"name": "main"}` creates a vault. Names are 1–64 ASCII letters,
digits, `.`, `_` or `-`. `201` with the vault, `409 exists` if the user
already has one with that name.

### `GET /v1/vaults/{vault}/changes?since={cursor}&limit={n}`

The current state of every path whose version is above `since`, in
version order: `{"entries": [...], "cursor": N, "more": bool}`. `limit`
defaults to 500, at most 5000. When `more` is true the page was full;
ask again with `since=cursor`. Otherwise `cursor` is the vault's latest
version. Since only the current state is listed, a path edited ten
times since the cursor shows up once, and tombstones show up as entries
with `deleted: true`. Start with `since=0` to get everything.

### `GET /v1/vaults/{vault}/files/{path}[?version=N]`

The file's bytes, with headers `Nota-Version`, `Nota-Hash`, `Nota-Kind`,
`Nota-Device`, `Nota-Modified-At`, `Nota-Moved-From` (if any) and
`ETag: "<version>"`. With `?version=N` it returns that old version from
the history (for restore). A deleted file answers `410 deleted` with the
tombstone in `current`; a path that never existed `404 not_found`.

### `PUT /v1/vaults/{vault}/files/{path}`

Writes the body as the new content. Headers:

- `Base-Version: N` (required): the version the client's edit is based
  on. `0` creates a path that doesn't exist or is deleted (the
  tombstone's version works too).
- `Nota-Hash` (optional): the server refuses a body with another hash
  (`400 hash_mismatch`).
- `Nota-Modified-At` (optional): the client's clock, RFC 3339.

Answers:

- `200` with the new Entry. Writing the content the path already has,
  based on its current version, changes nothing and returns the current
  entry (idempotent retries).
- `409 version_mismatch` with `current` when `Base-Version` isn't the
  current version. The client fetches the current content, merges and
  tries again with `Base-Version = current.version`.
- `409 case_collision` (see Case).
- `413 too_large`.

### `DELETE /v1/vaults/{vault}/files/{path}`

`Base-Version: N` required. Writes a tombstone and answers `200` with
it. `409 version_mismatch` when the file changed since (the client then
restores the remote edit: an edit wins over a delete). Deleting a path
that is already deleted answers `410 deleted`. Content stays in the
history.

### `POST /v1/vaults/{vault}/moves`

`{"from": "a.md", "to": "b/a.md", "base_version": N}` renames a file
atomically: a tombstone for `from` and a create of `to` with the same
content and `moved_from` set, so `to`'s history continues `from`'s.
`200 {"from": Entry, "to": Entry}`. `409 version_mismatch` when `from`
changed, `409 exists` when `to` exists, `409 case_collision`, `404` when
`from` doesn't exist. Moves are optional: a client may always send a
delete and a create instead, and loses only the history link.

### `GET /v1/vaults/{vault}/history/{path}`

`{"path": "...", "versions": [Entry, ...]}`, newest first, following
`moved_from` back through renames. Fetch old content with
`GET files/{path}?version=N` using the path of that entry.

### `GET /v1/vaults/{vault}/events`

Server-sent events. On connect and after every change in the vault the
server sends

```
event: change
data: {"cursor": 42}
```

plus a `: keepalive` comment every 25 seconds. Events are coalesced
and may be dropped: they are only a nudge to pull `changes`.
Correctness never depends on them.

## Client algorithm (informative)

The client (`internal/sync`) keeps per path the last synced content (the
merge base), its hash and its server version, plus the vault cursor.

1. **Scan** the vault and hash every file that isn't ignored. A file
   whose hash differs from its base is dirty.
2. **Push** dirty files with `Base-Version`. A local delete is a
   `DELETE`; a delete plus a new file with the same hash is a move.
3. On **409** fetch the current version, 3-way merge base/local/remote
   (line based; changes that don't overlap merge), write the result and
   push it. Overlapping changes, and any change to a blob on both sides,
   produce a conflict copy `Name (conflict 2026-11-22 1042 laptop).md`
   with the local content, while the original takes the remote content.
   A delete against an edit keeps the edit.
4. **Pull** `changes?since=cursor` and apply entries to files that are
   not dirty. Remote deletes move the local file to `.trash/`. The cursor
   advances only past entries that were applied.
5. Listen to **events** and run again on a nudge, on local file changes
   and on a timer.
