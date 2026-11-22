# Nota server API: accounts, devices and the web API

This is the HTTP API of `nota serve` next to the sync protocol
(`docs/sync-protocol.md`). The web app (`nota.banjoanton.com/app`), the
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
   `https://nota.banjoanton.com/app/connect?challenge=<base64url(sha256(verifier))>&device=<name>&redirect=nota://auth&state=<random>`.
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
