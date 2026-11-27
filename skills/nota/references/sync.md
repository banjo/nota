# Sync

Sync is opt-in; a vault works offline without it. Each machine runs `nota sync` against a Nota
server (`nota serve`, self-hosted, see [server.md](server.md)). Without a URL, `nota login` uses
the vault's remote, else `$NOTA_SERVER_URL`, else `https://nota.app.banjoanton.com`.

## Connect a vault

Accounts are an e-mail address and a passkey, created in the server's web app with an invite.

```sh
nota login https://nota.example.com --device laptop   # Prints a code and a link to approve.
nota remote add https://nota.example.com --remote-vault main
nota sync --json                                      # One pass.
nota sync status --json                               # Remote, cursor, unsynced changes.
nota sync --watch                                     # Keeps syncing; long-running.
```

`nota login` waits for the user to approve the sign-in in the browser with their passkey: hand
the link to the user and wait. Add `--no-browser` to only print it.

Without the web app, an admin creates a device token on the server (`nota serve token create`)
and the device uses `nota remote add <url> --token <token>`.

- The token is kept in the OS keyring, or `~/.config/nota/credentials` (mode 0600) where there
  is none. `NOTA_KEYRING=file` forces the file; `NOTA_TOKEN` passes a token without storing it.
- The remote lives in `.nota/sync.toml`; only one sync runs per vault at a time.
- `nota remote show`, `nota remote rm` (files stay), `nota devices`, `nota devices revoke`,
  `nota logout` (revokes this device's token).
- The desktop app signs in through the browser and syncs in the background when the vault has a
  remote.

## How merges work

- Edits to different lines of a note on two machines merge.
- Edits to the same lines keep both: the local version is saved as
  `Note (conflict 2026-11-22 1042 laptop).md` next to the note. To resolve, merge the conflict
  copy into the note with the user's approval, then `nota rm` the copy.
- Deletes move files to `.trash/`, and an edit always beats a delete.

## History and restore

The server keeps every version of every note, through renames.

```sh
nota history Plan --json            # Versions, newest first.
nota history Plan --version 3       # The content of version 3.
nota restore Plan 3                 # Makes version 3 the newest version and syncs it here.
```

A restore adds a new version; nothing in the history is rewritten. The app has the same as
History in a note's "…" menu (⌘⇧H).

## Sharing

A synced note can be shared as a public web page on the server (`/s/…`), optionally behind a
password, always showing the latest version with its images. It is done in the app only: Share…
in the note's "…" menu or ⌘K "Share note…"; Stop sharing turns the link off. Moving or renaming
keeps the link; deleting the note ends it. There is no CLI or MCP for it: point the user there.

## Trash and empty folders

- A delete syncs: every device moves the file to its own `.trash/`. Emptying the trash on one
  device leaves the others, and the server keeps every version. A restored note syncs as new.
- The web app has no `.trash/`: its Trash lists notes whose newest version is a delete.
- Empty folders don't sync; a folder reaches other devices once it holds a note.
