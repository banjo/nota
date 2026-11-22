# Nota

Lightweight local-first markdown notes: CLI first, MCP, sync, desktop and phone app.

The plan is in [issue #1](https://github.com/banjo/nota/issues/1).

## Layout

- `cmd/nota`: the CLI (`nota mcp`, `nota serve` later)
- `internal/`: the Go core (frontmatter, index, ...)
- `apps/desktop`: the MyGo app (Go + React)
- `packages/editor`: CodeMirror 6 setup
- `prototypes/`: UI prototypes

## Develop

Needs Go 1.27+ and Bun.

```sh
bun install
bun run dev          # desktop app (mygo dev + Vite+)
bunx vp check        # format, lint, types
bunx vp run -r test  # JS tests
go test ./...        # Go tests
```
