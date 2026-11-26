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
nota pin Plan                                  # Under Pinned in the sidebar; nota unpin Plan undoes it.
nota ls --pinned                               # Pinned notes, in their order.
nota mv Plan Archive                           # Links to it that would break are rewritten.
nota mv Plan Projects/Roadmap.md               # A path ending in .md renames.
nota replace "Anna S" "Anna B" --dry-run      # Every match, as [-old-]{+new+}; drop --dry-run to replace.
nota folder add Projects/Ideas                  # Empty folders, nested paths too.
nota folder ls                                 # Every folder with its note count.
nota folder rename Projects Work                # Moves every note in it; path links follow.
nota inbox
nota ls --view drafts                          # .nota/views/drafts.yaml
nota plugins                                   # .nota/plugins: themes, or why one is broken
nota settings                                  # Theme, palette, note mode, properties, and where each comes from.
nota settings edit                             # ~/.config/nota/settings.toml, for your dotfiles.
nota backlinks Plan
nota rm Plan                                   # Into .trash/; nothing is deleted.
nota trash ls                                  # What's in it, newest delete first.
nota trash restore Plan                        # Back where it was (--to <folder> for elsewhere).
nota trash empty --older 30d --yes             # Delete for good.
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
by `nota add`/`create_note`. `mv` does not touch the moved file, except for its own relative
links. Pinning (below) is not an edit either. `nota tag -x` edits the frontmatter
only: an inline `#x` in the body stays. Frontmatter keys Nota does not know are kept byte for
byte.

## Pinned notes

A note is pinned when its frontmatter has `pinned:` set to a number or `true`. The app lists
pinned notes under **Pinned** at the top of the sidebar and first in ⌘K, lowest number first and
`pinned: true` last. Pin and unpin from a note's menu, ⌘K or ⌘⇧P in the app, `nota pin` and
`nota unpin`, or `update_frontmatter` over MCP; drag a pinned note in the sidebar to reorder.

`nota pin` puts the note last (ten past the highest number); reordering in the app picks a number
between the neighbours, so it usually rewrites one file. A change that only touches `pinned`
leaves `updated` alone. As it is frontmatter, the pin syncs with the note and stays through a
move or rename. `nota ls --pinned` and `list_notes` with `pinned: true` list them in order;
`--sort pinned` sorts any list that way.

## Moving and renaming

`nota mv`, `nota folder rename`, `move_note` and a rename or move in the app keep every link
pointing at the same note:

- A link that names the note by its file name (`[[Plan]]`) changes only when the name does.
- A link with a path (`[[Projects/Plan]]`, `[plan](Projects/Plan.md)`) follows the note, and a
  moved note's own relative links (`[x](../Other.md)`, `![](img/a.png)`) are rewritten from
  its new folder. `[[Projects/Plan]]` stays when it still names the note by the end of its path.
- A link that meant another note with the same name keeps meaning it: when the moved note is now
  the closer match, `[[Plan]]` becomes `[[Other/Plan]]`.
- Only the target changes: `#heading`, `#^block`, `|alias` and the `!` of an embed stay as
  written; a Markdown path keeps its `<…>`, or gets `%20` for spaces without them. Links in code are left alone. Each changed note is saved like an
  edit, so `updated` moves and sync and hooks see it.

`--no-update-links` turns this off for one command. Without it, the command says on stderr how
many links changed, and `--json` lists them under `links`.

## Find and replace

`nota replace <find> <replace>` replaces text in the bodies of notes. It lists every match on its
line as `[-old-]{+new+}` and asks before writing; `--yes` writes without asking, `--dry-run` only
lists (with `--json`, one of them is required). Each changed note is saved like an edit.

- Case is ignored unless `--case`; `--word` matches whole words; `--folder` and `--tag` narrow
  the notes.
- `--regex` reads `<find>` as an [RE2](https://github.com/google/re2/wiki/Syntax) expression. In
  `<replace>`, `$1` is the first group (`$1st` too), `${name}` a named group and `$$` a `$`.
- Frontmatter is never touched (use `nota set` and `nota tag`), nor where a link points: rename
  the note instead, and the links follow. Code blocks and inline code are skipped unless `--code`.
- A note that changes between the listing and the write is left alone, and the command fails
  naming it.

The same replace is `replace_in_notes` over MCP (a dry run unless `dry_run: false`) and ⌘⇧F in the
app, where each match can be unticked and the toast has Undo.

## Trash

`nota rm`, deleting in the app and deletes synced from other devices move notes to `.trash/` in
the vault, keeping their folders; nothing is deleted. Nota records where each came from and
when in `.nota/trash.json` (local, never synced); a file put in `.trash/` by hand lists with its
modification time.

- `nota trash ls` lists them, most recently deleted first (`--json`: `path` in the trash,
  `from`, `deletedAt`, `size`).
- `nota trash restore <note>` puts one back where it was. The note is named by its path in the
  trash (`Projects/Plan.md` or `.trash/Projects/Plan.md`) or by the path it was deleted from,
  the newest delete first. It refuses (exit 5) to overwrite a note that has taken the path:
  `--to <folder>` restores it elsewhere, `--to <name>.md` under another name.
- `nota trash rm <note> --yes` deletes one for good; `nota trash empty --yes` deletes them all,
  or with `--older 30d` (also `2w`, `12h`) only those deleted longer ago than that.

