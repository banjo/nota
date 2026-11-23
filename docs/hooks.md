# Views and hooks

Both live in the vault's `.nota/` folder and sync like any other file, so a view or hook added
on one machine works on the others too. Nota's own state in `.nota/` (index, sync state) stays
local.

## Views

A view is a saved query, shown as a virtual folder in the app's sidebar and listed with
`nota ls --view <id>` (the id is the file name):

```yaml
# .nota/views/drafts.yaml
name: Drafts
icon: pen
where: { status: draft }
tag: projects
folder: Projects
sort: updated desc
limit: 50
```

Every field except `name` is optional. `where` matches frontmatter values (case-insensitive,
list items included); `tag` includes nested tags. `nota views` lists them.

## Hooks

Scripts in `.nota/hooks.toml` run when notes change, like git hooks. They get the event as JSON
on stdin (`event`, `vault`, `path`, `title`, `tags`, `frontmatter`, `body`) and `$NOTA_VAULT`,
`$NOTA_EVENT`, `$NOTA_NOTE`, `$NOTA_HOOK`, and run with the vault as working directory
(`sh -c`).

```toml
# File #work notes from the inbox into Work/ and tag them.
[[hook]]
name = "sort"
event = "inbox.added" # note.created note.saved note.moved note.deleted inbox.added sync.completed
mode = "transform" # "after" (default): async, never blocks
run = "sh scripts/sort.sh"
timeout = "2s" # default 2s for transform, 30s for after
match = { folder = "Inbox", tag = "work" }

# Log every save.
[[hook]]
event = "note.saved"
run = "cat >> .nota/saved.jsonl; echo >> .nota/saved.jsonl"
```

`scripts/sort.sh` prints a patch; empty output means no change:

```sh
#!/bin/sh
echo '{"folder": "Work", "tags": {"add": ["sorted"]}, "set": {"status": "todo"}}'
```

Transform hooks run only on `note.created` and `inbox.added`. Their patch (`set`, `unset`,
`tags.add`/`tags.remove`, `folder`) goes through the normal write path, so events fire and
`updated` is set, but a hook's own change never triggers it again. A hook that fails, times out
or prints something that is not a patch is logged and leaves the note unchanged.

`nota hooks list` shows the hooks; `nota hooks test inbox.added Inbox/x` runs them on a note
without applying anything. Hooks run in the CLI, `nota mcp` and the desktop app. Transform hooks
do not run on notes that arrive through sync (they ran on the device that made them); hooks on
`sync.completed` run after every sync pass. The server can run them too, see
`NOTA_SERVER_HOOKS` in [deploy.md](deploy.md).
