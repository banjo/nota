# Themes

The app has built-in themes (Nota's own and classics such as Dracula, Nord, Solarized, Gruvbox,
Catppuccin, Tokyo Night, Rosé Pine, One and GitHub), picked in Settings › Appearance or ⌘K
"Theme: …". A custom theme is a vault plugin: a folder in `.nota/plugins/` with a
`manifest.json`. It syncs with the vault and the desktop app picks it up on save. For example,
`.nota/plugins/sunset/manifest.json`:

```json
{
  "name": "Sunset",
  "version": "1.0.0",
  "contributes": {
    "themes": [
      {
        "id": "sunset",
        "name": "Sunset",
        "light": { "background": "#fff8f0", "text": "#3a2a20", "ink": "#b2410c" },
        "dark": { "background": "#1f1612", "text": "#f2e6dc", "ink": "#fb923c" }
      }
    ]
  }
}
```

## Rules

- Each mode needs `background`, `text` and `ink` (the accent), as hex without alpha. Nota derives
  every other colour from them. A theme may have only `light` or only `dark`.
- Optional overrides, as `#rgb`, `#rrggbb`, `#rrggbbaa`, `rgb()`, `rgba()`, `hsl()` or `hsla()`:
  `foreground`, `text-soft`, `muted-foreground`, `faint`, `sidebar`, `hairline`, `divider`,
  `hover`, `selected`, `chip`, `chip-foreground`, `file`, `code`, `surface`, `overlay`,
  `ink-soft`, `ink-soft-hover`, `ink-strong`, `ink-foreground`, `success`, `warning`, `danger`,
  `md-heading`, `md-bold`, `md-italic`, `md-link`, `md-tag`, `md-code`, `md-quote`.
- An unknown field, an unknown token or a value that is not a colour (`url()`, `var()`, colour
  names) makes the whole plugin fail to load.
- Plugin and theme ids are lowercase letters, digits and dashes. The plugin id is the folder
  name unless the manifest sets `id`; the app knows the theme as `<plugin>.<theme>`.
- Plugins cannot run code; today they only add themes.

## Check it

```sh
nota plugins --json   # Every plugin with its themes, or why it did not load; exit 1 if broken.
```

Aim for readable contrast: body text at least 4.5:1 against the background, `ink` at least 3:1
against the page and sidebar, and `ink-foreground` at least 4.5:1 against `ink`. The app warns
when a vault theme misses these.
