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
| `NOTA_SERVER_PUBLIC_URL` | – | the API's public URL, e.g. `https://nota.app.banjoanton.com` |
| `NOTA_SERVER_WEB_ORIGIN` | – | the web app's origins, comma-separated, e.g. `https://nota.banjoanton.com` (add `http://localhost:5173` for a dev web app). CORS with credentials is allowed for them, passkeys are accepted from them, and the first one is where sign-in links point (`/app/connect`, `/app/signup`). |
| `NOTA_SERVER_RP_ID` | – | the WebAuthn relying party id, the web app's domain: `nota.banjoanton.com`. Passkeys are off without it. Changing it later invalidates every passkey. |
| `NOTA_SERVER_COOKIE_DOMAIN` | host only | session cookie domain; `banjoanton.com` lets the cookie set by the API (`nota.app.banjoanton.com`) be sent from the web app's site (`nota.banjoanton.com`) |
| `NOTA_SERVER_COOKIE_INSECURE` | off | `1` drops the cookie's `Secure` flag, for local http only |
| `NOTA_SERVER_SIGNUP` | `invite` | `invite`, `open` or `closed` |
| `NOTA_SERVER_TRUSTED_PROXIES` | none | reverse proxies whose `X-Forwarded-For` is believed for the client IP (rate limits): IPs, CIDRs or `private` (loopback + RFC 1918 + fc00::/7). Behind Coolify's Traefik use `private`. Without it the client IP is the TCP peer. |
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

## Coolify

The production server runs on Coolify as the application `nota` (project
`nota`), built from this repository's `Dockerfile` on the build server
and deployed on every push to the deployed branch:

- domain `https://nota.app.banjoanton.com`, port `8080`
- persistent volume mounted at `/data`
- health check path `/healthz`
- environment: `NOTA_SERVER_PUBLIC_URL=https://nota.app.banjoanton.com`,
  `NOTA_SERVER_WEB_ORIGIN=https://nota.banjoanton.com`,
  `NOTA_SERVER_RP_ID=nota.banjoanton.com`,
  `NOTA_SERVER_COOKIE_DOMAIN=banjoanton.com`, `NOTA_SERVER_SIGNUP=invite`,
  `NOTA_SERVER_HOOKS=off`, `NOTA_SERVER_TRUSTED_PROXIES=private`

Admin commands run inside the container (`docker exec <container> nota
serve …` on the server, or the terminal in the Coolify UI).

Backups: copy `/data` (with the server stopped, or `sqlite3 nota.db
".backup"` for the database plus `blobs/`). `vaults/` can be left out; it
is rebuilt.
