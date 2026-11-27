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
nota ls --pinned --json                      # Pinned notes, in their pinned order.
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
nota pin Plan --json                                    # Last under Pinned; nota unpin Plan undoes it.
nota mv Plan Archive --json                             # Folder is created; / is the root.
nota mv Plan Projects/Roadmap.md                        # Renames; --no-update-links keeps links.
nota folder add Projects/Ideas                          # Empty folders, parents too.
nota folder rename Projects Work                        # Moves every note in it; links follow.
nota replace "Anna S" "Anna B" --dry-run --json         # Every match; then --yes to write.
nota rm Plan                                            # To .trash/; confirm with the user first.
nota trash ls --json                                    # Deleted notes: path in .trash/, from, deletedAt.
nota trash restore Plan --to Archive                    # Back where it was, or into --to; exit 5 if taken.
nota trash empty --older 30d --yes                      # For good: only with the user's say-so.
nota index --rebuild                                    # Only after big outside changes.
```

Listing sorts: `path`, `title`, `created`, `updated`, `modified` or `pinned`, with `asc` or
`desc`.

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

Adding an image: copy the file into `<vault>/Attachments/` (keep its name, add " 2" if taken)
and put `![[name.png]]` in the note. Syntax and where images are found:
[markdown.md](markdown.md).

## Find and replace

`nota replace <find> <replace>` changes note bodies across the vault. Run `--dry-run --json`
first and show the user the matches (an array of notes, each with `matches`: `line`, `text`,
`with` and the text around it), then the same command with `--yes`. With `--json`, one of
`--dry-run` or `--yes` is required.

- Case is ignored unless `--case`; `--word` matches whole words; `--folder` and `--tag` narrow.
- `--regex` reads `<find>` as RE2; in `<replace>`, `$1`, `${name}`, `$$`.
- Frontmatter and link targets are never touched (rename the note to change links), nor code
  unless `--code`. A note that changed between listing and writing makes the command fail.

## Pinning

A note is pinned when its frontmatter has `pinned:` set to a number (its place, lowest first) or
`true` (last). `nota pin` puts it last; `nota unpin` removes the key. Changing only `pinned` does
not touch `updated`. The app shows pinned notes at the top of the sidebar.

## Export and import

```sh
nota export Plan --json                          # Plan.html: one page, images inside.
nota export Plan --format md -o -                # Plain Markdown to stdout.
nota export Projects --format zip -o out/ --json # {path, notes, bytes}
nota export --format zip -o backup.zip           # The whole vault.

nota import obsidian ~/Documents/Obsidian --dry-run --json
nota import markdown ~/Downloads/Bear.zip --folder Bear --json
nota import apple-notes --json                   # macOS; asks once for permission.
```

Details, formats and the import report: [import-export.md](import-export.md).

## Opening for the user

`nota open Plan` opens `$VISUAL`/`$EDITOR` and blocks: do not run it from an agent.

`nota open --app <note|file>` (`-a`) hands it to the desktop app and returns at once, so an agent
can use it to show the user something:

- A note of the vault opens in Reading mode (for that open only).
- Any other Markdown file (`.md`, `.markdown`) by path opens as a read-only **preview**: nothing
  is copied into the vault or written to the file unless the user picks Save to vault…, Move file
  into vault… or Overwrite original file… in the app. Previews show no images.
- macOS goes through Launch Services; Linux runs `/opt/nota/nota`; `$NOTA_APP` overrides both.

## Other commands

- `nota settings --json` lists the user's settings and the layer each comes from;
  see [settings.md](settings.md).
- `nota completion zsh|bash|fish` for shell completion.
- `nota plugins` and `nota hooks`: see [themes.md](themes.md) and
  [views-and-hooks.md](views-and-hooks.md).
- `nota login`, `remote`, `sync`, `devices`, `history`, `restore`, `logout`: see
  [sync.md](sync.md).
- `nota serve`: see [server.md](server.md).
