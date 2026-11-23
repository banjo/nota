# Deploying the Nota server

`nota serve` is one static binary with all its state in one directory:
the SQLite database (`nota.db`), the content-addressed blobs (`blobs/`)
and the per-vault working copies with their indexes (`vaults/`, a cache
rebuilt from the database). The repository's `Dockerfile` packages it:

```sh
docker build -t nota --build-arg VERSION=$(git rev-parse --short HEAD) .
docker run -d --name nota -p 8080:8080 -v nota-data:/data \
  -e NOTA_SERVER_PUBLIC_URL=https://nota.example.com nota
curl http://localhost:8080/healthz   # ok
```

The image is Alpine with the binary, `tini` (PID 1: forwards SIGTERM and
reaps the children of server hooks), `curl` (for health checks) and
`ca-certificates`. It runs as the unprivileged user `nota` (uid 10001)
and keeps everything in the volume `/data`, which must be writable by
that user. On SIGTERM the server stops accepting connections, ends SSE
streams and gives running requests 10 s.

## Configuration

| Variable | Default | |
| --- | --- | --- |
| `NOTA_SERVER_ADDR` | `:8080` | listen address |
| `NOTA_SERVER_DATA` | `/data` | data directory |
| `NOTA_SERVER_LOG_FORMAT` | `json` (image), `text` | one wide event per request on stderr |
| `NOTA_SERVER_MAX_FILE_SIZE` | `100MiB` | largest file accepted |
| `NOTA_SERVER_PUBLIC_URL` | – | the API's public URL, e.g. `https://api.notes.example.com` |
| `NOTA_SERVER_WEB_ORIGIN` | – | the web app's origins, comma-separated, e.g. `https://notes.example.com` (add `http://localhost:5173` for a dev web app). CORS with credentials is allowed for them, passkeys are accepted from them, and the first one is where sign-in links point (`/app/connect`, `/app/signup`). |
| `NOTA_SERVER_RP_ID` | – | the WebAuthn relying party id, the web app's domain, e.g. `notes.example.com`. Passkeys are off without it. Changing it later invalidates every passkey. |
| `NOTA_SERVER_COOKIE_DOMAIN` | host only | session cookie domain; `example.com` lets the cookie set by the API (`api.notes.example.com`) be sent from the web app's site (`notes.example.com`) |
| `NOTA_SERVER_COOKIE_INSECURE` | off | `1` drops the cookie's `Secure` flag, for local http only |
| `NOTA_SERVER_SIGNUP` | `invite` | `invite`, `open` or `closed` |
| `NOTA_SERVER_TRUSTED_PROXIES` | none | reverse proxies whose `X-Forwarded-For` is believed for the client IP (rate limits): IPs, CIDRs or `private` (loopback + RFC 1918 + fc00::/7). Behind a reverse proxy on the same host or network (Traefik, Caddy, nginx) use `private`. Without it the client IP is the TCP peer. |
| `NOTA_SERVER_HOOKS` | `off` | `on` runs each vault's `.nota/hooks.toml` on changes from clients. Hooks are shell commands that come from the synced vault and run inside the container with the server's permissions; only their timeouts limit them. Leave it off unless every account on the server is yours. |
| `NOTA_SERVER_POLL_INTERVAL` | `5s` | minimum interval between `nota login` polls |

Health: `GET /healthz` answers `ok` while the process serves HTTP;
`GET /readyz` also checks the database and that `/data` takes writes
(`503` otherwise).

## Behind a proxy

- Terminate TLS at the proxy and forward to port 8080.
- SSE (`/v1/vaults/{v}/events`, `/v1/api/vaults/{v}/events`) needs
  unbuffered responses. The server sends `X-Accel-Buffering: no` and
  flushes every event; Traefik does not buffer (its compress middleware
  skips `text/event-stream`). Keepalive comments go out every 25 s, so
  idle timeouts above that are fine.
- Set `NOTA_SERVER_TRUSTED_PROXIES` so rate limits see real client IPs.

## First user

Accounts are invite-only by default. Bootstrap the first one inside the
container:

```sh
docker exec nota nota serve user add you@example.com
# prints a setup code (an invite bound to the email, 30 days) to register
# the account's passkey in the web app at <web origin>/app/signup?invite=…
```

Until the web app is deployed, give a device a token directly:

```sh
docker exec nota nota serve token create you@example.com --device laptop   # printed once
nota remote add https://nota.example.com --token <token>                  # on the laptop
```

or approve a `nota login` from the server side:

```sh
nota login https://nota.example.com --device laptop      # on the laptop: waits
docker exec nota nota serve auth approve you@example.com --device laptop
```

More: `nota serve invite create [--email]`, `nota serve token list|revoke`,
`nota serve auth pending`, `nota serve rebuild` (drop the working copies;
stop the server first).

Delete an account with everything the server keeps for it:

```sh
docker exec nota nota serve user rm you@example.com          # prints the plan
docker exec nota nota serve user rm you@example.com --yes
```

It removes the user in one transaction (device tokens, web sessions,
passkeys, recovery codes, invites bound to the address, vaults with their
files and version log) and then the vaults' working copies plus the blob
files only that user referenced. Blob bytes are content addressed and
shared, so a file goes only when its last reference does.

## Blob garbage collection

Deleting a vault or an account, or an upload that failed between writing
its bytes and committing, leaves blob content that nothing references.
`nota serve blobs gc` reclaims it:

```sh
docker exec nota nota serve blobs gc --dry-run   # what would go, in bytes
docker exec nota nota serve blobs gc             # remove it
```

Rows of `blobs` whose hash is in no `versions` and no `files` row go
first, in one write transaction (the same serialised path as every other
mutation, so it is safe next to a running server). A file in `/data/blobs`
is removed only when no version and no row references it — versions keep
their content alive, so `nota history` and `nota restore` still work —
and unreferenced files younger than `--min-age` (1h by default) are left
alone so an upload in flight is never swept. The command prints the bytes
reclaimed.

It is deliberately not run at startup: walking the whole blob store on
every boot costs time proportional to its size, to reclaim nothing most
of the time. Put it on a timer (a host cron calling `docker exec`, or your
platform's scheduled tasks) or run it by hand after deleting accounts.

## On a platform (Coolify, Dokku, Fly, …)

Any platform that builds a `Dockerfile` works. What the server needs:

- the port `8080`, behind HTTPS
- a persistent volume mounted at `/data`
- the health check path `/healthz`
- the environment from the table above; for an API at `https://api.notes.example.com` and the
  web app at `https://notes.example.com`:

```sh
NOTA_SERVER_PUBLIC_URL=https://api.notes.example.com
NOTA_SERVER_WEB_ORIGIN=https://notes.example.com
NOTA_SERVER_RP_ID=notes.example.com
NOTA_SERVER_COOKIE_DOMAIN=example.com
NOTA_SERVER_SIGNUP=invite
NOTA_SERVER_TRUSTED_PROXIES=private
```

Admin commands run inside the container (`docker exec <container> nota serve …`, or the
platform's terminal).

Backups: copy `/data` (with the server stopped, or `sqlite3 nota.db ".backup"` for the database
plus `blobs/`). `vaults/` can be left out; it is rebuilt.

# Deploying the web app

The web app (`apps/web`) is a static build served under `/app/` (`base: "/app/"`). It talks to
the API at the URL given by `VITE_NOTA_API` at build time:

```sh
bun install
VITE_NOTA_API=https://api.notes.example.com bunx vp run @nota/web#build   # dist/app/
```

Serve `dist/` from the web app's origin (`NOTA_SERVER_WEB_ORIGIN`) with one rule: every
extensionless path under `/app` answers with `app/index.html`, so deep links such as
`/app/connect` and `/app/signup` work, while `/app/assets/*` are served as files. Passkeys work
because the API's `NOTA_SERVER_RP_ID` is the web app's domain, not the API's; keep it that way,
or every passkey is invalidated.

`bunx vp run site:build` builds the landing page and the web app together into `dist/` (run it
without extra flags, after `rm -rf dist`). On Cloudflare Pages, `apps/landing/public/_worker.js`
(copied to `dist/_worker.js`) does the fallback: a `_redirects` rule for `/app/*` would also
swallow `/app/assets/*` and serve HTML as JavaScript. Check `test -f dist/app/index.html` before
you deploy.

The landing page (`apps/landing`) is Astro, prerendered to `dist/index.html` and
`dist/llms.txt`. Its build reads the latest GitHub release of `banjo/nota`
(`apps/landing/src/data/release.ts`) to choose between download buttons and the `go install`
line; `NOTA_LANDING_RELEASE=none|fixture` forces either state and `PUBLIC_CF_BEACON_TOKEN` adds
Cloudflare Web Analytics.
