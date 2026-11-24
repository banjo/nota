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
nota open Plan                                 # In $VISUAL/$EDITOR.
nota open --app Plan                           # In the desktop app, in Reading mode.
nota open --app ~/Downloads/README.md          # Any .md file: a read-only preview.
nota set Plan status=done priority=2           # Frontmatter, edited line by line.
nota tag Plan +nota -draft                     # Flags go before the note.
nota mv Plan Archive
nota folder add Projects/Ideas                  # Empty folders, nested paths too.
nota folder ls                                 # Every folder with its note count.
nota folder rename Projects Work                # Moves every note in it; links name notes, so none change.
nota inbox
nota ls --view drafts                          # .nota/views/drafts.yaml
nota plugins                                   # .nota/plugins: themes, or why one is broken
nota settings                                  # Theme, palette, note mode, properties, and where each comes from.
nota settings edit                             # ~/.config/nota/settings.toml, for your dotfiles.
nota backlinks Plan
nota index --rebuild
nota completion zsh > "${fpath[1]}/_nota"
```

Exit codes: 0 ok, 1 error, 2 usage, 3 not found, 4 ambiguous note reference, 5 target
exists or differs only in case.

## The vault

The vault is `--vault`, else `$NOTA_VAULT`, else `vault = "…"` in
`~/.config/nota/config.toml`, else `~/Notes`. The config also takes `inbox = "Inbox"`.

The desktop app shares that file: its first-run setup (and Settings › Vault › Switch vault…)
writes `vault`, `inbox` and `onboarded = true`, so the CLI picks up the chosen vault with no
environment variable. The app edits only those lines, atomically; comments and other keys stay as
they are. The setup runs only when no flag, no `$NOTA_VAULT` and no `vault` key chose a vault,
`onboarded` is not set, and `~/Notes` is missing or empty. With `$NOTA_VAULT` set, the variable
wins, so the app's vault switch has no effect until it is unset.

`config.toml` is per machine. The settings that follow you (theme, palette, note mode,
properties) are in `settings.toml` next to it and in your account: see [settings.md](settings.md).

- `.notaignore` (gitignore syntax) excludes files; `.git/ .obsidian/ .nota/ .trash/
  .DS_Store` are always ignored.
- The index lives in `<vault>/.nota/index.db` (SQLite: full-text search, tags, properties,
  links) and can be deleted at any time; it is rebuilt from the files.
- Paths are NFC-normalised, so a note named `Möte.md` is the same note on macOS and Linux.

## Opening files in the app

`nota open --app <note|file>` (short `-a`) hands a note, or any Markdown file (`.md`,
`.markdown`) by its path, to the desktop app. A note of the vault opens as that note in Reading
mode (for that open only; the mode setting is unchanged). A file outside the vault opens as a
read-only preview: nothing is copied into the vault or written to the file unless you choose
**Save to vault…**, **Move file into vault…** or **Overwrite original file…** in the app, and
**Edit** makes an in-memory draft. On macOS it goes through Launch Services
(`open -b com.banjoanton.nota`), so a running app takes the file and a quit one starts; on Linux
it runs `/opt/nota/nota` (the `.deb`/`.rpm` install). `$NOTA_APP` overrides both with a command
that gets the file's absolute path appended, such as an AppImage. Plain `nota open` still uses
the editor. Finder's **Open With ▸ Nota** and dropping a file on the Dock icon do the same
without the CLI; Nota is offered there but never made the default app for `.md`.

## Notes and timestamps

Edits through Nota (`set`, `tag`, the app, MCP, hooks) set `updated` in the frontmatter to the
current local time, but only in notes that already have frontmatter; `created` is written once
by `nota add`/`create_note`. `mv` does not touch the file. `nota tag -x` edits the frontmatter
only: an inline `#x` in the body stays. Frontmatter keys Nota does not know are kept byte for
byte.
