# Onboarding prototype

Three clickable variants of the desktop first-run flow from issue #5, behind the prototype picker.
Static, self-contained: open `index.html` in a browser (`?v=1|2|3` picks a variant).

| # | Variant | Axis |
| - | ------- | ---- |
| 1 | Wizard | One decision per screen, linear (welcome → vault → inbox → sync → done) |
| 2 | One form | Everything on one page, explicit New / Existing switch |
| 3 | Folder first | Pick or drop a folder; the app infers create / open / use-empty |

Keys: `1–3`, `←`/`→` switch variants, `R` restarts the flow. Inside a flow: `Enter` continues, `Esc` goes back
(or closes the folder sheet). The sun/moon button top right cycles System / Light / Dark.

The file system, folder picker, note counts and sign-in are fake. Names to try: `Writing` (exists, 124 notes),
`Archive` (empty), `Recipes` (files, no notes), `a/b`, `.hidden`, `CON`, `/System/...`. In variant 3, dropping a real
folder counts its `.md` files in the browser (the browser hides the real path, so it is filed under `~/Documents`).

Screenshots: `screenshots/<variant>-<step>-<light|dark>.png`, 1000×680.
