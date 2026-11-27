# Settings

The user's app settings (mode, colour theme, note mode, properties, backlinks) live in layers;
the highest layer that sets a key wins:

| Layer   | Where                          | Written by                          |
| ------- | ------------------------------ | ----------------------------------- |
| File    | `~/.config/nota/settings.toml` | the user or an agent; never the app |
| Account | the sync server                | the app while signed in             |
| Device  | the app's storage              | the app while signed out            |

```toml
#:schema https://nota.banjoanton.com/settings.schema.json
theme = "dark" # system, light or dark
palette = "paper" # a theme id: nota, paper, dracula, … or <plugin>.<theme> from .nota/plugins
note_mode = "live" # live, reading or source
properties = "hide" # hide or show
backlinks = "hide" # hide or show "Linked from" under a note
```

- A key in the file is **locked**: the app shows "Set in settings.toml" and won't change it. To
  let the user change a setting in the app again, remove the key. Set only the keys the user asks
  for.
- The path is `$XDG_CONFIG_HOME/nota/settings.toml`, else `~/.config/nota/settings.toml`
  (`nota settings path`). It may be a symlink into dotfiles: edit the target.
- The desktop app applies edits at once. An unknown key or value is a warning and falls back;
  invalid TOML disables the whole file.

```sh
nota settings --json            # Every key, its value and the layer it comes from.
nota settings --account --json  # Also the account's values (needs a sync remote).
```

The CLI can't see the device layer. `config.toml` next to it is per machine (`vault`, `inbox`,
`onboarded`) and is not a setting; `credentials` there holds tokens: leave it alone.
