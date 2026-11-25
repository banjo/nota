---
"@nota/desktop": patch
---

A trash you can see into. The sidebar's trash button (and ⌘K "Open trash") lists deleted notes: read one, restore it, or delete it for good, and empty the trash. Deleting a note now offers **Undo**. In the CLI, `nota trash ls`, `nota trash restore` (`--to` for another folder), `nota trash rm` and `nota trash empty --older 30d`; agents get `list_trash` and `restore_note`. The web app lists the notes deleted on the server and restores them.
