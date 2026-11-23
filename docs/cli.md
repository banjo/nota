# The CLI

`nota` is one binary: the CLI, `nota mcp` (MCP over stdio) and `nota serve` (the sync
server). Every command takes `--json` (errors then go to stderr as JSON too) and
`--verbose`; `nota <command> --help` has the details.

```sh
nota add "Buy milk" --tag shopping            # New note in Inbox/.
echo "# Plan" | nota add --folder Projects -   # Body from stdin.
nota ls --tag projects --where status=draft --sort "updated desc"
nota search "meeting anna"
nota search -- plan -draft                     # Exclude a word.
nota cat Plan                                  # By path, title or unique prefix.
nota set Plan status=done priority=2           # Frontmatter, edited line by line.
nota tag Plan +nota -draft                     # Flags go before the note.
nota mv Plan Archive
nota inbox
nota ls --view drafts                          # .nota/views/drafts.yaml
nota backlinks Plan
nota index --rebuild
nota completion zsh > "${fpath[1]}/_nota"
```

Exit codes: 0 ok, 1 error, 2 usage, 3 not found, 4 ambiguous note reference, 5 target
exists or differs only in case.

## The vault

The vault is `--vault`, else `$NOTA_VAULT`, else `vault = "…"` in
`~/.config/nota/config.toml`, else `~/Notes`. The config also takes `inbox = "Inbox"`.

- `.notaignore` (gitignore syntax) excludes files; `.git/ .obsidian/ .nota/ .trash/
  .DS_Store` are always ignored.
- The index lives in `<vault>/.nota/index.db` (SQLite: full-text search, tags, properties,
  links) and can be deleted at any time; it is rebuilt from the files.
- Paths are NFC-normalised, so a note named `Möte.md` is the same note on macOS and Linux.

## Notes and timestamps

Edits through Nota (`set`, `tag`, the app, MCP, hooks) set `updated` in the frontmatter to the
current local time, but only in notes that already have frontmatter; `created` is written once
by `nota add`/`create_note`. `mv` does not touch the file. `nota tag -x` edits the frontmatter
only: an inline `#x` in the body stays. Frontmatter keys Nota does not know are kept byte for
byte.
