# The nota CLI

One binary: the CLI, `nota mcp` and `nota serve`. `nota <command> --help` is the authority on
flags; this file covers what an agent needs around it.

## Calling it

- Pass `--json` on every command. Results go to stdout as JSON; errors go to stderr as
  `{"error":{"code":"not_found","message":"…"}}`.
- Exit codes: 0 ok, 1 error, 2 usage, 3 not found, 4 ambiguous note reference, 5 target exists
  or differs only in case.
- The vault is `--vault <dir>`, else `$NOTA_VAULT`, else `vault = "…"` in
  `~/.config/nota/config.toml`, else `~/Notes`. To see which one is used:
  `nota ls --verbose 2>&1 >/dev/null | grep 'msg=vault'` (the `dir=` field).
- Flags go before positional arguments when an argument starts with `-` (tags, search
  exclusions): `nota tag --json Plan +a -b`, `nota search --json -- plan -draft`.

## Commands

```sh
nota search "meeting anna" --json            # Best matches first, with snippets.
nota ls --tag projects --where status=draft --sort "updated desc" --limit 20 --json
nota ls --view drafts --json                 # A saved view; other filters narrow it.
nota inbox --json                            # Unsorted notes, newest first.
nota cat Plan --json                         # path, properties, tags, body, content.
nota backlinks Plan --json                   # [{from, to, target}]
nota tags --json
nota folder ls --json                        # Every folder, empty ones too, with counts.
nota views --json

nota add "Call the plumber" --tag home --json           # Title only.
printf '# Plan\n\nFirst draft.\n' | nota add --folder Projects --tag work --json -
nota set Plan status=done priority=2 --json             # Values are YAML: 2, true, [a, b].
nota set Plan "aliases=[plan, planen]" --unset draft --json
nota tag --json Plan +nota -draft                       # Without changes: prints the tags.
nota mv Plan Archive --json                             # Folder is created; / is the root.
nota folder add Projects/Ideas                          # Empty folders, parents too.
nota folder rename Projects Work                        # Moves every note in it.
nota rm Plan                                            # To .trash/; confirm with the user first.
nota index --rebuild                                    # Only after big outside changes.
```

Listing sorts: `path`, `title`, `created`, `updated` or `modified`, with `asc` or `desc`.

A note in JSON has `path`, `folder`, `title`, `tags`, `properties` (the decoded frontmatter),
`created`, `updated`, `modified` and `size`; `cat` adds `body` and `content`, `search` adds
`snippet`.

## Editing a body

The CLI has no body-editing command besides `nota open` (an interactive editor). Notes are plain
files, so edit `<vault>/<path>` directly with your file tools, using the `path` from `nota ls` or
`nota cat --json`:

- Keep the frontmatter block as it is; change properties with `nota set` and `nota tag`.
- Appending: add text on a new line at the end of the file.
- Read the file right before you write it, so you do not overwrite an edit made in the app or by
  sync in between.

The index notices the change on the next command. A direct file edit does not set `updated` or
fire hooks; `nota set` and `nota tag` do.

## Opening for the user

`nota open Plan` opens `$VISUAL`/`$EDITOR` and blocks: do not run it from an agent. `nota open
--app Plan` hands the note to the desktop app (Reading mode), and `nota open --app <file.md>`
shows any Markdown file as a read-only preview without copying it into the vault.

## Other commands

- `nota completion zsh|bash|fish` for shell completion.
- `nota plugins` and `nota hooks`: see [themes.md](themes.md) and
  [views-and-hooks.md](views-and-hooks.md).
- `nota login`, `remote`, `sync`, `devices`, `history`, `restore`, `logout`: see
  [sync.md](sync.md).
- `nota serve`: see [server.md](server.md).
