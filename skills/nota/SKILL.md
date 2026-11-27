---
name: nota
description: Read, write and organise notes in a Nota vault (plain Markdown files with YAML frontmatter) through the `nota` CLI or the Nota MCP tools. Use when the user mentions Nota, their notes or vault, inbox, capturing or filing a note, images in notes, importing notes from Obsidian, Apple Notes, Bear or Notion, exporting notes, or asks to set up Nota's MCP server, settings, views, hooks, themes, sync or a self-hosted Nota server.
---

# Nota

Nota is a vault of plain `.md` files with optional YAML frontmatter. The files are the truth;
`.nota/index.db` is a disposable SQLite index for search, tags, properties and links. The same
vault is shared by the CLI, `nota mcp`, the desktop app, the web app and, through sync, other
machines.

## Pick the interface

1. If tools such as `search_notes`, `read_note` and `create_note` are available, the Nota MCP
   server is connected: use them. Details in [references/mcp.md](references/mcp.md).
2. Otherwise use the `nota` CLI with `--json` on every command. Details in
   [references/cli.md](references/cli.md).
3. If neither works (`nota: command not found`), tell the user how to install it:
   `brew install --cask banjo/tap/nota` (macOS) or
   `go install github.com/banjo/nota/cmd/nota@latest`. To connect MCP, see
   [references/mcp.md](references/mcp.md).

| Task                 | MCP tool                                  | CLI                                              |
| -------------------- | ----------------------------------------- | ------------------------------------------------ |
| Find by content      | `search_notes`                            | `nota search <query> --json`                     |
| Read                 | `read_note`                               | `nota cat <note> --json`                         |
| List / filter        | `list_notes`                              | `nota ls --folder/--tag/--where/--view/--pinned` |
| Unsorted notes       | `list_notes` with `folder: "Inbox"`       | `nota inbox --json`                              |
| Create               | `create_note`                             | `nota add` (body on stdin with `-`)              |
| Add to the end       | `append_to_note`                          | edit the file (see cli.md)                       |
| Rewrite the body     | `update_note` with `expected_hash`        | edit the file (see cli.md)                       |
| Replace across notes | `replace_in_notes` (dry run first)        | `nota replace <find> <with> --dry-run`           |
| Properties           | `update_frontmatter`                      | `nota set <note> k=v --unset k`                  |
| Tags                 | `update_frontmatter` (`tags` list)        | `nota tag <note> +a -b`                          |
| Pin to the sidebar   | `update_frontmatter` (`pinned: true`)     | `nota pin <note>`, `nota unpin <note>`           |
| File into a folder   | `move_note`                               | `nota mv <note> <folder>`                        |
| Folders, tags, views | `list_folders`, `list_tags`, `list_views` | `nota folder ls`, `nota tags`, `nota views`      |
| Who links here       | `get_backlinks`                           | `nota backlinks <note>`                          |
| Add an image         | none                                      | copy into `Attachments/`, embed `![[name.png]]`  |
| Export / import      | none                                      | `nota export`, `nota import` (dry run first)     |
| Show the user a note | none                                      | `nota open --app <note or file.md>`              |
| Delete               | none: ask the user                        | `nota rm <note>` (moves it to `.trash/`)         |
| Undo a delete        | `list_trash`, then `restore_note`         | `nota trash ls`, `nota trash restore <note>`     |

## The model

- **Note reference**: a vault path (`Projects/Plan.md`, `.md` optional), a title, or a unique
  prefix. A reference that matches several notes fails as ambiguous; retry with the full path
  from a listing. Prefer paths once you have them.
- **Title**: the first line of the body (a `# Heading` or plain text) is the title and the file
  name. To create a note with a body, start the content with `# Title` and a blank line.
- **Inbox**: new notes land in the inbox folder (`Inbox` by default) unless a folder is given;
  `/` means the vault root. Hooks may file a new note elsewhere, so use the path the command
  returns, not the one you expected.
- **Frontmatter**: `tags`, `created`, `updated`, `pinned` and any custom keys (`status`,
  `priority`, …). Nota edits only the lines of the keys it changes and keeps everything else byte
  for byte. `created` is written once on create; `updated` is set on every edit, but only in
  notes that already have frontmatter.
- **Tags** come from the frontmatter `tags` list and inline `#tag` in the body; `#a/b` is a
  nested tag and filtering by `a` includes it. Removing a tag edits only the frontmatter.
- **Links**: `[[Title]]`, `[[path|alias]]`, `[[Note#heading]]`, embeds `![[x]]` and
  `[text](other.md)`. Moving or renaming a note or folder through Nota rewrites the links that
  would break; moving files by hand does not.
- **Images** live in `Attachments/` and are embedded with `![[name.png]]`. Syntax, callouts and
  image rules: [references/markdown.md](references/markdown.md).
- **Search query**: all words must occur (the last as a prefix), `"quoted phrase"` is exact,
  `-word` excludes, `OR` means either. Filters: folder, tag, `where key=value` / `key!=value`
  (case-insensitive, list items match).

## Working rules

- Search or list before creating, so you add to an existing note instead of making a duplicate.
- Edit with the narrowest tool: `update_frontmatter`/`set`/`tag` for properties, append for
  logs and lists, a full body rewrite only when the content itself changes.
- Before rewriting a body over MCP, `read_note` and pass its `hash` as `expected_hash`. If the
  update fails because the note changed, read it again and redo the edit on the new content.
- Rename and move with `move_note` / `nota mv`, so links follow.
- Bulk changes (replace, import) run as a dry run first; show the user the result, then write.
- Leave `.nota/index.db`, `.nota/sync.toml`, `.nota/state.db` and conflict copies alone.
  Conflict copies are named `Note (conflict <date> <device>).md`; merge them only when asked.
- Deleting is the user's call: confirm before `nota rm` or emptying the trash.

## More

Read the reference that matches the task:

- Markdown syntax, images, embeds and the app's note modes:
  [references/markdown.md](references/markdown.md).
- Exporting (HTML, Markdown, zip, PDF) and importing from Obsidian, Apple Notes, Bear, Notion or
  Logseq: [references/import-export.md](references/import-export.md).
- The user's app settings (`settings.toml`, theme, note mode):
  [references/settings.md](references/settings.md).
- Saved views (`.nota/views/*.yaml`) and hooks that run scripts on note events
  (`.nota/hooks.toml`): [references/views-and-hooks.md](references/views-and-hooks.md).
- Colour themes as vault plugins (`.nota/plugins/<id>/manifest.json`):
  [references/themes.md](references/themes.md).
- Syncing a vault, devices, conflicts, version history, restore and sharing a note as a web page:
  [references/sync.md](references/sync.md).
- Running the sync server (`nota serve`, Docker, accounts, tokens, blobs, backups):
  [references/server.md](references/server.md).
