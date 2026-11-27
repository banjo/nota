# Nota over MCP

## Connecting

`nota mcp` serves the vault over stdio. Generic MCP client config:

```json
{
  "mcpServers": {
    "nota": { "command": "nota", "args": ["mcp"] }
  }
}
```

- Claude Code: `claude mcp add nota -- nota mcp`
- Another vault than the default: add `"--vault", "/path/to/vault"` to `args`
  (`claude mcp add nota -- nota mcp --vault ~/Work`).

A sync server (`nota serve`) offers the same tools over Streamable HTTP at `/mcp`, on the
server's copy of a synced vault. It needs a device token (`nota serve token create`, see
[server.md](server.md)); pick the vault with `?vault=name` or the `Nota-Vault` header (default:
the account's first vault). Writes become versions of that device and sync to every machine.

```sh
claude mcp add --transport http nota https://nota.example.com/mcp \
  --header "Authorization: Bearer <device token>"
```

The client must be restarted, or the server reloaded, before the tools appear.

## Tools

Every `note` argument takes a path, a title, a unique prefix or a `nota://note/` URI. Results
are structured JSON; failures come back as tool errors with a readable message.

| Tool                 | Arguments                                                                                            | Notes                                                                                                                         |
| -------------------- | ---------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `search_notes`       | `query`, `folder?`, `tag?`, `limit?` (20)                                                            | Best matches first, with `snippet`.                                                                                           |
| `read_note`          | `note`                                                                                               | Properties, tags, `body`, `content` and `hash`.                                                                               |
| `list_notes`         | `folder?`, `tag?`, `where?` (`["k=v", "k!=v"]`), `view?`, `pinned?`, `sort?`, `limit?` (100)         | Sort: `path`, `title`, `created`, `updated`, `modified`, `pinned` + `asc`/`desc`. `pinned: true` lists pinned notes in order. |
| `create_note`        | `title?`, `body?`, `folder?`, `tags?`, `properties?`                                                 | Title becomes `# Title` and the file name; without it the first body line is the title. Returns where the note ended up.      |
| `append_to_note`     | `note`, `text`                                                                                       | Added on a line of its own; start `text` with an empty line for a new paragraph.                                              |
| `update_note`        | `note`, `body`, `expected_hash?`                                                                     | Replaces the body; the frontmatter is kept.                                                                                   |
| `update_frontmatter` | `note`, `set?` (JSON values), `unset?`                                                               | Tags: `set: {"tags": [...]}` replaces the whole list.                                                                         |
| `replace_in_notes`   | `find`, `replace`, `regex?`, `case?`, `word?`, `include_code?`, `folder?`, `tag?`, `dry_run?` (true) | Bodies only. A dry run lists each match with its line; show the user, then call again with `dry_run: false`.                  |
| `move_note`          | `note`, `folder`                                                                                     | Creates the folder; `/` is the root. Fails if the name is taken there. Rewrites links to it; `links` says which notes.        |
| `create_folder`      | `path`                                                                                               | `a/b` makes both; fails if it exists. Rarely needed: create and move make folders.                                            |
| `list_folders`       | none                                                                                                 | Every folder, empty ones too, with note counts.                                                                               |
| `set_folder_icon`    | `path`, `icon?`, `color?`                                                                            | The folder's icon in the sidebar (an id from `search_icons` or an emoji) and colour; both empty take them away.               |
| `search_icons`       | `query?`, `set?`, `limit?` (40)                                                                      | Icons by name, best first: `lucide`, `tabler`, `emoji`, `simple-icons` (brand logos). `id` is what `set_folder_icon` takes.   |
| `list_tags`          | none                                                                                                 | Every tag with its count.                                                                                                     |
| `list_views`         | none                                                                                                 | Saved views; pass an `id` as `view` to `list_notes`.                                                                          |
| `get_backlinks`      | `note`                                                                                               | Notes linking here via wikilinks, embeds or Markdown links.                                                                   |
| `list_trash`         | none                                                                                                 | Deleted notes in `.trash/`: `path` there, `from`, `deletedAt`, newest first.                                                  |
| `restore_note`       | `note`, `folder?`                                                                                    | `note` is a trash `path` or the `from` path. Back where it was, or into `folder`; fails if a note has taken the path.         |

There is no delete, empty-trash, folder-rename, export, import or file-upload tool. For those, use
the CLI (`nota rm`, `nota trash empty`, `nota folder rename`, `nota export`, `nota import`, a file
copy into `Attachments/`) after confirming with the user.

Pin a note with `update_frontmatter` `set: {"pinned": true}` (or a number for its place); unpin
with `unset: ["pinned"]`.

## Safe rewrites

1. `read_note` and keep `hash`.
2. Build the new body from that read.
3. `update_note` with `expected_hash`. If it fails because the note changed, go back to 1.

Tags: take `properties.tags` from `read_note` (the top-level `tags` also holds inline `#tags`),
add or remove, then `update_frontmatter` with the full list. Inline `#tags` stay until the body
is edited.

## Resources

Each note is a resource at `nota://note/<path>` (`text/markdown`, the whole file).
Percent-encode non-ASCII names: `nota://note/M%C3%B6ten/Plan.md`.
