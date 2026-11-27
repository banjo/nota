# @nota/desktop

## 0.1.7

### Patch Changes

- [#75](https://github.com/banjo/nota/pull/75) [`aca15c2`](https://github.com/banjo/nota/commit/aca15c2fe2f773bc399400e4ed1f89153d34348d) - Folders and saved views can have an icon and a colour. Right-click a folder (or a view) and choose "Change icon…" to search 12,000 icons from Lucide, Tabler and Simple Icons, or an emoji, and pick a colour that follows the theme. The icons sync with the vault (`.nota/folders.yaml`) and follow a renamed folder. `nota folder icon Work lucide:briefcase --color blue` and `nota icons <words>` do the same from the terminal, and `set_folder_icon` and `search_icons` over MCP.

- [#73](https://github.com/banjo/nota/pull/73) [`5102ec4`](https://github.com/banjo/nota/commit/5102ec43c53829620fb453e6b9e4ddec1750533c) - Imported notes keep their titles. Obsidian and other apps that keep notes as files title a note by its file name, so a note that starts with something else (a greeting, a section heading) now gets its name as a heading on top. The title field also skips blank lines at the top of a note, as the sidebar does, instead of showing "Untitled".

## 0.1.6

### Patch Changes

- [#69](https://github.com/banjo/nota/pull/69) [`a28f503`](https://github.com/banjo/nota/commit/a28f50385d09147c7ac5f41702b4782ee92fb834) - The attachments folder has its own row in the sidebar, opening a page of its images with the notes that use each one, so unused ones are easy to spot.

- [#71](https://github.com/banjo/nota/pull/71) [`bd8ea78`](https://github.com/banjo/nota/commit/bd8ea78437ac832ada479e289d179d8ab6b9f00b) - Shared notes get a link preview: chats and social sites show the note's title, the start of the note and a card drawn in the note's theme. A protected note's preview only says that it is protected.

- [#72](https://github.com/banjo/nota/pull/72) [`47b596f`](https://github.com/banjo/nota/commit/47b596f2dfa181e666942d16d42aaabd51b840a0) - On a shared note's page, an image alone on its line may be wider than the text (up to 960 px), so screenshots are readable. Images narrower than the text, and images with a width set, stay as they are.

## 0.1.5

### Patch Changes

- [#65](https://github.com/banjo/nota/pull/65) [`91b7ad0`](https://github.com/banjo/nota/commit/91b7ad09c68ea951f192e43191a806ec4fb42c9d) - Export a note, a folder or the whole vault: a web page with the images inside, a PDF (⌘P prints), Markdown that other apps read, or the files as they are in a zip. In the "…" menus, ⌘K, File ▸ Export and `nota export`.

- [#67](https://github.com/banjo/nota/pull/67) [`2da2608`](https://github.com/banjo/nota/commit/2da2608f609f7bfba960a2e451b2a42e82f2ef7b) - Import notes from Obsidian, Apple Notes (on a Mac), Bear, Notion, Logseq or any folder or zip of Markdown files. Everything goes into a new folder, nothing is replaced, links keep working and dates come along. In Settings, ⌘K, File ▸ Import Notes… and `nota import`.

## 0.1.4

### Patch Changes

- [#61](https://github.com/banjo/nota/pull/61) [`7eff385`](https://github.com/banjo/nota/commit/7eff3851889f0e500f0feb7cf5f7dc5529837c1f) - Find and replace in a note. ⌘F opens a find bar over the note with match case, whole word and regex toggles, a match count, and Enter / ⇧Enter to step through the matches; ⌥⌘F adds a replace row with Replace and All. It works in Live, Source and Reading (find only), in the desktop app and on the web, and on a phone from the note's "…" sheet.

- [#62](https://github.com/banjo/nota/pull/62) [`6b5a934`](https://github.com/banjo/nota/commit/6b5a934babfc29602a22a3bc9b11d2ee0130b931) - Find and replace across notes. ⌘⇧F (or "Find in notes…" in ⌘K) shows every match note by note with what it becomes; untick the ones to keep, replace, and undo from the toast. A match opens its note with the find bar on it. The CLI has `nota replace` (it lists the changes and asks first) and agents get the MCP tool `replace_in_notes` (a dry run unless told otherwise). Only note text changes: frontmatter and link targets are left alone, and code unless asked.

- [#57](https://github.com/banjo/nota/pull/57) [`a0f5d9b`](https://github.com/banjo/nota/commit/a0f5d9bc68ffb7e7694a6633f5cf9ab7e2a2fd1c) - A note's history in the app: **History** in the note's "…" menu, ⌘K "Show history" or ⌘⇧H lists its versions on the sync server. Pick one to read it or see what changed since, then restore it (as a new version, so nothing is lost) or copy its text. Works on a phone too.

- [#60](https://github.com/banjo/nota/pull/60) [`041f47a`](https://github.com/banjo/nota/commit/041f47a4be99fe6bd8b05ca582b26c31ff43dfea) - Pinned notes. Pin a note from its … menu, the right-click menu (a long press on a phone), ⌘K or ⌘⇧P, and it shows under **Pinned** at the top of the sidebar and first in ⌘K. Drag to reorder. The pin is `pinned: <number>` in the note's frontmatter, so it syncs and survives moves and renames, and pinning doesn't change `updated`. In the CLI, `nota pin`, `nota unpin` and `nota ls --pinned`; agents get a `pinned` filter on `list_notes`.

- [#57](https://github.com/banjo/nota/pull/57) [`e7b04c3`](https://github.com/banjo/nota/commit/e7b04c3f0d8dd0a8ec8d6ddb0f5a0eeb148fdb47) - The server can collect unreferenced blobs on its own: set `NOTA_SERVER_BLOB_GC_INTERVAL=24h` (or `7d`) and it runs the same pass as `nota serve blobs gc` on that timer. Off by default.

- [#63](https://github.com/banjo/nota/pull/63) [`80e1a84`](https://github.com/banjo/nota/commit/80e1a845dcf77849d4bbb19128533a1c91fd0af0) - Share a note as a web page: "Share…" in a note's menu makes a link to it on your sync server, live, with its images, in your theme and optionally behind a password.

- [#57](https://github.com/banjo/nota/pull/57) [`755d720`](https://github.com/banjo/nota/commit/755d720698a8b387c260aed168d7d226020e3f7a) - A trash you can see into. The sidebar's trash button (and ⌘K "Open trash") lists deleted notes: read one, restore it, or delete it for good, and empty the trash. Deleting a note now offers **Undo**. In the CLI, `nota trash ls`, `nota trash restore` (`--to` for another folder), `nota trash rm` and `nota trash empty --older 30d`; agents get `list_trash` and `restore_note`. The web app lists the notes deleted on the server and restores them.

- [#57](https://github.com/banjo/nota/pull/57) [`161a02d`](https://github.com/banjo/nota/commit/161a02d8fa52596ba4c2dfab3a60810c21b67764) - Renaming or moving a note, or renaming a folder, rewrites the links to it so they keep working: `[[Plan]]` becomes `[[Roadmap]]`, `[[Projects/Plan]]` and `[plan](Projects/Plan.md)` follow the note, and a moved note's own relative links and images are fixed from its new folder. Headings, block ids and aliases stay as written. The app says how many links changed; `nota mv` (which now also renames: `nota mv Plan Roadmap.md`) and `nota folder rename` take `--no-update-links`. See docs/cli.md.

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
