# Plan: one settings file, synced with the account

Status: **approved by Anton 2026-11-24.** The decisions below override anything older in this document.

## Decisions (Anton, 2026-11-24)

| # | Question | Decision |
|---|---|---|
| 1 | Should `inbox` become a setting? | No, not now. It stays in `config.toml`. Moving it to the vault (`.nota/settings.toml`) is a separate follow-up. |
| 2 | Per-device override on top of the file? | No. The file is the top layer. |
| 3 | A "Copy as TOML" button in Settings? | Yes. It copies the resolved settings as `settings.toml` content. |
| 4 | Should the web show keys locked by the file? | Not in this round. The web shows only the account layer. |

## TL;DR

* User settings (theme, palette, note mode, properties) move out of `localStorage` into three
  layers. The highest one that sets a key wins:

  | Layer | Where | Written by |
  |---|---|---|
  | 4. File | `~/.config/nota/settings.toml` | the user, by hand or with dotfiles; **never the app** |
  | 3. Account | the server, per user | the app (Settings, ⌘K) when signed in |
  | 2. Device | `localStorage` | the app when not signed in, and as a cache |
  | 1. Defaults | code | – |

* A key set in the file is **locked**. Settings shows it read-only with the note "Set in
  settings.toml", so the file stays the declarative source of truth and the app never rewrites
  a dotfile. The account syncs everything else. Two sync channels never write the same key, so
  there is nothing to merge.
* The format is TOML. It matches `config.toml` and `hooks.toml` and is easy to write by hand (§2).
* `config.toml` stays per machine (vault path, `onboarded`) and is not synced.

---

## 1. What counts as a setting

| Key | Today | Layer |
|---|---|---|
| `theme` (`system`/`light`/`dark`) | `localStorage nota:theme` | setting |
| `palette` | `localStorage nota:palette` | setting |
| `note_mode` | `localStorage nota:note-mode` | setting |
| `properties` | `localStorage nota:properties` | setting |
| `vault`, `onboarded` | `config.toml` | machine, stays |
| `inbox` | `config.toml` | vault-specific, stays (later maybe `.nota/`) |
| route, sidebar, expanded folders (`nota:ui`), recent folders, drafts, reading hint, the web's chosen vault | `localStorage` | device state, stays |

Rule: a setting is a **choice the user would want on every machine**. Anything about a specific
window, vault path or ongoing session is device state.

## 2. Format: TOML

| | TOML | YAML | JSON |
|---|---|---|---|
| Comments | yes | yes | no (JSONC isn't standard) |
| Writing by hand | good | indentation and type traps (`no` → false, `1.10` → 1.1) | quotes, no trailing commas |
| Consistency in Nota | `config.toml`, `hooks.toml`, `sync.toml` | `.nota/views/*.yaml` | – |
| Go | `BurntSushi/toml` already a dependency | new dependency | stdlib |
| Nesting | fine for 1–2 levels (`[editor]`) | best | good |

TOML fits a flat, hand-written dotfile best and adds no dependency. JSON lacks comments, which
dotfiles need. YAML stays for views, where the structure is deeper.

The schema is published as JSON Schema (`settings.schema.json`) anyway. Taplo/Even Better TOML
uses it for completion in the editor via `#:schema` on the first line.

```toml
#:schema https://nota.example/settings.schema.json
theme = "dark"
palette = "paper"
note_mode = "live"
properties = "shown"
```

## 3. The schema in one place

* Go: `internal/settings` with `type Settings struct` (pointer fields: `nil` = not set in this
  layer), `Defaults()`, `Validate()` and `Merge(layers ...Settings) Resolved`. `Resolved` holds the
  value **and the source** of each key (`"default" | "device" | "account" | "file"`).
* TS: the types are generated or hand-mirrored in `packages/client/src/types.ts`, with a test
  that compares the key lists so they can't drift.
* **Unknown keys produce a warning, not an error** (unlike `hooks.toml`), so an older client
  doesn't break on a file written for a newer version. Invalid values are ignored with a warning,
  and that key falls back to the layer below.

## 4. The file

* Path: `$XDG_CONFIG_HOME/nota/settings.toml`, otherwise `~/.config/nota/settings.toml`. It lives
  next to `ConfigPath()` in `internal/vault/config.go`, with a shared helper for the directory.
* The app only reads it. The desktop app watches the file (it may be a symlink into a dotfiles
  repo, so it watches the target and the directory) and pushes `settings.changed` to the frontend.
* CLI: `nota settings` lists the resolved values with their sources. `nota settings path` prints
  the path. `nota settings edit` opens it in `$EDITOR`.
* Docs: put `settings.toml` in dotfiles, **not** the whole `~/.config/nota/`, because
  `credentials` and `config.toml` live there.

## 5. The account

* Migration `00004_user_settings.sql`:
  `CREATE TABLE user_settings (user_id … PRIMARY KEY REFERENCES users, data TEXT NOT NULL, rev INTEGER NOT NULL, updated_at …)`.
  `data` is a JSON object holding only the keys set at this layer.
* API (`internal/server`, session or device token):
  * `GET /v1/account/settings` → `{rev, settings}`
  * `PATCH /v1/account/settings` with `{base_rev, set: {...}, unset: [...]}` → `{rev, settings}`.
    Changes are per key, so two devices that change different keys don't collide. If `base_rev`
    is stale, the server still applies them: last writer wins per key, which is enough for
    preferences.
* Live updates: a `settings.changed` event on the existing SSE stream, so other devices update
  without polling.
* The desktop app reaches the server through the same token as sync. When not signed in, layer 3
  is skipped and changes go to layer 2.

## 6. Frontend

* A store `packages/app/src/settings/` replaces the reads and writes in `theme/store.ts`,
  `lib/noteMode.ts` and `lib/propertiesPref.ts`. Each component keeps its API, so screens barely
  change.
* Writing: if the key comes from the file, the control is disabled with "Set in settings.toml".
  Otherwise the store writes to the account (or to the device when not signed in).
* **The resolved values are cached in `localStorage`** under the current keys (`nota:theme`, …).
  The inline pre-paint script in `index.html` keeps reading them, so the first paint shows no
  wrong theme while the file or server loads. The landing page's `nota:theme` keeps working.
* ⌘K commands (`view.mode.*`, theme) go through the same store, so they are locked by the file too.

## 7. Migration

* On first start with an account: if the account has no settings, the device's current
  `localStorage` values go up once. Otherwise the account wins over the device.
* `settings.toml` is never created automatically, only by `nota settings edit` or by the user.

## 8. Steps (one commit each)

1. `internal/settings`: types, validation, merging, tests.
2. The file: path, reading, watching, `nota settings` in the CLI.
3. Server: migration, `GET/PATCH /v1/account/settings`, SSE event, tests.
4. `packages/client`: types and API calls.
5. Desktop: Go binding (`Settings()`, `SetSetting()`) and `settings.changed`.
6. Frontend: the settings store, migrating theme, note mode and properties, locked state and
   "Copy as TOML" in Settings.
7. Web: layer 3 only (no file), with the same cache.
8. Docs: `docs/settings.md`, references from `cli.md` and the README.

## 9. Follow-ups (out of scope)

* Move `inbox` to the vault (`.nota/settings.toml`) as its own layer (vault > account).
* Let the web show keys locked by the file, e.g. by uploading the file's keys to the account
  marked "locked by file".
