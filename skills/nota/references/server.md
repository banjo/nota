# Running the Nota server

`nota serve` is the sync server, the web API, the web app's backend and HTTP MCP, in the same
binary. All state is in one data directory (`--data`, `$NOTA_SERVER_DATA`; `/data` in the
image): `nota.db` (SQLite), `blobs/` (content-addressed files) and `vaults/` (working copies,
rebuilt on demand).

## Docker

```sh
docker build -t nota --build-arg VERSION=$(git rev-parse --short HEAD) .   # In the nota repo.
docker run -d --name nota -p 8080:8080 -v nota-data:/data \
  -e NOTA_SERVER_PUBLIC_URL=https://nota.example.com nota
curl http://localhost:8080/healthz   # ok
```

The image runs as uid 10001; `/data` must be writable by it. Health: `/healthz` (process up),
`/readyz` (database and `/data` writable). Any platform that builds a Dockerfile works (Coolify,
Dokku, Fly): port 8080 behind HTTPS, a persistent volume at `/data`, health check `/healthz`.

## Configuration

| Variable                       | Default       | Meaning                                                                                                                                                  |
| ------------------------------ | ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `NOTA_SERVER_ADDR`             | `:8080`       | Listen address.                                                                                                                                          |
| `NOTA_SERVER_DATA`             | `/data`       | Data directory.                                                                                                                                          |
| `NOTA_SERVER_PUBLIC_URL`       |               | The API's public URL.                                                                                                                                    |
| `NOTA_SERVER_WEB_ORIGIN`       |               | Web app origins, comma-separated; CORS, passkeys and sign-in links use them.                                                                             |
| `NOTA_SERVER_RP_ID`            |               | Passkey domain: the web app's domain. Passkeys are off without it; changing it invalidates every passkey.                                                |
| `NOTA_SERVER_COOKIE_DOMAIN`    | host          | E.g. `example.com` when API and web app are on sibling subdomains.                                                                                       |
| `NOTA_SERVER_COOKIE_INSECURE`  | off           | `1` for local http only.                                                                                                                                 |
| `NOTA_SERVER_SIGNUP`           | `invite`      | `invite`, `open` or `closed`.                                                                                                                            |
| `NOTA_SERVER_TRUSTED_PROXIES`  | none          | Proxies whose `X-Forwarded-For` is trusted; `private` behind Traefik, Caddy or nginx.                                                                    |
| `NOTA_SERVER_HOOKS`            | `off`         | `on` runs vaults' hooks on the server as shell commands. Only when every account is the owner's.                                                         |
| `NOTA_SERVER_MAX_FILE_SIZE`    | `100MiB`      | Largest file accepted.                                                                                                                                   |
| `NOTA_SERVER_LOG_FORMAT`       | `text`        | `json` in the image; one wide event per request on stderr.                                                                                               |
| `NOTA_SERVER_POLL_INTERVAL`    | `5s`          | Minimum interval between `nota login` polls.                                                                                                             |
| `NOTA_SERVER_BLOB_GC_INTERVAL` | `off`         | Run blob garbage collection this often (`24h`, `7d`); first pass one interval after start.                                                               |
| `NOTA_SERVER_BLOBS`            | `/data/blobs` | `s3://bucket/prefix` keeps file contents in an S3-compatible bucket (R2, S3, MinIO, B2).                                                                 |
| `NOTA_SERVER_S3_ENDPOINT`      | AWS S3        | The bucket's service, e.g. `https://<account>.r2.cloudflarestorage.com`; plus `NOTA_SERVER_S3_REGION` and `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`. |

Typical split setup, API at `api.notes.example.com` and web app at `notes.example.com`:

```sh
NOTA_SERVER_PUBLIC_URL=https://api.notes.example.com
NOTA_SERVER_WEB_ORIGIN=https://notes.example.com
NOTA_SERVER_RP_ID=notes.example.com
NOTA_SERVER_COOKIE_DOMAIN=example.com
NOTA_SERVER_TRUSTED_PROXIES=private
```

Behind a proxy: terminate TLS there, forward to 8080, and leave server-sent events unbuffered
(`/v1/vaults/{v}/events`, `/v1/api/vaults/{v}/events`); keepalives go out every 25 s. Shared
notes are pages at `/s/…` on the API's domain, so route that path to the server too.

## Accounts and devices

Admin commands run inside the container: `docker exec nota nota serve …`.

```sh
nota serve user add you@example.com                    # Prints a setup code for the first passkey.
nota serve token create you@example.com --device laptop # A device token, printed once.
nota serve auth approve you@example.com --device laptop # Approves a waiting `nota login`.
nota serve user rm you@example.com                      # Prints the plan; add --yes to delete.
```

Also: `nota serve invite create [--email]`, `nota serve token list|revoke`,
`nota serve auth pending`, `nota serve vault list|rm`, `nota serve rebuild` (stop the server
first). `user rm` and `vault rm` delete history for good: list first and let the user choose.
`nota serve <command> --help` has the flags.

## Maintenance

- Backups: copy `/data` with the server stopped, or `sqlite3 nota.db ".backup …"` plus
  `blobs/` (or the bucket). `vaults/` can be left out.
- `nota serve blobs gc --dry-run`, then `nota serve blobs gc`, reclaims blobs nothing
  references (after deleting accounts or vaults). It is safe next to a running server. It runs
  on its own only with `NOTA_SERVER_BLOB_GC_INTERVAL`, never at startup.
- Moving blobs to a bucket: with the bucket's variables set, `nota serve blobs migrate` while
  the server runs, again after stopping it, then start it with the variables and finish with
  `nota serve blobs migrate --delete` to remove the local copies.

## The web app

A static build served under `/app/` on the web origin, pointed at the API at build time:

```sh
bun install
VITE_NOTA_API=https://api.notes.example.com bunx vp run @nota/web#build   # dist/app/
```

Every extensionless path under `/app` must serve `app/index.html` (deep links such as
`/app/connect`), while `/app/assets/*` are served as files.
