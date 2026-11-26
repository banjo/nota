---
"@nota/desktop": patch
---

Pinned notes. Pin a note from its … menu, the right-click menu (a long press on a phone), ⌘K or ⌘⇧P, and it shows under **Pinned** at the top of the sidebar and first in ⌘K. Drag to reorder. The pin is `pinned: <number>` in the note's frontmatter, so it syncs and survives moves and renames, and pinning doesn't change `updated`. In the CLI, `nota pin`, `nota unpin` and `nota ls --pinned`; agents get a `pinned` filter on `list_notes`.
