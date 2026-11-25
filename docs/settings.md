# Settings

Nota's settings are the choices you want on every machine: the mode, the colour theme, how notes
open and whether properties show. They live in layers, and the highest layer that sets a key wins:

| Layer | Where | Written by |
| --- | --- | --- |
| File | `~/.config/nota/settings.toml` | you, by hand or from your dotfiles; never the app |
| Account | your account on the sync server | the app (Settings, ⌘K) while signed in |
| Device | the app's storage on one machine | the app while signed out |
| Default | Nota | – |

Signed in, a change in Settings or ⌘K goes to your account and reaches your other devices within a
moment. Signed out, it stays on the device. The first time a device signs in to an account that has
no settings yet, the device's choices go up to the account; after that the account wins.

On the desktop the account is the open vault's: a vault you sync with `nota remote add` or Settings
› Sync uses that account. The web app has the account layer only.

## settings.toml

```toml
#:schema https://nota.banjoanton.com/settings.schema.json
theme = "dark"         # system, light or dark
palette = "paper"      # a theme id: nota, paper, forest, dracula, … or one from .nota/plugins
note_mode = "live"     # live, reading or source
properties = "hide"    # hide or show
backlinks = "hide"     # hide or show "Linked from" under a note
```

The file is `$XDG_CONFIG_HOME/nota/settings.toml`, else `~/.config/nota/settings.toml`.

- **A key set in the file is locked.** Settings shows it with "Set in settings.toml" and doesn't
  let you change it there. A shortcut that changes it (⌘E for the note mode, the dark-mode toggle)
  still works, but only until the window reloads: the file's value comes back.
- **Leave a key out to let the app and your account decide it.**
- The desktop app watches the file, and the file it links to when it is a symlink, so an edit
  applies in every window at once.
- An unknown key or a value Nota doesn't know is a warning, not an error: the key falls back to the
  layer below. That way a file written for a newer Nota works in an older one. A file that isn't
  valid TOML is ignored as a whole, with a warning in Settings.

Settings › Appearance › **Copy as TOML** copies your current settings as a `settings.toml`, ready to
paste into your dotfiles.

### In your dotfiles

Keep `settings.toml` in your dotfiles, not the whole `~/.config/nota/` folder: `config.toml` there
holds this machine's vault path, and `credentials` holds sign-in tokens.

```sh
ln -s ~/dotfiles/nota/settings.toml ~/.config/nota/settings.toml
```

### Completion in your editor

The `#:schema` line points editors with a TOML language server (Taplo, Even Better TOML) at
[settings.schema.json](https://nota.banjoanton.com/settings.schema.json), so they complete the keys
and check the values.

## The CLI

```sh
nota settings              # every key, its value and the layer it comes from
nota settings --account    # the account's values too (needs a sync remote or $NOTA_SERVER_URL)
nota settings --json
nota settings path         # where settings.toml is
nota settings edit         # open it in $VISUAL or $EDITOR; creates a commented template first
```

The CLI can't see the device layer: that lives in the app.

## What is not a setting

The vault path and `onboarded` stay in `config.toml` on each machine, and the inbox folder stays
there too. The sidebar, the open note, the folders you expanded and other window state stay on the
device.

The account's settings API is in [api.md](api.md#settings).
