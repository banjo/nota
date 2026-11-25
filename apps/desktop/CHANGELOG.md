# @nota/desktop

## 0.1.3

### Patch Changes

- [#43](https://github.com/banjo/nota/pull/43) [`250e810`](https://github.com/banjo/nota/commit/250e810256c4855b921a2c21f8ca403c4448929d) - Images and embedded notes: `![](image.png)` and `![[image.png|300]]` show the image from the vault, and `![[Note]]` or `![[Note#Heading]]` embeds the note. Pasting or dropping an image saves it in `Attachments/` and links it; images sync like notes. Images from the web are never loaded. See docs/markdown.md.

- [#47](https://github.com/banjo/nota/pull/47) [`625a67f`](https://github.com/banjo/nota/commit/625a67f051c6ef88aecd5f55af30c629877d96ae) - "Linked from" is hidden by default behind a chip with the count, like Properties, and shows nothing when no note links here. Toggle it from the chip, ⌘K or Settings, or set `backlinks = "show"` in settings.toml.

- [#36](https://github.com/banjo/nota/pull/36) [`6b02ca3`](https://github.com/banjo/nota/commit/6b02ca34388a65429ef291ea6f5085cb14a81b63) - Callouts: GitHub alerts (`> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]`) and Obsidian callouts (`> [!info] Title`) show with an icon, a title and their own colour in every theme. Themes can set the colours with the `md-callout-*` tokens.

- [#33](https://github.com/banjo/nota/pull/33) [`2bf4313`](https://github.com/banjo/nota/commit/2bf4313761c42191a556bfd2fdd035330e148d2c) - Footnotes: `[^id]` shows as a raised number and `[^id]: text` as a quiet note line. Click a number to jump to its note, and the note's number to jump back.

- [#41](https://github.com/banjo/nota/pull/41) [`865dafa`](https://github.com/banjo/nota/commit/865dafa0b7a8414ca168dfe520dd7aa552b61800) - More Markdown shows as it should: links to headings and blocks (`[text](#heading)`, `[[Note#Heading]]`, `[[#^id]]`) and to other notes (`[text](Other.md)`) open in Nota and scroll there, reference links, setext headings, escapes, entities, nested quotes, and a safe set of inline HTML (`<kbd>`, `<sup>`, `<br>` …). From Obsidian: `%%comments%%`, inline footnotes `^[text]`, block ids and task statuses like `- [/]`. See docs/markdown.md.

- [#35](https://github.com/banjo/nota/pull/35) [`44fd84f`](https://github.com/banjo/nota/commit/44fd84ff9427f722a7bc57f09869a6c3cb073e94) - `==Highlighted text==` gets a marker-pen background, as in Obsidian, in every theme. Themes can set it with the `md-highlight` token.

- [#36](https://github.com/banjo/nota/pull/36) [`6b02ca3`](https://github.com/banjo/nota/commit/6b02ca34388a65429ef291ea6f5085cb14a81b63) - Snappier UI: opening notes, folders and ⌘K re-render far less (sidebar rows update only when their own state changes, row menus and tooltips mount on first use, palette keystrokes no longer redraw the dialog), and Settings and the vault wizard load as their own chunks.

- [#45](https://github.com/banjo/nota/pull/45) [`4f4463f`](https://github.com/banjo/nota/commit/4f4463f6ff31fab99032bff5f75ae080adb76688) - The sync server can keep file contents in an S3-compatible bucket (Cloudflare R2, AWS S3, MinIO) with `NOTA_SERVER_BLOBS=s3://bucket/prefix`, so its disk stays small. `nota serve blobs migrate` moves existing blobs there. See docs/deploy.md.

- [#46](https://github.com/banjo/nota/pull/46) [`93191c8`](https://github.com/banjo/nota/commit/93191c8469c12cd972d7c310e9788ed64266a6ef) - `nota set`, `update_frontmatter` and the properties panel write a date (`2026-12-24`) or a timestamp (`2026-12-24T10:00:00+01:00`) without quotes, as Obsidian and the `created` and `updated` lines do.

## 0.1.2

### Patch Changes

- [#25](https://github.com/banjo/nota/pull/25) [`020bccc`](https://github.com/banjo/nota/commit/020bccc9c451eb84f9714bb2018c92d22ad762a8) - Clicking in a note in Reading mode, or in a preview of an outside file, starts editing where you clicked.

- [#27](https://github.com/banjo/nota/pull/27) [`9dbabe5`](https://github.com/banjo/nota/commit/9dbabe5ab8c77fb3efefce79c929e883a85b9f28) - Code blocks are coloured in their language, in every theme. Themes can set the colours with the `md-syntax-*` tokens.

- [#24](https://github.com/banjo/nota/pull/24) [`4d1dc47`](https://github.com/banjo/nota/commit/4d1dc47d62bfd2b8460c405cea51cbfb0ffa2bec) - Tables render as tables in Live and Reading mode, and show their source while the cursor is in one. Themes can colour them with `md-table-border` and `md-table-header`.

- [#26](https://github.com/banjo/nota/pull/26) [`2b0f492`](https://github.com/banjo/nota/commit/2b0f492a26d1e814ef24c9cfcc45f7490a892d33) - Settings follow you: while signed in, the theme, colour theme, note mode and properties sync with your account, and `~/.config/nota/settings.toml` sets them from your dotfiles. `nota settings` shows where each value comes from.

- [#22](https://github.com/banjo/nota/pull/22) [`f827dd5`](https://github.com/banjo/nota/commit/f827dd50b8d4701fa41737c92360baa85fa8bd3e) - Themes: nine classics (Dracula, Nord, Solarized, Gruvbox, Catppuccin, Tokyo Night, Rosé Pine, One, GitHub), your own themes from `.nota/plugins/`, and `nota plugins` to check them.
