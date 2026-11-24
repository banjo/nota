# Contributing

Thanks for helping. Bug reports, small fixes and docs improvements are welcome as issues or pull
requests; for a larger change, open an issue first so we can agree on the approach.

## Setup

You need [Go](https://go.dev/dl/) 1.27+ and [Bun](https://bun.sh) 1.4+. The desktop app on
Linux also needs WebKitGTK (`libwebkit2gtk-4.1`).

```sh
bun install
bun run dev                  # Desktop app (MyGo + Vite+), with hot reload.
bun run dev:web              # Web app; VITE_NOTA_API=http://localhost:8080 for a local server.
go build -o bin/nota ./cmd/nota
scripts/demo-vault.sh /tmp/nota-demo   # A small vault to try things on.
```

## Layout

- `cmd/nota`: the `nota` binary (CLI, `nota mcp`, `nota serve`); CLI scripts in `cmd/nota/testdata/script`.
- `internal/`: the Go core: `vault` (files), `frontmatter`, `markdown`, `index` (SQLite), `views`,
  `events`, `edit`, `hooks`, `mcp`, `protocol`, `server`, `sync`, `cli`.
- `apps/desktop`: the MyGo desktop app (Go services + the shared React UI).
- `apps/web`, `apps/landing`: the web app and the landing page.
- `packages/app` (React UI), `packages/client` (desktop and HTTP clients), `packages/editor`
  (CodeMirror 6), `packages/ui` (Tailwind tokens and components).

The `.md` files are the source of truth; the SQLite index and all sync state can be rebuilt.
The frontend never touches files directly: all vault IO goes through the Go core.

## Before you push

Every commit should pass:

```sh
bunx vp check                # Format, lint and types (bunx vp check --fix formats).
bunx vp run -r test          # TypeScript tests.
gofmt -l .                   # Prints nothing.
go vet ./...
go test ./...                # Add -short to skip the 10k-note fixture.
rm -rf dist && bunx vp run site:build && test -f dist/app/index.html
```

`NOTA_UPDATE_SCRIPTS=1 go test ./cmd/nota` rewrites the expected output in the CLI scripts;
review the diff before you commit it.

## Commits and pull requests

- One logical change per commit, with a message in the imperative ("Add …", "Fix …") and a body
  that says why.
- Add or update tests with the change, and docs when behaviour changes.
- Add a changeset (`bunx changeset`, pick `@nota/desktop`, patch) when the change reaches users;
  its line goes into the release notes. See [RELEASE.md](RELEASE.md).
- `bun.lock` resolves everything from the public npm registry. If your machine points npm at
  another registry, add dependencies with `NPM_CONFIG_REGISTRY=https://registry.npmjs.org/ bun install`.
- Keep pull requests focused; describe how you tested them.

By contributing you agree that your contributions are licensed under the [MIT License](LICENSE).
