# Plan: a fuller Markdown preview (tables, code, callouts …) that follows every theme

Status: **approved by Anton 2026-11-24.** Every recommendation below was accepted.

## Decisions (Anton, 2026-11-24)

| # | Question | Decision |
|---|---|---|
| 1 | Tables in Live mode: show the whole table as source while the cursor is in it, or edit inside the rendered cells? | **Source while the cursor is in it** (as Obsidian). Cell editing is a later, separate feature. |
| 2 | Callouts: GitHub alerts (`> [!NOTE]`), Obsidian callouts (`> [!info] Title`), or both? | **Both.** One parser, Obsidian types mapped onto GitHub's five colours. |
| 3 | Images: part of this work or their own issue? | **Own issue.** They need the backend to serve vault files and rules for outside previews. |
| 4 | Math (KaTeX) and Mermaid: now or later? | **Later**, each lazy-loaded, if anyone asks. |
| 5 | Code colours: new tokens per theme, or derived from existing tokens only? | **A few new `md-syntax-*` tokens** with fallbacks to existing ones. |

## TL;DR

* We keep **one engine**: the CodeMirror live preview in `packages/editor`. Live, Reading and
  Source stay one document and one view. No second HTML renderer and no WYSIWYG editor.
* The feature baseline is **GFM plus GitHub alerts and the Obsidian syntax Nota already
  speaks**. The look stays Nota's own. We don't copy `github-markdown-css`, because its colours
  are fixed and would fight the themes.
* Every new colour is a `--md-*` token that falls back to an existing app token. A theme that
  sets none of them still looks right. Built-in, classic and plugin themes work without changes.

## Why not the alternatives

| | One engine (CodeMirror decorations) | Separate HTML renderer for Reading (remark/rehype, goldmark) | WYSIWYG (Milkdown, Tiptap) |
|---|---|---|---|
| Live and Reading look the same | yes | no, two renderers drift apart | yes |
| Mode switch keeps text, undo and scroll | yes | no | n/a |
| Wikilinks and tags | already there | written again as plugins | written again |
| Outside-file preview safety | no HTML or images rendered (open-markdown plan) | needs sanitising | – |
| Work per feature | higher | low | – |
| Keeps the file as written | yes | yes | no, it re-serialises |

## Today

* `decorations.ts` (`previewSpecs`) turns the Lezer tree into specs: `hide`, `mark`, `line`,
  `widget`. `preview.ts` turns them into decorations in a `ViewPlugin`, for the visible ranges
  only.
* Covered: headings, emphasis, strong, strikethrough, inline code, links, URLs, wikilinks, tags,
  blockquotes, horizontal rules, fenced code (no colours), lists and tasks.
* Missing: **tables** (`GFM` already parses `Table`, nothing decorates it), **code colours**,
  callouts, footnotes, `==highlight==`, images, math, Mermaid.
* `@lezer/markdown` has no footnote or highlight parser, so those need small Nota extensions
  next to `syntax.ts`.

## Steps

Each step is a separate PR with tests in `packages/editor/tests` (pure `previewSpecs` tests as
today) and a check in every built-in theme, light and dark.

### 0. Block widgets

A `ViewPlugin` may not replace line breaks or add block widgets. Add a `StateField` for block
replacements and a new spec type (`{ type: "block"; from; to; widget }`). Keep `previewSpecs`
pure, so the specs can still be tested without a DOM. The field reads the whole syntax tree, but
it only looks at `Table` nodes, so it stays cheap.

### 1. Tables

* Outside the cursor, and always in Reading mode: the table is replaced by a widget that renders
  a real `<table>`.
  * Alignment comes from the delimiter row (`:--`, `:-:`, `--:`).
  * Cells render inline Markdown (bold, code, links, wikilinks, tags) from the Lezer nodes
    already in the tree. Clicks on links behave as elsewhere.
  * Wide tables scroll sideways inside the widget, not the page.
* With the cursor in the table (Live): the source shows, with a monospace font so the pipes line
  up (decision 1).
* A click on a cell in Live mode puts the cursor at that cell in the source.
* Tokens: `md-table-border` (→ `hairline`), `md-table-header` (→ `code`). Zebra rows use
  `color-mix()` on `code`.

### 2. Code block colours

* Pass `codeLanguages` from `@codemirror/language-data` to `markdown()` in `index.ts`. Languages
  load on demand, so the bundle stays small. Check that the web build splits them.
* A `HighlightStyle` built only from tokens, used in all three modes:

  | Token | Fallback |
  |---|---|
  | `md-syntax-keyword` | `ink-strong` |
  | `md-syntax-string` | set in `tokens.css` |
  | `md-syntax-number` | set in `tokens.css` |
  | `md-syntax-comment` | `muted-foreground` |
  | `md-syntax-function` | `ink` |
  | `md-syntax-type` | `md-tag` → `ink-strong` |

  `success` and `warning` are too light for text on `code` (about 2:1), so they can't be used
  directly for strings and numbers. Token themes (classics, plugins) derive all six instead: each
  is moved toward black or white until it reaches 4.5:1 (strings, numbers) or 3:1 (the others)
  on `code`.

* The fence line already shows the language (` ```ts `) faintly, so there is no extra label. A
  copy button is optional, after the rest.

### 3. Callouts

* `> [!TYPE] Optional title` on a blockquote's first line, in both syntaxes (decision 2).
* GitHub's five types: `NOTE`, `TIP`, `IMPORTANT`, `WARNING`, `CAUTION`. Obsidian's types map
  onto them, for example `info`/`todo` → note, `success`/`check` → tip, `question`/`example`
  → important, `failure`/`danger`/`bug` → caution, `warning`/`attention` → warning. Unknown
  types fall back to note.
* The marker line renders an icon and the title (or the type's name). The other lines keep the
  quote line style, with a coloured border and a soft background.
* Tokens: `md-callout-note` (→ `ink`), `md-callout-tip` (→ `md-syntax-string`),
  `md-callout-important` (→ `ink-strong`), `md-callout-warning` (→ `md-syntax-number`),
  `md-callout-caution` (→ `danger`). Tip and warning borrow the code colours because `success` and
  `warning` are too light for a title. Backgrounds are an 8% `color-mix()` of the same colour.
  Token themes derive all five, ≥ 3.4:1 on the page, so they stay ≥ 3:1 on the tint.
* Folding (`[!note]-`) is out of scope.

### 4. Footnotes

* A Lezer extension in `syntax.ts`: `[^id]` (inline) and `[^id]: text` (definition).
* References render as a superscript number, numbered by first use as on GitHub, and only when
  the footnote is defined. A click jumps to the definition and a click on the definition's number
  jumps back; the target line lights up briefly and the cursor stays put. Definitions get a
  small, quiet line style; indented lines below one continue it.
* `internal/markdown` (the index) needs no change.

### 5. `==highlight==`

* A Lezer inline extension, as in Obsidian.
* Token: `md-highlight`, which falls back to `color-mix(in srgb, var(--warning) 30%, transparent)`.

### Later, as their own issues

* **Images** (`![alt](path)`, `![[image.png]]`): vault files only, never remote URLs, and never
  in the preview of an outside file (see `docs/plans/open-markdown.md`). Needs a way to serve
  vault files on desktop and web (decision 3).
* **Math and Mermaid**: lazy-loaded KaTeX and Mermaid (decision 4).
* Editing table cells in place, and folding callouts.

## Themes

* Every new token goes into:
  * `THEME_TOKENS` in `packages/app/src/theme/custom.ts`
  * the list in `internal/plugins/plugins.go`
  * `docs/themes.md`
* No new values in `palettes.css` or `classics.ts` unless a theme needs one. The fallbacks cover
  the rest.
* `packages/app/tests/themes.test.ts`: contrast checks for the new text colours (syntax tokens
  on `code`, callout colours on `background`) in every built-in and classic theme, light and
  dark.
* No fixed colour anywhere in `theme.ts`. Only tokens and `color-mix()` of tokens.
* Update the screenshots in `docs/screenshots.md` with a note that has a table, a code block and
  a callout.

## Out of scope

* A second renderer, raw HTML in notes, a WYSIWYG editor.
* Changes to how notes are saved. The file stays exactly as written.
