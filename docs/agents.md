# Agents (MCP)

AI is not built into Nota. Agents work with your notes through the CLI or MCP, on the same
files you edit.

## Skill

[`skills/nota`](../skills/nota/SKILL.md) is an agent skill that teaches an agent to use Nota:
the CLI or the MCP tools (whichever it has), Markdown and images, find and replace, pinning,
export and import, settings, views, hooks, themes, sync, sharing and the server. Install
it into Claude Code, OpenCode, Codex, Cursor and others with:

```sh
npx skills add banjo/nota
```

## Over stdio

`nota mcp` is a Model Context Protocol server on stdio. Add it to your MCP client:

```json
{
  "mcpServers": {
    "nota": { "command": "nota", "args": ["mcp"] }
  }
}
```

With Claude Code: `claude mcp add nota -- nota mcp`. Add `"--vault", "/path/to/vault"` to the
args to pick a vault other than the default.

## Tools and resources

| Tool | What it does |
| --- | --- |
| `search_notes` | full-text search, with snippets |
| `read_note` | a note, its frontmatter, links and hash |
| `list_notes` | a folder, a tag, a `where` clause, `pinned` or a view |
| `create_note` | a new note, in the inbox by default |
| `append_to_note` | adds text to the end of a note |
| `update_note` | replaces a body; pass `expected_hash` from `read_note` to avoid overwriting newer edits |
| `update_frontmatter` | properties and tags, line by line |
| `replace_in_notes` | find and replace across note bodies; lists the matches unless `dry_run` is `false` |
| `move_note` | files a note into a folder and rewrites the links that would break |
| `create_folder` | an empty folder (`a/b` makes both); fails if it exists |
| `list_folders` | every folder, empty ones too, with note counts |
| `set_folder_icon` | a folder's icon and colour in the sidebar |
| `search_icons` | icons by name, for `set_folder_icon` |
| `list_tags` | every tag with its count |
| `list_views` | the saved views in `.nota/views` |
| `get_backlinks` | the notes that link to a note |
| `list_trash` | the deleted notes in `.trash/`, with where each came from and when |
| `restore_note` | puts a deleted note back where it was, or into another folder |

Results are structured JSON; failures come back as tool errors. Each note is also a resource at
`nota://note/<path>` (percent-encode non-ASCII names, e.g. `nota://note/M%C3%B6ten/Plan.md`).

## Over HTTP

The sync server (`nota serve`, see [deploy.md](deploy.md)) offers the same tools at `/mcp`
(Streamable HTTP, stateless), on the server's copy of a synced vault. Authenticate with a device
token; pick the vault with `?vault=name` or the `Nota-Vault` header (default: your first vault).
Writes become versions of that token's device and sync to every machine:

```sh
claude mcp add --transport http nota https://nota.example.com/mcp \
  --header "Authorization: Bearer <device token>"
```
