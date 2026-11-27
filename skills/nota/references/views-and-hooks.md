# Views and hooks

Both live in the vault's `.nota/` folder and sync with it. Nota's own state there (`index.db`,
sync state) stays local and is not to be edited.

## Views

A view is a saved query: a virtual folder in the app's sidebar, `nota ls --view <id>` in the
CLI and `list_notes` with `view` over MCP. The id is the file name.

```yaml
# .nota/views/drafts.yaml
name: Drafts # required
icon: lucide:pen-line # as for folder icons, below
color: blue
where: { status: draft } # frontmatter values; case-insensitive, list items match
tag: projects # nested tags included
folder: Projects
sort: updated desc
limit: 50
```

Every field except `name` is optional. Check it with `nota views --json` (each view with its
note count) and `nota ls --view drafts --json`.

## Folder icons

A folder's icon in the sidebar, and its colour, are in `.nota/folders.yaml` by folder path. Set
them with `nota folder icon <folder> <icon> [--color <colour>]` (`--remove` to clear) or
`set_folder_icon`, not by hand: they check the icon and keep the file sorted.

- Icon: `<set>:<name>` from `lucide`, `tabler` or `simple-icons` (brand logos), or an emoji. A bare
  name is Lucide's. Find one with `nota icons <words> --json` or `search_icons`.
- Colour: `accent`, `red`, `orange`, `yellow`, `green`, `blue`, `purple`, `pink`, `gray`; the app
  draws each from the theme.
- Renaming a folder with Nota carries its icon along; `mv` in a shell does not.

## Hooks

Scripts in `.nota/hooks.toml` run when notes change, from the CLI, `nota mcp` and the desktop
app (and the server, when it enables them).

```toml
# File #work notes from the inbox into Work/ and tag them.
[[hook]]
name = "sort"
event = "inbox.added"
mode = "transform"
run = "sh scripts/sort.sh"
timeout = "2s"
match = { folder = "Inbox", tag = "work" }

# Log every save.
[[hook]]
event = "note.saved"
run = "cat >> .nota/saved.jsonl; echo >> .nota/saved.jsonl"
```

- `event`: `note.created`, `note.saved`, `note.moved`, `note.deleted`, `inbox.added` or
  `sync.completed`.
- `mode`: `after` (default) runs asynchronously and never blocks the write; `transform` runs
  before the write and may change the note. Transform hooks only run on `note.created` and
  `inbox.added`.
- `timeout`: default `2s` for transform, `30s` for after.
- `match`: optional `folder` and `tag` filters.
- `run` is executed with `sh -c` in the vault directory. Input: the event as JSON on stdin
  (`event`, `vault`, `path`, `title`, `tags`, `frontmatter`, `body`) and `$NOTA_VAULT`,
  `$NOTA_EVENT`, `$NOTA_NOTE`, `$NOTA_HOOK` in the environment.

A transform hook prints a patch on stdout; empty output means no change:

```json
{
  "set": { "status": "todo" },
  "unset": ["draft"],
  "tags": { "add": ["work"], "remove": [] },
  "folder": "Work"
}
```

The patch goes through the normal write path (events fire, `updated` is set) but never
triggers the same hook again. A hook that fails, times out or prints something that is not a
patch is logged and leaves the note unchanged.

Transform hooks do not run on notes that arrive through sync (they already ran on the device that
made them); `sync.completed` hooks run after every sync pass.

## Testing a hook

```sh
nota hooks list --json
nota hooks test inbox.added Inbox/x --json   # Runs the hooks on a note; applies nothing.
```

A `hooks.toml` that does not parse disables every hook (the CLI warns `hooks disabled` on
stderr), so run `nota hooks list` after each edit. Then run `nota hooks test` on a real note and
show the user the `patch` before relying on it.
