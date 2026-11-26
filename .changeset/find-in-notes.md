---
"@nota/desktop": patch
---

Find and replace across notes. ⌘⇧F (or "Find in notes…" in ⌘K) shows every match note by note with what it becomes; untick the ones to keep, replace, and undo from the toast. A match opens its note with the find bar on it. The CLI has `nota replace` (it lists the changes and asks first) and agents get the MCP tool `replace_in_notes` (a dry run unless told otherwise). Only note text changes: frontmatter and link targets are left alone, and code unless asked.
