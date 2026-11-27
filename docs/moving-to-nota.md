# Moving to Nota

Nota keeps notes as Markdown files in a folder, so most apps' notes come along as they are.
Import puts them in a new folder of your vault: nothing in it is replaced, a name that is taken
gets " 2", links between the imported notes keep working, and each note keeps its dates as
`created` and `updated`. When it is done, it lists what was renamed and what was left out.

In the app: **Settings › Import** or "Import notes…" in ⌘K (File ▸ Import Notes… on the
desktop). In a terminal: `nota import` ([cli.md](cli.md#import) has every detail). Add
`--dry-run` to see what would happen first.

## Obsidian

An Obsidian vault already is what Nota reads: `[[links]]`, `![[embeds]]`, callouts, tags and
frontmatter all work. You can open the vault's folder as your Nota vault (Settings › Vault ›
Switch vault…) and keep using both apps on it; Nota ignores `.obsidian/`.

To bring it into the vault you have instead:

```sh
nota import obsidian ~/Documents/Obsidian
```

or "Folder of Markdown notes" in the app. `.obsidian/` and `.trash/` stay behind. Obsidian titles a
note by its file name and Nota by its first line, so a note that doesn't start with its name gets
it as a heading on top. Obsidian's plugins (Dataview queries, Templater, canvases) are files Nota
doesn't run; they come along as they are.

## Apple Notes

On a Mac, in the app: "Apple Notes" in the import dialog. In a terminal:

```sh
nota import apple-notes
```

macOS asks once whether Nota (or your terminal) may control Notes. Every note comes in with its
folder, its title as a heading, formatting, checklists, tables and images, and the dates it was
created and last changed. Locked notes and "Recently Deleted" are left out. Notes hands out
images but no other attachments (PDFs, scans, drawings): the report lists the notes that had
some, so you can save them from Notes.

## Bear

In Bear: **File › Export Notes…**, choose **Markdown**, and turn on **Export attachments**. Then
import the folder:

```sh
nota import markdown ~/Downloads/Bear --folder Bear
```

Bear's `#tags` are tags in Nota too, and its `==highlights==` work.

## Notion

In Notion: **Settings › Export all workspace content** (or a page's **⋯ › Export**), with
**Markdown & CSV** and **Include subpages**. Import the zip as it is:

```sh
nota import markdown ~/Downloads/Export-1234.zip --folder Notion
```

The page id Notion puts at the end of every name (`Plan 1a2b…ef.md`) is left out, and the links
follow. Databases come as CSV files next to their pages; Nota keeps them but doesn't show them.

## Logseq

Import the graph's folder:

```sh
nota import markdown ~/Documents/Logseq --folder Logseq
```

Pages and journals become notes in `pages/` and `journals/`; Logseq's own `logseq/` folder
(settings and backups) stays behind. Its `key:: value` properties stay in the text, and outlines
stay lists.

## Anything else

Any folder or zip of Markdown files imports with `markdown`. Most apps can export that; if one
exports HTML only, convert it first (for example with Pandoc). Evernote, Google Keep and Notion's
HTML aren't read yet.
