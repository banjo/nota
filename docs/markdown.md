# Markdown in Nota

Notes are plain Markdown files. Nota reads [GitHub Flavored Markdown](https://github.github.com/gfm/),
GitHub's alerts and footnotes, and the parts of Obsidian's syntax listed below, so a vault reads the
same in Nota, on GitHub and in Obsidian.

In Live mode the markers (`**`, `[[ ]]`, `> ` …) hide except on the line you're editing. Reading
mode hides them all, and Source shows the file as it is.

## GitHub Flavored Markdown

| Write | Shows |
|---|---|
| `# Heading`, or a line of `===` / `---` under text | A heading |
| `**bold**`, `*italic*`, `~~struck~~`, `` `code` `` | Formatting |
| `[text](https://…)`, `<https://…>`, `www.…` | A web link |
| `[text][label]` with `[label]: https://…` below | A web link (the definition is a quiet line) |
| `[text](Other.md)`, `[text](../Folder/Other.md#Heading)` | A link to a note, and a heading in it |
| `[text](#heading)` | A heading in this note (its text, or GitHub's `#my-heading`) |
| `- item`, `1. item`, `- [ ] task`, `- [x] done` | Lists and tasks |
| `> quote`, `> > nested` | Quotes, with a border per level |
| A table with a `\| --- \|` row | A table (its source while the cursor is in it) |
| ```` ```ts ```` … ```` ``` ```` | Code, coloured in its language |
| `[^1]` with `[^1]: text` below | A numbered footnote; click to jump to it and back |
| `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]` | A callout |
| `\*`, `&copy;`, `&rarr;`, `&#8594;` | The character itself |
| `<kbd>`, `<sup>`, `<sub>`, `<mark>`, `<u>`, `<s>`, `<b>`, `<i>`, `<small>`, `<br>` | Formatting. No other HTML is rendered |

## Obsidian

| Write | Shows |
|---|---|
| `[[Note]]`, `[[Note\|text]]`, `[[Folder/Note]]` | A link to a note |
| `[[Note#Heading]]`, `[[#Heading]]` | A heading in that note, or in this one |
| `text ^id`, `[[Note#^id]]`, `[[#^id]]` | A block id (hidden), and links to that block |
| `#tag`, `#area/sub` | A tag |
| `> [!info] Title` (and `tip`, `warning`, `bug` …) | A callout. Obsidian's types map onto GitHub's five colours |
| `==highlight==` | Highlighted text |
| `%%comment%%`, or `%%` lines around a block | Hidden in Reading mode, faint while writing |
| `^[inline footnote]` | A numbered footnote, its text shown on hover or tap |
| `- [/]`, `[-]`, `[>]`, `[<]`, `[!]`, `[?]`, `[*]` | Tasks in progress, cancelled, forwarded, scheduled, important, a question, starred |

## Not yet

Images and embedded notes (`![](image.png)`, `![[Note]]`) show as links for now (#38). Maths and
Mermaid diagrams show as code.
