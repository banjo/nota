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

## Use it from an agent (MCP)

`nota mcp` is a Model Context Protocol server on stdio. Add it to Claude
Code, Hermes or any MCP client:

```json
{
  "mcpServers": {
    "nota": { "command": "nota", "args": ["mcp"] }
  }
}
```

In Hermes (`~/.hermes/config.yaml`):

```yaml
mcp_servers:
  nota:
    command: nota
    args: [mcp]
```

With Claude Code: `claude mcp add nota -- nota mcp`. Add `"--vault",
"/path/to/vault"` to the args to pick a vault other than the default.

Tools: `search_notes`, `read_note`, `list_notes` (folder, tag, `where`,
view), `create_note` (inbox by default), `append_to_note`, `update_note`
(pass `expected_hash` from `read_note` to avoid overwriting newer edits),
`update_frontmatter`, `move_note`, `list_tags`, `list_views`,
`get_backlinks`. Results are structured JSON; failures come back as tool
errors. Each note is also a resource at `nota://note/<path>` (percent-encode
non-ASCII names, e.g. `nota://note/M%C3%B6ten/Plan.md`).

## Hooks

Scripts in `.nota/hooks.toml` run when notes change, like git hooks. They
get the event as JSON on stdin (`event`, `vault`, `path`, `title`, `tags`,
`frontmatter`, `body`) and `$NOTA_VAULT`, `$NOTA_EVENT`, `$NOTA_NOTE`,
`$NOTA_HOOK`, and run with the vault as working directory (`sh -c`).

```toml
# File #jobb notes from the inbox into Jobb/ and tag them.
[[hook]]
name = "sort"
event = "inbox.added" # note.created note.saved note.moved note.deleted inbox.added sync.completed
mode = "transform" # "after" (default): async, never blocks
run = "sh scripts/sort.sh"
timeout = "2s" # default 2s for transform, 30s for after
match = { folder = "Inbox", tag = "jobb" }

# Log every save.
[[hook]]
event = "note.saved"
run = "cat >> .nota/saved.jsonl; echo >> .nota/saved.jsonl"
```

`scripts/sort.sh` prints a patch; empty output means no change:

```sh
#!/bin/sh
echo '{"folder": "Jobb", "tags": {"add": ["sorterad"]}, "set": {"status": "todo"}}'
```

Transform hooks run only on `note.created` and `inbox.added`. Their patch
(`set`, `unset`, `tags.add`/`tags.remove`, `folder`) goes through the
normal write path, so events fire and `updated` is set, but a hook's own
change never triggers it again. A hook that fails, times out or prints
something that is not a patch is logged and leaves the note unchanged.
`nota hooks list` shows the hooks; `nota hooks test inbox.added Inbox/x`
runs them on a note without applying anything. Hooks run in the CLI,
`nota mcp` and the desktop app.

## Notes and timestamps

Edits through Nota (`set`, `tag`, the app, MCP, hooks) set `updated` in the
frontmatter to the current local time, but only in notes that already have
frontmatter; `created` is written once by `nota add`/`create_note`. `mv`
does not touch the file. `nota tag -x` edits the frontmatter only: inline
`#x` in the body stays.

## Layout

- `cmd/nota`: the CLI entry point (`nota mcp`, `nota serve` later)
- `internal/vault`: files (on an `FS`: `DirFS` or the in-memory `MemFS`), note model, discovery, ignore rules, NFC paths
- `internal/edit`: note edits shared by the CLI, MCP, hooks and the app
- `internal/frontmatter`: byte-stable frontmatter edits
- `internal/markdown`: tags and links from note bodies
- `internal/index`: SQLite index (FTS, tags, properties, links)
- `internal/views`: saved queries in `.nota/views/*.yaml`
- `internal/events`: core event bus
- `internal/mcp`: MCP tools and resources (transport-agnostic)
- `internal/hooks`: `.nota/hooks.toml` scripts
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
go test ./cmd/nota   # CLI scripts in cmd/nota/testdata/script
```
