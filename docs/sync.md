# Sync

Sync is opt-in: a vault works offline forever without it. `nota serve` is the sync server (see
[deploy.md](deploy.md)); every machine runs `nota sync` against it. The files stay the truth:

- Edits to different lines of a note on two machines merge.
- Edits to the same lines keep both: yours in `Note (conflict 2026-11-22 1042 laptop).md`.
- Deletes go to `.trash/`, and an edit always beats a delete.

The wire protocol is in [sync-protocol.md](sync-protocol.md).

## Connect a vault

With an account on the server (e-mail and passkey, created in the web app with an invite):

```sh
nota login https://nota.example.com   # Shows a code to approve in the web app.
nota remote add https://nota.example.com --remote-vault main
nota sync                             # One pass; --json for scripts.
nota sync status                      # Unsynced changes, server cursor, daemon.
nota sync --watch                     # Keep syncing: file changes, server nudges, every minute.
```

`nota devices` lists the signed-in devices and `nota logout` revokes this one. Without the web
app, an admin can create a device token instead (`nota serve token create`, see
[deploy.md](deploy.md)) and you pass it with `nota remote add <url> --token <token>`.

`nota remote add` creates the server vault if it is new and keeps the token in the OS keyring.
Where there is none (a headless Linux box) it says so and uses `~/.config/nota/credentials`
(mode 0600) instead; `NOTA_KEYRING=file` always uses the file and `NOTA_TOKEN` passes a token
without storing it.

Sync state lives in `.nota/` (`sync.toml`, `state.db`, merge bases); only one sync runs per
vault at a time. The desktop app signs in through the browser (`/app/connect`, handing a device
token back over the `nota://` scheme) and syncs in the background when the vault has a remote.

## Empty folders

Sync works on files, so a folder you made with `nota folder add` (or ⌘K → "Add folder…") stays
on that machine until it holds a note; then the note carries the folder to every device. A folder
that empties out on another machine is not removed here. Empty folders are not part of the sync
protocol (`Nota-Protocol: 1`) and nothing about them is sent to the server, which lists its own
only when made through the web API.

## History

The server keeps every version: `nota history <note>` lists them (through renames,
`--version N` prints one) and `nota restore <note> <version>` makes an old one the newest
version and syncs it here.

In the app, **History** in a note's "…" menu (or ⌘K "Show history", ⌘⇧H) lists the same
versions, newest first, with the device and the size change of each. Pick one to read it, or to
see what changed from it to the note now; **Restore this version** makes it the newest version
(the one it replaces stays in the history), and **Copy text** copies it without restoring. On the
desktop the restored text arrives with the next sync pass, a moment later. A vault without sync
has no history: the panel says so and links to Settings.
