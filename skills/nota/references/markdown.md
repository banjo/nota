# Markdown in Nota

Notes are GitHub Flavored Markdown plus the common parts of Obsidian's syntax, so a vault reads
the same in Nota, on GitHub and in Obsidian. Write notes with this syntax; anything else shows as
plain text.

The app has three note modes: **Live** (markers hide except on the line being edited),
**Reading** (all hidden, read-only) and **Source** (the file as it is).

## Syntax

| Write                                                               | Shows                                                  |
| ------------------------------------------------------------------- | ------------------------------------------------------ |
| `**bold**`, `*italic*`, `~~struck~~`, `==highlight==`, `` `code` `` | Formatting                                             |
| `- [ ] task`, `- [x] done`                                          | Tasks; also `[/]` `[-]` `[>]` `[<]` `[!]` `[?]` `[*]`  |
| `> [!NOTE]` … `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]`  | A callout; Obsidian's `> [!info] Title` types work too |
| ` ```ts `                                                           | Highlighted code                                       |
| `[^1]` with `[^1]: text`, or inline `^[text]`                       | Footnotes                                              |
| `[[Note]]`, `[[Folder/Note\|text]]`, `[[Note#Heading]]`             | Links to notes; `[text](Other.md)` works too           |
| `text ^id`, `[[Note#^id]]`                                          | A block id and a link to that block                    |
| `#tag`, `#area/sub`                                                 | Tags                                                   |
| `%%comment%%`                                                       | Hidden in Reading mode and in exports                  |
| `<kbd>`, `<mark>`, `<sup>`, `<sub>`, `<u>`, `<br>` …                | The only HTML rendered                                 |

Maths and Mermaid show as code.

## Images and embeds

| Write                                                | Shows                                                                                    |
| ---------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `![[skiss.png]]`, `![[skiss.png\|300]]`              | The image (300 px wide), found by name: the note's folder, `Attachments/`, then anywhere |
| `![alt](../Attachments/skiss.png)`, `![alt\|300](…)` | The image, by path relative to the note (`%20` for spaces)                               |
| `![[Note]]`, `![[Note#Heading]]`, `![[Note#^id]]`    | The note, section or block, read-only                                                    |

- Shown types: png, jpg, gif, webp, avif, svg, bmp.
- The app saves pasted and dropped images in `Attachments/` at the vault root (or an existing
  `attachments/`), as `Pasted image <timestamp>.png`. Follow the same convention when adding an
  image yourself: copy the file into `<vault>/Attachments/`, then embed it with `![[name.png]]`.
- Images sync like notes. Images from the web (`![](https://…)`) are never loaded; they show as
  links. Previews of files outside the vault (`nota open --app <file>`) show no images.
- MCP has no tool to add a file: with only MCP (for example over HTTP), embed images that are
  already in the vault, or ask the user to drop the image into the note.
