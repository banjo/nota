---
"@nota/desktop": patch
---

Renaming or moving a note, or renaming a folder, rewrites the links to it so they keep working: `[[Plan]]` becomes `[[Roadmap]]`, `[[Projects/Plan]]` and `[plan](Projects/Plan.md)` follow the note, and a moved note's own relative links and images are fixed from its new folder. Headings, block ids and aliases stay as written. The app says how many links changed; `nota mv` (which now also renames: `nota mv Plan Roadmap.md`) and `nota folder rename` take `--no-update-links`. See docs/cli.md.
