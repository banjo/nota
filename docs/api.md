# Nota server API: accounts, devices and the web API

This is the HTTP API of `nota serve` next to the sync protocol
(`docs/sync-protocol.md`). The web app (`<web origin>/app`), the
desktop app and the CLI use it. Go types live in `internal/protocol`
(wire types shared with clients) and `internal/server`.

## Conventions

- Every `/v1/` request carries `Nota-Protocol: 1` (or `?protocol=1`),
  as in the sync protocol; anything else is `426` with
  `code: unsupported_protocol`.
- Bodies are JSON. Errors are `{"code": "...", "message": "..."}` plus
  optional fields (`current`, `existing`, `interval`). `code` is stable,
  `message` is for people.
- **Authentication** is either a device token (`Authorization: Bearer
  nota_…`, what `nota sync`, `nota login` and the desktop app use) or the
  browser session cookie `nota_session` (HttpOnly, Secure, SameSite=Lax,
  `Domain` from `NOTA_SERVER_COOKIE_DOMAIN`). Sync routes
  (`/v1/vaults/…`) take tokens only; the account and web API routes take
  either unless noted.
- **CSRF.** A state-changing request (`POST`, `PUT`, `PATCH`, `DELETE`)
  authenticated by the cookie, and every sign-in call, must send
  `Nota-Client: web`, and its `Origin` (when sent) must be one of
  `NOTA_SERVER_WEB_ORIGIN` or the API itself. Otherwise `403 csrf`.
  Token requests are exempt.
- **CORS.** Requests from the web origins get
  `Access-Control-Allow-Origin: <origin>` with credentials; preflights
  allow `Content-Type, Authorization, Nota-Client, Nota-Protocol,
  Base-Version, Nota-Hash, Nota-Modified-At, Last-Event-ID`.
- **Rate limits.** Sign-in routes share a token bucket per client IP
  (30/min, burst 20): `429 rate_limited`. The client IP is the TCP peer,
  or the right-most untrusted `X-Forwarded-For` hop when the peer is in
  `NOTA_SERVER_TRUSTED_PROXIES`.

## Accounts

An account is an email plus one or more passkeys (WebAuthn, relying party
`NOTA_SERVER_RP_ID`). There are no passwords. Sign-up is governed by
`NOTA_SERVER_SIGNUP`: `invite` (default; needs an invite code from `nota
serve invite create`), `open`, or `closed`. A user created by the admin
with `nota serve user add` has no passkey; the setup code that command
prints is an invite bound to the email, and registering with it claims the
account in any mode.

The WebAuthn calls are two steps. `begin` returns `{ceremony, options}`;
pass `options` to `navigator.credentials.create()` / `.get()` (e.g. with
`PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey)`),
then send the result (`credential.toJSON()`) with the `ceremony` id to
`finish`. A ceremony can be finished once, within 10 minutes.

| Route | Auth | Body → response |
| --- | --- | --- |
| `GET /v1/auth/config` | – | → `{signup, rp_id, passkeys}` |
| `POST /v1/auth/register/begin` | – (CSRF) | `{email, invite?}` → `{ceremony, options}`; `403 invite_required`, `invalid_invite`, `signup_closed`; `409 exists` |
| `POST /v1/auth/register/finish` | – (CSRF) | `{ceremony, credential, name?}` → `{user, recovery_codes}` and the session cookie. The 10 recovery codes are shown only here. |
| `POST /v1/auth/login/begin` | – (CSRF) | `{email?}` → `{ceremony, options}`. Without an email (or with an unknown one) the options are usernameless (discoverable passkeys). |
| `POST /v1/auth/login/finish` | – (CSRF) | `{ceremony, credential}` → `{user, recovery_codes_left}` and the cookie; `400 webauthn_failed` (wrong origin, bad signature, unknown passkey), `400 expired` |
| `POST /v1/auth/recovery` | – (CSRF) | `{email, code}` → `{user, needs_passkey: true, recovery_codes_left}`; `401 invalid_credentials`. Each code works once. The session may only add a passkey (and sign out) until it has: other routes answer `403 passkey_required`. |
| `GET /v1/auth/me` | any | → `{user, via, device?, needs_passkey, passkeys, recovery_codes_left}` |
| `POST /v1/auth/logout` | session | → `204`, cookie cleared |
| `GET /v1/auth/passkeys` | any | → `{passkeys: [{id, name, created_at, last_used_at}]}` |
| `POST /v1/auth/passkeys/begin`, `…/finish` | session | add a passkey, as registration; finish → `{passkeys}` |
| `DELETE /v1/auth/passkeys/{id}` | session | → `204`; `409 last_passkey` |
| `POST /v1/auth/recovery-codes` | session | → `{recovery_codes}`, replacing the old ones |

Sessions are server-side (the cookie holds a random id, the database its
SHA-256). They expire after 30 days without use; every use pushes the
expiry out.

## Device sign-in

Devices get their own revocable token.

**CLI (device code).** `nota login [url]`:

1. `POST /v1/auth/device` `{device}` → `{device_code, user_code,
   verification_uri, verification_uri_complete, expires_in: 600,
   interval: 5}`. `verification_uri` is `<web app>/app/connect`; the
   complete form adds `?code=USER-CODE`.
2. The user opens the link, signs in and the page calls
   `POST /v1/auth/device/lookup` `{user_code}` → `{id, device,
   created_at, expires_at}` to show which device asks, then
   `POST /v1/auth/device/approve` `{user_code, approve?: true|false}` →
   `204` (session only).
3. Meanwhile the CLI polls `POST /v1/auth/token` `{grant_type:
   "urn:ietf:params:oauth:grant-type:device_code", device_code}` every
   `interval` seconds. Answers: `400 authorization_pending`, `400
   slow_down` (polled too fast; `interval` in the body is the new
   interval, +5 s each time), `400 access_denied`, `400 expired_token`,
   or `200` with the token (once).

Admins can approve a pending sign-in without the web app:
`nota serve auth pending` and `nota serve auth approve <email> --device
NAME` (or `--id N`).

**Desktop (browser + PKCE).**

1. The app makes a random `code_verifier` (43–128 chars of
   `[A-Za-z0-9-._~]`) and opens
   `<web origin>/app/connect?challenge=<base64url(sha256(verifier))>&device=<name>&redirect=nota://auth&state=<random>`.
2. After passkey sign-in the page calls `POST /v1/auth/connect`
   `{code_challenge, code_challenge_method: "S256", device, redirect_uri,
   state}` (session only) → `{code, redirect, expires_in: 120}` and sends
   the browser to `redirect` (`nota://auth?code=…&state=…`).
   `redirect_uri` must be `nota://auth` or a loopback
   `http://127.0.0.1:<port>/…` URL.
3. The app checks `state` and calls `POST /v1/auth/token` `{grant_type:
   "authorization_code", code, code_verifier}` → `{token, token_type:
   "bearer", token_id, device, email}`. A code works once (a wrong
   verifier burns it) and expires after 2 minutes: `400 invalid_grant`.

## Devices

| Route | Auth | |
| --- | --- | --- |
| `GET /v1/devices` | any | → `{devices: [{id, device, created_at, last_used_at, current}]}` (active tokens; `current` is the calling token) |
| `DELETE /v1/devices/{id}` | any | revoke one of your tokens → `204`; another user's id is `404` |
| `DELETE /v1/devices/current` | token | revoke the calling token (`nota logout`) |

CLI: `nota devices [--server URL]`, `nota devices revoke <id>`,
`nota logout [url]`.

## Settings

The account keeps the user's settings (`docs/settings.md`) so they
follow them to every device. The server stores any key that matches
`^[a-z][a-z0-9_]{0,63}$` with a string value of at most 256 bytes, at
most 64 keys, so an older server keeps what a newer app writes; the
apps check the values.

| Route | Auth | Body → response |
| --- | --- | --- |
| `GET /v1/account/settings` | any | → `{rev, settings: {key: value}}`; a new account is `{rev: 0, settings: {}}` |
| `PATCH /v1/account/settings` | any | `{set?: {key: value}, unset?: [key]}` → `{rev, settings}`; `400 bad_request` for a bad key or value. Keys change one by one and the last write of a key wins, so two devices changing different keys don't collide. A patch that changes nothing keeps `rev`. |
| `GET /v1/account/events` | any | Server-sent events: `event: settings` with `data: {"rev": N}`, once at the start and after every change from any device. Fetch the settings when `rev` is new. Keep-alive comments as on the vault stream. |

## Web API

The web app's `NotesClient` talks to `/v1/api/vaults/{vault}/…` with the
session cookie (a device token works too). It reads the server's working
copy of the vault and its index; every write is a new version in the
version log, exactly like a sync push, so other devices pull it. Writes
made with the cookie are versions of the device `web`; with a token, of
the token's device. Note paths in URLs are vault paths escaped segment by
segment (`protocol.EscapePath`); the `.md` may be left out.

Shapes:

```jsonc
// NoteInfo (lists, tree, search)
{"path": "Projekt/Plan.md", "folder": "Projekt", "title": "Plan",
 "tags": ["work"], "properties": {"status": "draft"},
 "created": "2026-11-22T10:12:00Z", "updated": "2026-11-22T10:42:00Z",
 "modified": "…", "size": 120, "snippet": "…matching text…"}  // snippet: search only
// Note = NoteInfo plus
{"version": 7, "hash": "<sha256 hex of content>", "body": "# Plan\n…",
 "content": "---\n…\n---\n# Plan\n…",
 "backlinks": [{"from": "Möte.md", "to": "Projekt/Plan.md", "target": "Plan"}],
 "links": [{"from": "Projekt/Plan.md", "to": "Möte.md", "target": "Möte"}]}
```

| Route | Body → response |
| --- | --- |
| `GET /v1/api/vaults` | → `{vaults: [{name, cursor, created_at}]}` |
| `POST /v1/api/vaults` | `{name}` → `201` vault; `409 exists` for a name the user's vaults have or had (see below) |
| `PATCH /v1/api/vaults/{vault}` | session only; `{name}` → the vault with its new name. `400 bad_request` for an invalid name, `409 exists` for a name another of the user's vaults has or had, `404 not_found` |
| `DELETE /v1/api/vaults/{vault}` | session only; `{confirm}` (must be the vault's current name, as typed by the user) → `204`. Deletes the files, every version, the earlier names and the server's working copy, plus the blob files nothing else references. `400 bad_request` when `confirm` does not match. Cannot be undone. |
| `GET …/tree` | → `{inbox, folders: [{path, name, notes}], notes: [NoteInfo], cursor}` (every note, by path; every folder, empty ones too, with counts including subfolders) |
| `GET …/notes?folder=&tag=&where=k=v&where=k!=v&view=&sort=&limit=` | → `{notes: [NoteInfo], count}`. `sort` is `path|title|created|updated|modified [asc|desc]`; `view` starts from a saved view and the other filters narrow it. |
| `GET …/search?q=&folder=&tag=&limit=` | → `{notes, count}` best match first, with `snippet`; empty `q` is `400` |
| `GET …/inbox` | → `{notes, count}` the inbox folder, newest first |
| `GET …/tags` | → `{tags: [{tag, count}]}` |
| `GET …/views` | → `{views: [{id, name, icon, where, tag, folder, sort, file, count}]}` from `.nota/views/*.yaml` |
| `GET …/plugins` | → `{plugins: [{id, name, version?, description?, author?, contributes: {themes: [{id, name, light?, dark?}]}, dir, error?}]}` from `.nota/plugins/*/manifest.json`; a broken plugin has only `id`, `dir` and `error` ([themes](themes.md)) |
| `GET …/notes/{path}` | → `Note`; `404 not_found` |
| `POST …/notes` | `{title?, body?, folder?, tags?, properties?}` → `201 Note`. Default folder is the inbox (`"/"` is the root); the file name comes from the title; `created` is stamped. Hooks may move it: the response shows where it ended up. |
| `PUT …/notes/{path}` | header `Base-Version: <version>` (required; `0` creates the note at that path), body `{content}` (whole file) or `{body}` (keeps the frontmatter) → `Note`. A stale base is `409 version_mismatch` with `current`, like sync. `updated` is set when the note has frontmatter. |
| `PATCH …/notes/{path}` | optional `Base-Version`; `{set?: {k: v}, unset?: [k], tags?: {add?, remove?}}` → `Note`. Only the touched frontmatter lines change. |
| `DELETE …/notes/{path}` | optional `Base-Version` → the tombstone entry. Devices move the file to `.trash/`; the server keeps every version. |
| `POST …/move` | `{path, folder}` (keep the name; `"/"` = root) or `{path, to}` (rename), optional `base_version` → `{from, path, version}`; `409 exists` / `case_collision` |
| `POST …/folders/rename` | `{path, to}` (`to` is the new vault path, so it can move the folder too) → `200 {from, path}`. Every file in the folder moves to the new path one by one, as versions of the calling device, so sync sees plain note moves; wikilinks are not rewritten (they name notes). `404 not_found`, `409 exists` / `case_collision` (a case-only rename is refused), `400 invalid_path` (root, hidden names, into itself). |
| `POST …/folders` | `{path}` (nested `a/b` makes the parents) → `201 {path}`; `409 exists` / `case_collision`, `400 invalid_path` (hidden, ignored or reserved names). An empty folder is not a version: it stays on this server, sends no event and does not sync until a note lands in it. |
| `GET …/history/{path}` | → `{path, versions: [Entry]}` newest first, through renames |
| `GET …/history/{path}?version=N` | → `Entry` plus `content` (absent for a delete) |
| `POST …/history/{path}/restore` | `{version}`, optional `Base-Version` → the new `Entry`: the old bytes become the newest version; history is never rewritten |
| `GET …/files/{path}` | the raw file (attachments), with the sync protocol's headers. Images (png, jpg, gif, webp, avif, svg, bmp) have their content type, anything else is `application/octet-stream`; every file has `nosniff` and a sandboxing CSP, so an SVG opened on its own runs nothing. `Cache-Control: private, no-cache`; `If-None-Match` with the `ETag` → `304`. With `?from=<note path>` the path is an embed's target (`![[name]]`), found from that note: a vault path, the note's folder, `Attachments/`, then the shortest path ending in it; `404 not_found` otherwise. `protocol=1` may be in the query, for `<img src>`. |
| `POST …/attachments?name=` | the raw bytes of an image (pasted or dropped into a note) → `201 {path}`: saved to `Attachments/` (or an existing `attachments/`) under its name, or with ` 2`, ` 3` …, as a version of the calling device. `400 invalid_path` for other file types, `413 too_large` above the file size limit. |
| `GET …/changes?since=` | the sync change feed (to see which paths changed) |
| `GET …/events?protocol=1` | SSE: `event: change` / `data: {"cursor": N}` on every new version (from any device, the web or a hook), `: keepalive` comments every 25 s. Use `new EventSource(url, {withCredentials: true})`. Responses send `X-Accel-Buffering: no` so proxies do not buffer. |

**Vault names.** A vault is addressed by its name everywhere: these
routes, the sync protocol, `.nota/sync.toml` (`nota remote`, the desktop
app) and `/mcp?vault=`. Device tokens belong to the account, not to a
vault. Renaming keeps the vault's id, history and working copy, and the
old name stays an alias: a device still configured with it keeps
syncing with the same vault (and gets its change events), and no other
vault of the user can take that name until the vault is deleted. A
vault may be renamed back to one of its own earlier names. Rename and
delete need a browser session, so a device token (a synced laptop, an
agent on `/mcp`) cannot do either; `nota serve vault rm` is the admin
equivalent of the delete.

Errors use the codes of the sync protocol (`not_found`, `version_mismatch`,
`exists`, `case_collision`, `invalid_path`, `bad_request`, `too_large`)
plus `unauthorized`, `csrf`, `passkey_required`.
