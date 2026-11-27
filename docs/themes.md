# Themes

Settings › Appearance has three kinds of colour theme:

- **Nota's own**: Nota (the default), Indigo, Graphite, Paper, Forest and Rose.
- **Classics**: Dracula, Nord, Solarized, Gruvbox, Catppuccin, Tokyo Night, Rosé Pine, One and
  GitHub. Each has a light and a dark side; Dracula's light side is Alucard.
- **This vault**: themes from the vault's plugins, which you can write yourself.

⌘K has "Theme: …" for each one. The theme you pick is kept per device. The mode (System, Light or
Dark) decides which side of the theme you see.

## Make your own

A theme lives in a plugin: a folder in the vault's `.nota/plugins/` with a `manifest.json`.
The folder syncs with the vault, so the theme shows up on your other devices and in the web app.
For example, `.nota/plugins/sunset/manifest.json`:

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

Each mode needs three colours. Nota works out the rest from them:

| Token        | What it colours                                           |
| ------------ | --------------------------------------------------------- |
| `background` | The page                                                  |
| `text`       | Body text                                                 |
| `ink`        | The accent: links, the selection, buttons, checked boxes |

Save the file and the app picks the theme up right away; the desktop app watches the folder.
Your theme shows under **This vault** in Settings › Appearance.

A theme can have only `light` or only `dark`. Choosing it then switches the app to that mode.

## Every token

Set any of these to override what Nota works out. The names match the CSS variables in
[`packages/ui/src/tokens.css`](../packages/ui/src/tokens.css) without the `--`.

| Token                                                       | What it colours                                         |
| ----------------------------------------------------------- | ------------------------------------------------------- |
| `foreground`                                                | Headings, titles, strong text                           |
| `text-soft`                                                 | Rows and values                                         |
| `muted-foreground`                                          | Labels and secondary text                               |
| `faint`                                                     | Placeholders, Markdown markers, list bullets            |
| `sidebar`                                                   | The sidebar                                             |
| `hairline`, `divider`                                       | Borders and separators                                  |
| `hover`, `selected`                                         | Rows under the pointer, and the selected row            |
| `chip`, `chip-foreground`                                   | Tags and small labels                                   |
| `file`                                                      | Note names in the sidebar tree                          |
| `code`                                                      | The background of code                                  |
| `surface`                                                   | Popovers, the ⌘K palette and the quick-capture card     |
| `overlay`                                                   | The backdrop behind dialogs                             |
| `ink-soft`, `ink-soft-hover`                                | Tinted backgrounds, such as tags in a note              |
| `ink-strong`                                                | Text on `ink-soft`                                      |
| `ink-foreground`                                            | Text on `ink`, such as a primary button's label         |
| `success`, `warning`, `danger`                              | Status colours                                          |
| `icon-red`, `icon-orange`, `icon-yellow`, `icon-green`      | Folder and view icon colours                            |
| `icon-blue`, `icon-purple`, `icon-pink`, `icon-gray`        | Folder and view icon colours                            |
| `md-heading`, `md-bold`, `md-italic`                        | Markdown in the editor                                  |
| `md-link`, `md-tag`, `md-code`, `md-quote`                  | Markdown in the editor                                  |
| `md-table-border`, `md-table-header`                        | Table borders, and the background of the header row     |
| `md-syntax-keyword`, `md-syntax-string`, `md-syntax-number` | Code in fenced blocks                                   |
| `md-syntax-comment`, `md-syntax-function`, `md-syntax-type` | Code in fenced blocks                                   |
| `md-callout-note`, `md-callout-tip`, `md-callout-important`  | Callouts (`> [!NOTE]`, `> [!info]` …)                   |
| `md-callout-warning`, `md-callout-caution`                  | Callouts                                                |
| `md-highlight`                                              | Behind `==highlighted==` text                           |

The `md-*` tokens give Markdown its own colours, like purple headings in Dracula. Without them,
headings use `foreground`, links use `ink`, tags use `ink-strong`, table borders use `hairline`
and table headers use `code`. Code colours are worked out from `ink`, `ink-strong`, `md-tag`,
`muted-foreground`, `success` and `warning`, darkened or lightened until they read on `code`. Callouts use `ink` (note), `md-syntax-string`
(tip), `ink-strong` (important), `md-syntax-number` (warning) and `danger` (caution).
Highlights are `warning` at 30% over the page. `icon-red`, `icon-yellow` and `icon-green`
default to `danger`, `warning` and `success`; an icon's accent colour is `ink`.

Colours are `#rgb`, `#rrggbb`, `#rrggbbaa`, `rgb()`, `rgba()`, `hsl()` or `hsla()`.
`background`, `text` and `ink` have to be hex without alpha, because Nota works out the other
colours from them.

For a full example, see the classics in
[`packages/app/src/theme/classics.ts`](../packages/app/src/theme/classics.ts). They use the same
format.

## Rules

- The plugin `id` is the folder name unless the manifest sets one. Ids use lowercase letters,
  digits and dashes. If two folders have the same id, the folder named after the id keeps it.
- Theme ids follow the same rules and must be unique within a plugin. The app knows a theme as
  `<plugin>.<theme>`, for example `sunset.sunset`.
- A manifest with an unknown field, an unknown token or something that isn't a colour does not
  load. Nota refuses `url()`, `var()` and colour names, so a theme can only change colours.
- A plugin can't run code yet. Today it can only add themes.

## Check a theme

```sh
nota plugins           # every plugin with its themes, or why it did not load
nota plugins --json
```

`nota plugins` exits with status 1 if a plugin is broken. Settings › Appearance lists the same
errors under the themes. It also warns about colours that are hard to read in your own themes:
body text under 4.5:1 against the background, ink under 3:1 against the page or the sidebar, and
button labels under 4.5:1 against the ink.
