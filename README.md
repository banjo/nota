# Nota

Lightweight local-first markdown notes: CLI first, MCP, sync, desktop and phone app.

The plan is in [issue #1](https://github.com/banjo/nota/issues/1).

## Use the CLI

```sh
go install ./cmd/nota
nota add "Köp mjölk" --tag handla        # new note in Inbox/
echo "# Plan" | nota add --folder Projekt -
nota ls --tag projekt --where status=draft --sort "updated desc"
nota search "möte anna"
nota cat Plan                            # by path, title or unique prefix
nota set Plan status=done priority=2     # frontmatter, edited line by line
nota tag Plan +nota -draft
nota mv Plan Arkiv
nota inbox
nota ls --view drafts                    # .nota/views/drafts.yaml
nota index --rebuild
nota completion zsh > "${fpath[1]}/_nota"
```

Every command takes `--json` (errors then go to stderr as JSON too) and
`--verbose`. Exit codes: 0 ok, 1 error, 2 usage, 3 not found, 4 ambiguous
note reference, 5 target exists or differs only in case.

The vault is `--vault`, else `$NOTA_VAULT`, else `vault = "…"` in
`~/.config/nota/config.toml`, else `~/Notes`. The config also takes
`inbox = "Inbox"`. `.notaignore` (gitignore syntax) excludes files;
`.git/ .obsidian/ .nota/ .trash/ .DS_Store` are always ignored. The index
lives in `<vault>/.nota/index.db` and can be deleted at any time.

## Layout

- `cmd/nota`: the CLI entry point (`nota mcp`, `nota serve` later)
- `internal/vault`: files, note model, discovery, ignore rules, NFC paths
- `internal/frontmatter`: byte-stable frontmatter edits
- `internal/markdown`: tags and links from note bodies
- `internal/index`: SQLite index (FTS, tags, properties, links)
- `internal/views`: saved queries in `.nota/views/*.yaml`
- `internal/events`: core event bus
- `internal/cli`: the commands
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
go test ./...        # Go tests, incl. the 10k-note performance budget
go test -short ./... # without the 10k-note fixture
```
