# Export and import

## Export

`nota export [note|folder]` exports a note, a folder or, with no argument, the whole vault. The
file goes to the current folder under the note's or folder's name; `-o` names a file, a folder
to write into, or `-` for stdout. An existing file is replaced only with `--force`. `--json`
prints `{path, notes, bytes}`.

| `--format`       | A note                                                  | A folder or the vault                                 |
| ---------------- | ------------------------------------------------------- | ----------------------------------------------------- |
| `html` (default) | one page with its images inside, in Reading mode's look | a zip of linked pages, images in `attachments/`       |
| `md`             | plain Markdown; a zip when it shows images              | a zip of notes and their images                       |
| `zip`            | the note and its images as they are, at vault paths     | every file under it, plus images shown from elsewhere |

- Plain Markdown (`md`) is for apps that don't read Obsidian syntax: `[[links]]` and `![[embeds]]`
  become Markdown links and images, `%%comments%%` and `^block-ids` go, inline footnotes become
  numbered ones. Frontmatter and code stay.
- In `html` and `md`, a link to a note inside the export leads to its file; a link to any other
  note becomes its text.
- PDF has no CLI: the app prints (⌘P) or saves a PDF from a note's "…" menu ("Export note…").
  The web app opens the browser's print dialog.

## Import

`nota import <source> [path]`:

| Source        | Path                              | Reads                                                            |
| ------------- | --------------------------------- | ---------------------------------------------------------------- |
| `markdown`    | a folder, a zip or one `.md` file | Markdown notes and the files next to them (Bear, Notion, Logseq) |
| `obsidian`    | the vault's folder                | the same as `markdown`                                           |
| `apple-notes` | none                              | every note in Apple Notes with folders and images (macOS only)   |

Run it with `--dry-run --json` first and tell the user what will happen, then without
`--dry-run`. Everything lands in one folder: a new one named after the source ("Bear", "Apple
Notes", with " 2" when taken), or `--folder <path>` (`/` for the vault root).

What it does to the files:

- **Nothing is replaced.** A taken name gets " 2", " 3" ….
- Names with characters Nota keeps out of file names (`: * ? " < > | # ^ [ ] \`) are cleaned,
  `.markdown` becomes `.md`, and Notion's page ids (`Plan 1a2b…ef.md`) are dropped.
- Hidden files and folders (`.obsidian`, `.trash`, `.DS_Store`), `__MACOSX` and Logseq's
  `logseq/` folder stay behind. A zip whose files sit in one top folder is read from inside it.
- Links between imported notes that a rename would break are rewritten; a bare link keeps its
  text (`[[Plan: Q1]]` becomes `[[Plan Q1|Plan: Q1]]`).
- Dates become `created` and `updated` (RFC 3339) from other apps' keys (`date created`,
  `created_at`, Notion's `Created`/`Last edited time`, `modified` …) or from the file times.
- Apple Notes: each note becomes Markdown in its folder, titled as in Notes, with its images in
  `Attachments/` of the import. Locked notes and "Recently Deleted" are left out; Notes hands out
  images only, so the report flags notes with other attachments. If macOS denies access, the
  error says to allow it in System Settings › Privacy & Security › Automation.

The JSON report: `{folder, notes, files, renamed, skipped, links, dryRun, items}`; each item has
`source`, `path`, `status` (`imported`, `renamed`, `skipped`), `reason` and `detail`. Tell the
user about renamed and skipped items and any `detail`.

Per-app export steps (Bear, Notion, Logseq) for the user are in
[Moving to Nota](https://github.com/banjo/nota/blob/main/docs/moving-to-nota.md). An Obsidian vault can also be opened as the Nota vault as it is.

The app has both: "Export…" in note and folder menus, "Import notes…" in Settings and ⌘K.
