# Plan: open Markdown files from Finder in Nota (preview + save to vault)

Status: **approved by Anton 2026-11-24, being implemented** on `feat/open-markdown` (base `305c9b2`),
one commit per step of §8. The decisions below override anything older in this document.
Sources I read: this repo at `305c9b2`, and MyGo **v0.10.0** (`egoist/mygo` tag `v0.10.0`,
commit `ebde939`, the version in our `go.mod`). MyGo was cloned read-only to
`/root/.hermes/cache/scratch/mygo-src` because it isn't in any local module cache. Nothing was built or run.
Wherever a statement depends on AppKit or Launch Services behaviour that I could only infer from
source and docs, it is marked **[verify on Mac]**.


## Decisions (Anton, 2026-11-24)

| # | Question (§10) | Decision |
|---|---|---|
| — | Banner | **Variant A**: a strip under the top bar (§7). |
| 1 | CLI | `nota open --app <file\|note>` (short `-a`); plain `nota open` keeps opening `$EDITOR`. |
| 2 | Default handler | Open With only, `LSHandlerRank: Alternate`, never set the default. No Settings row for now. |
| 3 | Editing | **New requirement, replaces "Edit in Nota = copy to inbox".** An outside file starts as a read-only **preview** (Reading mode + banner). The banner action **Edit** (also ⌘K `file.edit`) turns it into an editable **in-memory draft** in the normal editor (Live mode). Nothing is written, neither to the original nor to the vault, until the user picks **Save to vault…**, which saves the edited content. While editing, the banner reads "Draft · unsaved changes" and offers **Revert** (back to the file's text, keep editing) and **Discard** (drop the draft, back to the preview). The original file is never written, except by the explicit, clearly labelled ⌘K command **Overwrite original file…** (confirmation, hash-checked). |
| 3b | Vault notes | A vault note opened from Finder opens as the normal note page in **Reading** mode (per-open override; the saved mode setting is untouched) and is editable through the normal mode switch (⌘E, the switch, `view.mode.*`). |
| 4 | Size limit | 5 MiB. |
| 5 | Onboarding | Queue the files; they open after the setup's relaunch. |
| 6 | Finder role | `Viewer`. |
| 7 | Move into vault | Included: copy into the vault, then the original goes to the **Trash**, only after an explicit confirmation. |
| 8 | File ▸ Open… | Yes, menu item + ⌘K `file.open`, **no** new global shortcut (⌘O stays unbound). |
| 9 | Relative `.md` links | Followed as new previews (same checks as an OS open). |
| 10 | Imported bytes | Kept exactly (no `created` stamp); an edited draft keeps the frontmatter byte for byte and replaces the body. |
| 11 | Prototype | None; go with A. |

Every action lives in the ⌘K registry (`file.*`), per Anton's standing rule.

---

## TL;DR

* **MyGo already does the hard part. No patch, no cgo, no upstream PR needed.** `fileAssociations`
  in `mygo.config.ts` writes `CFBundleDocumentTypes`. The darwin backend implements
  `application:openURLs:` with purego, and every route in (Finder, Open With, Dock drop, `open -a`,
  argv, a forwarded second instance) ends up in `mygo.App.OnOpenFile(func(path string))`.
* What **we** have to add: a Go `Opener` service. It queues opened paths, because a cold start
  fires them before any window or page exists. It classifies each path as an in-vault note or an
  outside file (EvalSymlinks + NFC + `os.SameFile`), reads outside files under limits, and saves or
  moves them into the vault when Anton asks. On the frontend: a route for outside files, a preview
  page with a banner, ⌘K commands, and revealing the note in the tree.
* One MyGo default needs overriding: it hard-codes `LSHandlerRank: "Default"`, which can make Nota the
  default `.md` app. We replace `CFBundleDocumentTypes` through `macos.infoPlist` with
  `LSHandlerRank: "Alternate"`, so Nota shows up in **Open With** without taking over
  double-click.
* `nota open` already exists and opens a vault note in `$EDITOR`. **Proposal: `nota open --app <file|note>`**
  (macOS: `open -b com.banjoanton.nota <abs>`). This is an open question for Anton.
* About 8 commits, roughly 4–5 subagent-days, plus a 30–45 min manual checklist on Anton's Mac.

---

## 1. MyGo: what exists, what we add

### 1.1 What MyGo v0.10.0 already has (quoted)

| Piece | Where (MyGo v0.10.0) | What it does |
|---|---|---|
| Config | `packages/cli/index.d.ts:128-129`, `:162-172` | `fileAssociations?: FileAssociation[]` with `{ ext: string[]; name?; mimeType?; role?: "Editor" \| "Viewer" }`; doc comment: "The types of files the app opens, which reach `App.OnOpenFile`." |
| Info.plist | `cmd/mygo/bundle.go:280-302` | For each association: `doc := map[string]any{"CFBundleTypeName": name, "CFBundleTypeRole": role, "LSHandlerRank": "Default", "CFBundleTypeExtensions": exts}` (`:294`) plus `CFBundleTypeMIMETypes`, set as `d["CFBundleDocumentTypes"]`. **No `LSItemContentTypes`, no `UTImportedTypeDeclarations`, rank fixed to `Default`.** |
| Override | `cmd/mygo/bundle.go:315-316` | `for k, v := range c.MacOS.InfoPlist { d[k] = v // the app's own keys win }`: we can replace the whole `CFBundleDocumentTypes`. |
| Go API | `app.go:665-673` | `func (a *Application) OnOpenFile(fn func(path string)) (off func())`: "one of the types of fileAssociations … opened from the file manager, a file dropped on the Dock icon (macOS), or one passed on the command line. Register it before Run to receive the files the app was launched with." |
| Apple Events | `internal/darwin/app.go:361-375` | `method("application:openURLs:", …)` splits `isFileURL` URLs into `files` → `theBackend.h.OpenFiles(files)`. Non-file URLs go to `OpenURLs`, which is how `nota://auth` already arrives on macOS. purego, no cgo (`internal/darwin/app.go:9-10`). |
| Fan-out | `app.go:804-808` | `func (appHandler) OpenFiles(paths []string) { for _, p := range paths { fire1(&App.onOpenFile, p) } }` |
| argv | `deeplink.go:96-133`, `:187-190`; `app.go:383` | `fileArgs` keeps args whose extension is in `packageFileExtensions` (linked by `cmd/mygo/build.go:572`), makes relative paths absolute against the working dir, accepts `file://` URLs and drops non-regular files. `launchArgs()` delivers them via `postMain(launchArgs)` **after** the `WhenReady` callbacks (`app.go:381-383`). |
| Second instance | `single_instance.go:205-221` | Forwarded command line → `deliverArgs(msg.Args, msg.WorkingDir)` → `OnOpenFile` in the first instance (Linux/Windows; on macOS also when someone execs the binary directly). |
| Relaunch | `login.go:102` | `func (a *Application) Relaunch(args ...string)`: on macOS through Launch Services, "which passes the arguments on (open --args)". **We can carry pending files across the onboarding/vault-switch relaunch.** |
| Linux | `cmd/mygo/build.go:726-746` | The desktop entry gets `MimeType=<mime>;` and `Exec=… %U`. |
| Documents layer | `document_flow.go:55` | MyGo's own `App.OnOpenFile(openDocumentFile)` hooks in **only** when a Documents kind is registered. Nota registers none, so it won't steal our events. |
| Shell | `modules.go:41` `OpenPath`, `:47` `ShowItemInFolder`, `:211` `DefaultApp` | "Reveal in Finder" and opening a path. |

### 1.2 Limitations to design around

1. **No buffering.** `fire1` (`events.go:146-150`) calls whichever listeners exist right then; an
   event with no listener is lost. So `OnOpenFile` must be registered **before `mygo.App.Run()`**
   (we do it in `main()` next to `OnOpenURL`, `apps/desktop/main.go:196`).
2. **Cold start comes before the window and the page.** AppKit sends `application:openURLs:`
   between `applicationWillFinishLaunching` and `applicationDidFinishLaunching` (which calls
   `b.h.Ready()`, `internal/darwin/app.go:308-315`), so at that point no window exists and React
   hasn't subscribed **[verify on Mac: exact order]**. On Linux, argv files arrive after
   `WhenReady`, but the page still hasn't loaded. **Design: Go keeps a queue; the page pulls it on
   mount and on a nudge event.** A pushed event on its own isn't enough.
3. **The Apple Event path doesn't filter by extension.** `open -a Nota foo.txt` reaches
   `OnOpenFile` too. We check `.md`/`.markdown` ourselves and show a toast for anything else.
4. **Rank is hard-coded to `Default`** (`bundle.go:294`). Where no other app claims `.md` above
   Alternate, Launch Services may make Nota the double-click handler **[verify on Mac]**. Override it
   (see 2.1).
5. **No `LSItemContentTypes` or UTI declarations.** Extension matching
   (`CFBundleTypeExtensions`) still works on current macOS, but Apple prefers UTIs. Our override
   adds `LSItemContentTypes: ["net.daringfireball.markdown"]` plus a `UTImportedTypeDeclarations`
   entry, so it also works on macOS versions that don't ship that UTI **[verify on Mac with
   `mdls -name kMDItemContentType x.md`]**.
6. **No public test fake.** `internal/fake` can't be imported from our module. Testability has to
   come from our own pure `Opener` type, plus the real Linux argv / second-instance path under Xvfb
   (see §6).
7. **Nothing in MyGo sets the default role handler** (`LSSetDefaultRoleHandlerForContentType`). That
   suits us: we don't want to set it.
8. `mygo dev` builds aren't installed bundles, so Finder/Open With can't be tested in dev on macOS.
   Use `mygo build` + copy to `/Applications`, or argv (`…/Nota.app/Contents/MacOS/Nota ~/x.md`).

### 1.3 What we change around MyGo (no patch)

* `apps/desktop/mygo.config.ts`: add `fileAssociations` (needed for the linked argv extensions
  and Linux `MimeType`), **and** the `macos.infoPlist` override of `CFBundleDocumentTypes` /
  `UTImportedTypeDeclarations`.
* `apps/desktop/main.go`: `mygo.App.OnOpenFile(op.Opened)` before `Run`; `mygo.Bind(s, acc, d, op)`.
* **Optional upstream PR to egoist/mygo (not blocking):** `FileAssociation.rank?: "Owner" | "Default" | "Alternate" | "None"`
  and `contentTypes?: string[]` (UTIs) in `bundle.go:280-302`. Then our override can go. Worth
  filing, but don't wait for it.

---

## 2. Packaging, release, Launch Services

### 2.1 `mygo.config.ts` (proposed)

```ts
fileAssociations: [
  { ext: ["md", "markdown"], name: "Markdown Document", mimeType: "text/markdown", role: "Viewer" },
],
macos: {
  signingIdentity: "-",
  infoPlist: {
    LSApplicationCategoryType: "public.app-category.productivity",
    // Replaces MyGo's entry (cmd/mygo/bundle.go:294 hard-codes LSHandlerRank "Default"):
    // Nota is offered in Open With, never silently made the default.
    CFBundleDocumentTypes: [{
      CFBundleTypeName: "Markdown Document",
      CFBundleTypeRole: "Viewer",          // we preview; editing happens after "Save to vault"
      LSHandlerRank: "Alternate",
      LSItemContentTypes: ["net.daringfireball.markdown"],
      CFBundleTypeExtensions: ["md", "markdown"],
      CFBundleTypeMIMETypes: ["text/markdown"],
    }],
    UTImportedTypeDeclarations: [{
      UTTypeIdentifier: "net.daringfireball.markdown",
      UTTypeDescription: "Markdown Document",
      UTTypeConformsTo: ["public.plain-text"],
      UTTypeTagSpecification: { "public.filename-extension": ["md", "markdown"], "public.mime-type": ["text/markdown"] },
    }],
  },
},
```

`Viewer` versus `Editor` only affects how Finder labels and ranks the app; Launch Services opens
the file either way. We use `Viewer` because the product is "preview first" (open question Q6).

**Linux caveat:** the `.deb`/`.rpm` desktop entry will list `MimeType=text/markdown;`. On a desktop
with no other `text/markdown` handler, Nota could become the effective default. That's acceptable on
Linux, but noted. Anton asked for macOS; Linux comes along for free.

### 2.2 `release.yml` (`.github/workflows/release.yml`, macOS job "Verify and package")

Add assertions after `plutil -p "$plist"`:

```sh
plutil -extract CFBundleDocumentTypes.0.LSHandlerRank raw "$plist" | grep -qx Alternate
plutil -extract CFBundleDocumentTypes.0.CFBundleTypeExtensions json -o - "$plist" | grep -q '"markdown"'
```

Extend the smoke test. Start the app with a file argument
(`"$app/Contents/MacOS/Nota" "$RUNNER_TEMP/outside.md"`) and check the log for the
`opened` line we log (`slog.Info("opened file", "kind", …)`). That proves the argv path on a real
macOS runner. The Apple Event path can't be driven headless in CI without a logged-in GUI session.
`open -a` on a runner might work, **[unverified]**; we can try it in an rc build.

### 2.3 Homebrew cask (`packaging/homebrew/nota.rb`)

Nothing is strictly required: Launch Services registers an app the first time Finder sees it in
`/Applications` or it is launched. Optional: in `postflight_steps`, after the `xattr`, add

```ruby
run "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister",
    args: ["-f", "{{appdir}}/Nota.app"]
```

so that **Open With** lists Nota right after `brew install`, before the first launch. Do **not**
use `duti` or anything that sets the default handler. `scripts/bump-cask.sh` doesn't change.
**[verify on Mac: whether the cask DSL accepts this path in `postflight_steps`; else plain `postflight do system_command … end`]**

### 2.4 Ad hoc signing and Launch Services

* Ad hoc signing (`signingIdentity: "-"`) doesn't stop Launch Services from registering document
  types. Registration reads `Info.plist`, not the certificate.
* The quarantine flag is what blocks a launch, and the cask already removes it. A DMG user who
  dragged the app in still gets the first-launch Gatekeeper prompt, now also when the first launch
  comes from double-clicking a `.md`. That's the same friction as today.
* Each release has a new ad hoc cdhash. Launch Services keys on bundle id + path, so updating in
  place keeps the registration **[verify on Mac across an update]**.
* Stale registrations from old dev builds (`apps/desktop/build/...`) can appear as extra "Nota"
  entries in Open With. Clean them with `lsregister -u <path>`. This goes in the manual checklist.
* Default handler: we never set it. Anton can choose it himself in Finder ▸ Get Info ▸ Open with ▸
  Change All. Optional later: Settings › General shows "Default app for .md: TextEdit" using
  `mygo.Shell.DefaultApp` (`modules.go:211`), read-only.

---

## 3. Today's code and what a preview needs

### 3.1 What exists (relevant facts)

* **Desktop wiring** (`apps/desktop/main.go`): `RequestSingleInstanceLock` first (`:144`), services
  bound with `mygo.Bind(s, acc, d)` (`:179`), `OnOpenURL` for `nota://auth` (`:196-201`),
  `OnSecondInstance` → `ShowMain` (`:202`), windows made in `WhenReady` (`:217`). During onboarding
  an empty `&Vault{}` is bound and `ErrNoVault` is returned (`:161`, `apps/desktop/vault.go:187`).
* **Vault identity** (`internal/vault`): paths are NFC (`path.go:46`), case-insensitive collision
  detection is component by component (`vault.go:221-284` `lookup`/`resolve`), and only `.md` counts
  as a note (`path.go:84` `IsNote`). The root is `filepath.Abs`, **not** symlink-resolved
  (`vault.go:57`). `walk` skips non-regular entries (`vault.go:198`), so symlinked notes aren't
  indexed. No code calls `filepath.EvalSymlinks` today. `FreeName` / `freeName` add " 2", " 3"
  case-insensitively (`files.go:91`, `vault.go:490`). `Create` names the file from the title
  (`vault.go:457`), which isn't what we want for an imported file.
* **Watching** covers only the vault root (`apps/desktop/watch.go:102`
  `mygo.WatchFiles(ctx, s.v.Root(), …)`). Sync walks the vault. **An outside file is never seen by
  either** as long as we never write it inside the vault.
* **UI state** (`packages/app/src/state.ts`): `Route` is `home | note | inbox | list | settings`;
  `navigate` calls `reveal(path)`, which expands parent folders (`:92-95`, `:103-117`); **`route` is
  persisted** in `nota:ui` (`:124`). `noteMode` is a per-device preference (`lib/noteMode.ts`).
* **Note page/editor**: `NotePage` → `NoteEditor`, which autosaves through `client.save(path,…)`
  (`components/NoteEditor.tsx:79`) and keeps drafts under `nota:draft:<path>`. **Not usable for an
  outside file.**
* **Editor**: `createEditor(parent, { doc, mode: "reading", onWikilink, onTag, onLink })`
  (`packages/editor/src/index.ts:58-70`, `mode.ts:34-50`). Reading mode is read-only CodeMirror.
  **Images aren't rendered** (no image widget in `decorations.ts`/`preview.ts`), so a hostile file
  can't make the preview load remote images.
* **Link actions**: `openWikilink` **creates a note when the target doesn't exist**
  (`actions.ts:165`). `openExternal` turns any non-URL into `https://…` and calls `window.open`
  (`actions.ts:168-171`). Neither is acceptable inside a preview of an untrusted outside file
  (see §4).
* **Tree**: `FolderTree.tsx` is virtualized (`@tanstack/react-virtual`). The active row is styled
  (`NoteRow.tsx:37`, `Row.tsx:35` `aria-current`), but nothing scrolls it into view: `reveal` only
  expands folders.
* **Startup**: `OpenRecent` in `Shell.tsx:125-137` jumps to the newest note once on a home route,
  which would race with a cold-start open.
* **Client**: optional desktop-only capabilities hang off `NotesClient` (`sync?`, `account?`,
  `shell?`, `packages/client/src/client.ts:62-73`). The http client simply omits them.
* **Commands**: registered in `packages/app/src/builtins/index.tsx` (e.g. `view.mode.toggle`
  `:139`, `note.addToFolder` `:67`). The palette already has a folder-picker mode
  (`PaletteMode = "all" | "move" | "noteIn" | "newFolder"`, `state.ts:30`) with recent folders and the
  inbox suggested first, which is what "Save to vault → choose folder" should reuse.

### 3.2 Recommended architecture

```
Finder / Open With / Dock / open -a / argv / 2nd instance / nota open --app
        │  (MyGo)  App.OnOpenFile(path)          ← registered before Run
        ▼
apps/desktop/open.go  Opener (Go, pure core + thin mygo glue)
   • validate ext/size/regular file → classify:
       in vault & indexed  → Opened{kind:"note", notePath}
       otherwise           → Opened{kind:"external", id, name, displayPath}
   • queue (dedupe by real path / os.SameFile), ShowMain(), emit "opened" nudge
   • Read(id) / Save(id, folder) / Move(id, folder) / Reveal(id) / Dismiss(id) / Follow(id, href)
        ▼  bindings (src/mygo.ts, generated)
packages/client  NotesClient.opener?: OpenerApi   (mygo only; http: absent)
        ▼
packages/app  useOpenedFiles(): take() on mount + on "opened"
   note     → navigate({kind:"note", path, via:"open"}) + reading override + reveal + scroll
   external → navigate({kind:"external", id}) → ExternalPage (banner + read-only editor)
   ⌘K: file.open, file.edit, file.revert, file.discard, file.saveToVault, file.moveToVault,
       file.overwriteOriginal, file.reveal, file.dismiss
```

**Why opaque ids instead of paths over the bridge.** The frontend can only read files that the OS
handed to the app (or that `Follow` resolved from one of them). Even a compromised page then can't
call `ReadExternal("~/.ssh/id_ed25519")`. It also keeps raw OS paths out of the persisted route.

### 3.3 Go: new code

**`internal/vault/locate.go` (new)**

```go
// Locate maps an OS path onto the vault: the vault-relative NFC path of the
// same file, or ok=false when the file is outside the vault, ignored
// (.nota/, .trash/, .notaignore), not a note (.md), or reached only through
// a symlink the vault's walk would not follow.
func (v *Vault) Locate(osPath string) (rel string, ok bool, err error)
```

Algorithm:
1. `real := filepath.EvalSymlinks(abs(osPath))`; `root := filepath.EvalSymlinks(v.Root())`.
   Evaluate both, because `~/Notes` may itself be a symlink and `vault.Open` keeps the
   unresolved `Abs` (`vault.go:57`).
2. `r := filepath.Rel(root, real)`; reject `..` (reuse the `within` logic from `setup.go:161`).
3. `rel := NFC(filepath.ToSlash(r))`; reject `!IsNote(rel)` and `v.Ignored(rel,false)` (also reject
   if any parent directory is ignored, as `walk` does with `SkipDir`).
4. `r := v.resolve(rel)`. If `!r.exists && r.collision != ""` (a differently-cased CLI argument on
   case-insensitive APFS), retry with the collision's on-disk name.
5. **Identity check:** `os.SameFile(os.Stat(real), os.Stat(v.DiskPath(rel)))`. This covers
   NFD/NFC, case and symlinked directories in one test. If it fails, the file is external.
6. Linux/APFS case-sensitive volumes: step 4 can't misfire, because `SameFile` decides.

**`internal/vault` import helper (new, same file or `import.go`)**

```go
// Import writes data as a new note named name (".markdown" becomes ".md") in
// folder, adding " 2", " 3"… on a case-insensitive clash, and emits
// NoteCreated. The bytes are written as given: no frontmatter is added.
func (v *Vault) Import(folder, name string, data []byte) (*Note, error)
```

This does `CleanPath` → `checkWritable` → `mkdirs` → `freeName` → `WriteFile` → `readDisk` → emit,
following `Create` (`vault.go:457-488`) but with the given stem. `Filename()` sanitises the stem.

**`apps/desktop/open.go` (new), service `Opener`**

```go
type Opened struct {
    ID       string `json:"id"`        // random, per run
    Kind     string `json:"kind"`      // "note" | "external" | "error"
    NotePath string `json:"notePath,omitempty"`
    Name     string `json:"name"`      // base name, NFC
    Dir      string `json:"dir"`       // display only, ~-abbreviated
    Error    string `json:"error,omitempty"` // "too large", "not a Markdown file", …
}
type ExternalDoc struct {
    ID, Name, Dir string
    Title    string    // vault.Title(body, stem)
    Content  string    // whole file, valid UTF-8 (lossy if needed)
    Body     string    // after frontmatter (frontmatter.Split)
    Properties map[string]any
    Modified time.Time
    Size     int64
    Hash     string    // protocol.Hash(raw) — Save checks it to avoid importing a changed file silently
    UTF8     bool      // false → shown with U+FFFD, Save refused
}

func (o *Opener) Opened(path string)                 // OnOpenFile target; must not block the main thread
func (o *Opener) Take() []Opened                     // drains the queue (frontend pulls)
func (o *Opener) Read(id string) (ExternalDoc, error)
func (o *Opener) Save(id, folder, hash string) (Note, error)   // copy; original untouched
func (o *Opener) Move(id, folder, hash string) (Note, error)   // explicit; Import then os.Remove (original → Trash via mygo.Shell.TrashItem preferred)
func (o *Opener) Reveal(id string) error             // mygo.Shell.ShowItemInFolder
func (o *Opener) Dismiss(id string)
func (o *Opener) Follow(id, href string) (Opened, error) // relative .md link next to the file → new Opened
func (o *Opener) Pending() []string                  // real paths, for Relaunch(args...)
```

* The core is a struct with injected `emit func()`, `show func()`, `now`, and `vault *Vault`
  (nil while onboarding), with **no mygo calls**, so it can be unit-tested (the skill's pitfall:
  `mygo.Theme.SetSource` blocks with no app running, and other native calls may behave the same).
* `Opened(path)` only stats and enqueues on the main thread; classification (`Locate`, index
  lookup) runs in a goroutine, then `emit()` (the MyGo event `opened`, payload `{seq}`) and
  `d.ShowMain()`.
* "In vault" also requires `s.ix.Get(rel)` to succeed. That way a note that `walk` skips
  (symlinked) or one not indexed yet opens as a preview rather than as a broken note page.
* **Relaunch carry-over:** in `Desktop.restart()` (`onboarding.go:212-217`) call
  `mygo.App.Relaunch(op.Pending()...)`. The new instance gets them through `fileArgs` → `OnOpenFile`
  (`deeplink.go:96-133`). The `d.relaunch` test hook gets the args as well.
* Logging: `slog.Info("opened file", "kind", k)` with **no path** in the log (privacy), except at
  debug level.

**`apps/desktop/main.go`**
* `op := newOpener(...)`; `mygo.App.OnOpenFile(op.Opened)` next to `OnOpenURL` (before `Run`).
* `mygo.Bind(s, acc, d, op)`. `OnSecondInstance` already calls `ShowMain`.
* Then run `mygo generate` → `apps/desktop/src/mygo.ts` (generated; copy back from the devbox per the skill).

### 3.4 Client (`packages/client`)

* `types.ts`: `Opened`, `ExternalDoc`, `OpenerApi { take(); read(id); save(id, folder, hash); move(id, folder, hash); reveal(id); dismiss(id); follow(id, href); onOpened(listener) }`.
* `client.ts`: `opener?: OpenerApi` on `NotesClient` ("Files the OS opened with the app (desktop)").
* `mygo.ts`: add `Opener` to `MygoBindings` and `events.opened`; map `null` arrays the way `list()` does.
* `http.ts`: **no change**; the web app has no OS file opens. (A later "Import .md" in the web app
  could reuse the page with an in-memory `OpenerApi`. Out of scope.)
* `apps/desktop/src/main.tsx:8`: pass `Opener` into `createMygoClient`.

### 3.5 App (`packages/app`)

* `state.ts`
  * `Route` gains `{ kind: "external"; id: string }`; `note` gains optional `via?: "open"`.
  * `partialize`: persist `external` routes as `{kind:"home"}`, because ids die with the process.
  * `opened: Opened[]` (not persisted) plus `addOpened`/`dismissOpened`, for the sidebar section and for
    deduplication.
  * Reading override: `noteModeFor(route, noteMode)` returns `"reading"` while
    `route.via === "open"`. It **doesn't touch the stored per-device preference**. Any explicit mode
    change (⌘E, ModeSwitch, `view.mode.*`) clears `via`. `NotePage`/`NoteEditor` read
    `noteModeFor(...)` instead of `s.noteMode` (`NotePage.tsx:21`, `NoteEditor.tsx:61`).
* `lib/opened.ts` (new, pure, Vitest): reducer for the open queue: dedupe by id/notePath, last one
  wins focus, keep at most N=10 previews.
* `useOpenedFiles()` hook (in `Shell`): `client.opener?.take()` on mount and on every
  `onOpened`; for each result: note → `navigate({kind:"note", path, via:"open"})`; external →
  `addOpened` + `navigate({kind:"external", id})`; error → toast.
* `Shell.tsx`: `OpenRecent` waits for the first `take()` and skips if something was opened (this
  fixes the cold-start race). `Page` gets `case "external": <ExternalPage id=… />`.
* **Tree reveal + scroll:** `FolderTree` scrolls the virtualizer to the active note's index
  (`virtualizer.scrollToIndex(i, { align: "auto" })`) when `route.via === "open"`, or more simply
  whenever the active path changes and the row is off-screen. On narrow screens `navigate` already
  switches to the page pane.
* `components/ExternalPage.tsx` (new): `TopBar` crumbs `[Outside vault, name]`, a "Preview" pill,
  `PreviewBanner`, then a **read-only** `createEditor(host, { doc: body, mode: "reading", onWikilink,
  onTag, onLink })` mount. There's no `NoteEditor` and no drafts. The title comes from
  `splitTitle`, and the properties are a read-only key/value list (or hidden).
  * `onWikilink`: resolve against the vault tree with `resolveLink` and navigate if it's a hit;
    otherwise a toast ("No note named X in your vault"). **Never create.**
  * `onLink`: `http(s):`/`mailto:` → `openExternal`; relative `*.md` → `opener.follow(id, href)`;
    everything else → toast "Can't open links to local files from a preview".
  * `onTag`: no-op or toast (tags belong to the vault).
* `components/PreviewBanner.tsx` (new), see §7.
* Sidebar section `nota.opened` (registry `sidebar`, order 5, rendered only when
  `opened.length > 0`): "Opened files" with one row per preview and an × on each to dismiss.
* `Palette.tsx`: new `PaletteMode` `"saveExternal"` that reuses the folder list (`move` branch,
  `:160-230`); its `pick(folder)` calls `opener.save`. Keep the change small; the skill names
  `Palette.tsx` and `state.ts` as files to keep small.
* `builtins/index.tsx` commands (all with `when: ctx.route.kind === "external"`, no shortcuts):
  * `file.saveToVault`: "Save to vault…" → palette `saveExternal`.
  * `file.moveToVault`: "Move file into vault…" → palette `saveExternal` with `move` and a
    confirmation step ("The original at ~/Downloads/x.md moves to the Trash").
  * `file.edit`: "Edit (draft, nothing saved)" → the preview becomes an in-memory draft in Live
    mode; `file.revert` / `file.discard` while editing. (Replaces the earlier "Edit in Nota = copy
    to the inbox", see Decisions.)
  * `file.overwriteOriginal`: "Overwrite original file…" — the one way the original is written;
    confirmation toast, refused when the file changed since it was read (hash).
  * `file.open`: "Open Markdown file…" (native picker; also File ▸ Open…).
  * `file.reveal`: "Reveal in Finder".
  * `file.dismiss`: "Close preview".
  * Existing `view.mode.*` `when` stays `!!ctx.notePath` (preview is reading-only).
* Menu (`apps/desktop/main.go` `menu()`): optional **File ▸ Open…** (`send("file.open")` → native
  picker → `Opener.Opened`). It's a nice entry point for Mac users and adds no new shortcut; ⌘O stays
  unbound unless Anton wants it (Q8).

---

## 4. Security

| Concern | Decision |
|---|---|
| Arbitrary reads through the bridge | Opaque per-run ids; `Read` only serves files the OS handed us, or `Follow` targets that pass the same checks. |
| Symlinks | `EvalSymlinks` on the file and the vault root; in-vault requires `os.SameFile` with `DiskPath(rel)` **and** an index entry. A symlink in Downloads pointing into the vault opens the vault note; a symlink inside the vault pointing out opens as an external preview. |
| Paths outside the vault / `..` | `filepath.Rel` + the `within` logic; `CleanPath` already refuses `..` for anything we write. |
| Ignored vault paths (`.nota/base/…` sync copies, `.trash/`, `.git/`) | `Locate` returns ok=false → **external preview**, never a note page. Saving such a file "to the vault" makes a new note, which is fine. |
| Huge files | `Opened` refuses > **5 MiB** (open question Q4) with an `error` entry → toast. `Read` uses `io.LimitReader(f, max+1)`. |
| Binary files | A NUL byte in the first 8 KiB → refuse ("doesn't look like a text file"). |
| Non-UTF-8 | Show it with `strings.ToValidUTF8(…, "�")`, banner note "Not UTF-8, shown with replacement characters"; **Save disabled**, because sync and the index assume UTF-8. A UTF-8 BOM is stripped for display and dropped on Save (frontmatter parsing expects `---` at byte 0) **[check `internal/frontmatter` BOM handling in implementation]**. |
| Non-regular files (FIFO, device, dir) | `os.Stat` after EvalSymlinks; `Mode().IsRegular()` only. (MyGo's argv path already does this, `deeplink.go:122`; the Apple Event path doesn't.) |
| iCloud/Dropbox placeholders | Reads can block while the file downloads, so `Opened` classifies off the main thread. |
| Relative images/links | Images aren't rendered at all (editor has no image widget). Relative `.md` links → `Follow` (same checks as Opened). Other relative links → toast. Wikilinks resolve in the vault, never create. Only `http(s)`/`mailto` go to `openExternal`; `javascript:`, `file:` and custom schemes are blocked in the preview. Vault notes may want the same hardening later. |
| Watch/sync picking the file up | They only see `v.Root()`. We never write outside files into the vault without an explicit Save/Move, and we never write to the original. `Move` = `Import` + `mygo.Shell.TrashItem(original)` so it can be undone, refused unless the hash still matches. |
| Save of a changed file | `Save(id, folder, hash)` re-reads; if the hash changed since the preview loaded, return a conflict → the UI reloads the preview and asks again. |
| Hooks | `Import` emits `NoteCreated` → transform hooks may file or alter the imported note (same as `nota add`). Intended; the toast shows where it ended up (`bus.Follow`, as `Vault.Create` does at `apps/desktop/vault.go:617`). |
| Logs | No full paths at info level. |

---

## 5. Edge cases

| Case | Behaviour |
|---|---|
| **Onboarding active** (no vault) | `Opener` queues; the page shows the wizard (`App.tsx` `Gate`), so `take()` isn't called. At the end of setup, `restart()` relaunches with `Pending()` as args → the new instance opens them with the vault. Alternative (Q5): offer a vault-less read-only preview right away. |
| **Vault switch/new relaunch** while previews are open | Same carry-over, previews only (not the note route). |
| **Several files at once** (Finder multi-select → one `openURLs` with N files) | Every file goes into "Opened files" in the sidebar; the **last one** gets focus. At most 10 previews; the oldest drops. No tabs (Nota has none). |
| **Same file twice** | Dedupe by real path (`os.SameFile`) in Go → same id → just focus it. |
| **Vault note opened twice / via a symlinked path / differently-cased path** | `Locate` resolves all three to the same `rel`; `navigate` to it is idempotent. |
| **File changes on disk while previewing** | v1: re-`Read` when the window regains focus, and on an `externalChanged` event from a per-preview `mygo.WatchFiles(ctx, dir, {Recursive:false, Ignore: not(name)})`. Watch the directory, not the file, because editors replace by rename. Cancel it on Dismiss. Live reload keeps the scroll position via `replaceDoc` (`@nota/editor`). The step can be dropped (refresh on focus only) if time is short. |
| **File deleted/moved while previewing** | `Read` → not found → the banner switches to "This file is gone" with Dismiss. |
| **In-vault file that isn't indexed yet** (made by another app a moment ago) | `Locate` ok but `ix.Get` fails → run `ix.Sync` once (the watcher would do the same) and retry; else external preview. |
| **`.markdown` inside the vault** | Not a note (`IsNote` is `.md` only) → external preview; "Save to vault" makes `name.md`. |
| **Case-insensitive APFS** | Handled by the `resolve` collision retry + `SameFile`. |
| **App not running, `open`/double-click** | Cold start: queue → `take()` on mount; `OpenRecent` skipped. |
| **App running** | macOS: Apple Event to the running instance (no second process). Linux / direct exec: second instance → socket → `deliverArgs`. |
| **Quick capture window focused** | `ShowMain()` brings the main window forward; capture stays hidden. |
| **Window closed on macOS (app still running)** | `ShowMain` currently does nothing if `d.main` is nil or closed **[check: main window closed on macOS → is `d.main` a dead window?]**. May need "recreate main window" logic, which also improves `nota://auth`. |
| **`nota open --app` without the app installed** | `open -b` fails → CLI exit 1: "Nota.app isn't installed (brew install --cask banjo/tap/nota)". |
| **Linux CLI** | Best effort: `gtk-launch <desktop-id> <file>` **[unverified: desktop file id MyGo installs]**, else print a hint. Out of scope for v1 beyond not crashing. |

---

## 6. Testing

### 6.1 Headless (devbox / mini; never on the Hermes host)

* **Go unit**, `internal/vault/locate_test.go` (`t.TempDir`, real OS FS): inside; outside;
  `../` escape; vault root itself a symlink; file symlink in → out and out → in; symlinked
  subfolder; NFD name on disk with NFC input (write the NFD bytes directly); case-variant
  input (skipped where the FS is case-sensitive, but still asserts `ok=false` for a real other
  file); `.nota/base/x.md`, `.trash/x.md`, `.notaignore` rule; `.markdown`; directory; FIFO
  (`syscall.Mkfifo`, unix only).
* `internal/vault/import_test.go`: name kept; `.markdown` → `.md`; " 2" on a case-insensitive clash;
  ignored folder refused; event emitted; bytes byte-identical (no `created` stamp); new folder made.
* `apps/desktop/open_test.go` (pure `Opener`, fake emit/show, no mygo): queue before the frontend,
  `Take` drains; dedupe; classify note vs external; too large / binary / non-UTF-8; `Save` with a
  stale hash → conflict; `Move` refuses on a hash mismatch (trash call injected); `Follow`
  refuses `../../etc/passwd.md` outside the original's directory tree? (Q9: allow any
  `.md` reachable by a relative link, or only the same directory and below. Recommended: any
  existing `.md`, since the user can open it from Finder anyway, but still through the same
  checks); `Pending()` for relaunch; onboarding (nil vault) → everything external, Save →
  `ErrNoVault`.
* **CLI testscript** `cmd/nota/testdata/script/open_app.txtar`: a fake `open` script on `PATH`
  that records argv (works because the CLI uses `exec.Command("open", …)`, unlike GIO);
  `nota open --app note` → absolute disk path of the note; `nota open --app ./x.md` (outside) →
  absolute path; a missing file → exit 3; `GOOS` guard: on Linux the test checks the
  fallback message.
* **Vitest** (`packages/app/tests/`): `opened.test.ts` (reducer); `note-mode.test.ts` add
  `noteModeFor` override + clear-on-toggle; `external-page.test.tsx` with a fake `OpenerApi`:
  renders the banner, Save calls `opener.save(id, inbox, hash)`, a wikilink miss never calls
  `client.create`, a `javascript:` link isn't opened; `state` persistence maps `external` →
  `home`; `Shell` doesn't run `OpenRecent` when `take()` returned something.
* **Real open-event injection on Linux under Xvfb** (recipe: mini-pc skill
  `headless-desktop-apps`), using MyGo's actual argv and second-instance code that feeds the same
  `OnOpenFile`:
  1. Cold: `build/linux-amd64/nota /tmp/outside.md` → screenshot shows the preview + banner.
  2. Warm: with the app running, `env -u XDG_RUNTIME_DIR build/linux-amd64/nota "$VAULT/Projects/a.md"`
     (the skill pitfall: both processes must compute the same socket path) → the note opens in Reading,
     tree expanded and the row visible; `pgrep` shows one instance.
  3. `gio open /tmp/outside.md` after setting Nota as the `text/markdown` default in the test
     `HOME`'s `mimeapps.list` → exercises the `.desktop` `%U` path.
  4. Save to vault from the ⌘K palette (xdotool) → file appears in `Inbox/`, original unchanged
     (`sha256sum` before/after).
* Per-commit gate (as in the skill / CI `ci.yml`): `gofmt -l .` (empty), `go vet ./...`,
  `go test ./...`, `bunx vp check`, `bunx vp run -r test`, `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./apps/desktop ./cmd/nota`
  and `GOOS=linux … go build`, plus `mygo generate` output copied back when bindings change.

### 6.2 Only Anton can verify (real Mac). Checklist

Build an rc (`v0.1.x-rc.N` tag → release workflow) or `bunx mygo build -platform darwin/arm64`, install to
`/Applications` (brew from the rc cask, or drag + `xattr -dr com.apple.quarantine`).

1. `lsregister -dump | grep -A3 -i "com.banjoanton.nota"` shows the Markdown document type, rank Alternate.
2. `plutil -p /Applications/Nota.app/Contents/Info.plist | grep -A12 CFBundleDocumentTypes`.
3. **Default unchanged:** double-click an existing `README.md` → still opens in the previous app (TextEdit/Xcode/…).
4. Finder ▸ right-click `.md` ▸ **Open With** lists Nota (no "Other…" needed). Also try a `.markdown` file.
5. Open With ▸ Nota **while Nota is quit** (cold start) → window shows the preview banner; no flash of the recent note.
6. Same **while Nota is running**, with the window in the background / minimised / on another Space / closed (⌘W) → window comes to the front with the preview.
7. Drag a `.md` onto the **Dock icon** (running and not running). Drag a `.txt` → Dock doesn't highlight (or Nota shows "not a Markdown file").
8. `open -a Nota ~/Downloads/x.md` and `nota open --app ~/Downloads/x.md` from Terminal (running/not running).
9. A note **inside the vault** via Finder → opens as the normal note in Reading, tree expanded and scrolled to it, highlighted; ⌘E → Live, and the stored mode preference didn't change for the next note.
10. The same vault note through a **symlinked path** (e.g. `ln -s ~/Notes ~/n; open -a Nota ~/n/Inbox/a.md`) and through **wrong case** (`nota open --app ~/notes/inbox/A.md`) → same note.
11. Select 3 `.md` files in Finder ▸ Open With Nota → "Opened files" lists 3, the last one is shown.
12. Save to vault → palette with Inbox first → pick a folder that already has `x.md` → new `x 2.md`; original still in Downloads, unchanged (`shasum` before/after).
13. Move into vault → confirm → original goes to the **Trash** (Put Back works).
14. Edit the previewed file in another editor → preview updates on focus (or live).
15. A file that isn't UTF-8 (`iconv -t latin1`), a 20 MB `.md`, a renamed PNG → friendly refusals.
16. First run with no vault: open a `.md` → finish the setup → after the relaunch the file opens as a preview.
17. After `brew upgrade` to the next rc, Open With still lists one Nota (no stale duplicate); if there are duplicates from dev builds, `lsregister -u <path>` removes them.
18. Quick capture (⌥⌘N) and `nota://auth` sign-in still work (shared `OnOpenURL` / single-instance code).

---

## 7. Preview banner: three variants

The UI is a real choice because "obviously a preview" competes with the quiet Träd look. The
skill's rule for unsure UI is static prototypes on a `proto/*` branch. I suggest a quick
`proto/open-markdown` with these three if Anton wants to see them; otherwise go with A.

**A. Banner strip under the top bar (recommended)**

```
┌───────────────────────────────────────────────────────────────────────────┐
│ Outside vault › README          [Preview]                  ◫ Reading  ⋯ │  ← TopBar
├───────────────────────────────────────────────────────────────────────────┤
│ ⓘ  Preview of ~/Downloads/README.md. It isn't in your vault.             │  ← ink-soft strip
│    [ Save to vault… ]   Reveal in Finder   ·   Close                     │
├───────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│   README                                                                  │
│   Lorem ipsum… (read-only, reading mode)                                  │
```
Pros: impossible to miss, room for the path (the "never silently copied" promise is spelled out), and
the primary action is one click. Cons: one extra row. On phones it isn't reachable anyway (desktop only).

**B. Top-bar only (quietest)**

```
┌───────────────────────────────────────────────────────────────────────────┐
│ ⤓ README.md  [Preview · not in vault]        [Save to vault…]  ✕   ⋯    │
├───────────────────────────────────────────────────────────────────────────┤
│   README                                                                  │
```
Pros: no extra chrome, closest to the current NotePage. Cons: the path is hidden (tooltip only), and the
"Preview" pill looks like the existing "Reading" pill, so it's easy to mistake for a vault note.

**C. Floating action bar at the bottom**

```
│   README                                                                  │
│   Lorem ipsum…                                                            │
│                                                                           │
│        ╭───────────────────────────────────────────────────────────╮      │
│        │ 📄 ~/Downloads/README.md · preview    Save to vault…  ✕   │      │
│        ╰───────────────────────────────────────────────────────────╯      │
└───────────────────────────────────────────────────────────────────────────┘
```
Pros: the content starts at the top; it echoes the phone sheet pattern. Cons: covers text at the end of
the page, a new visual pattern on desktop, and easy to dismiss mentally as a toast.

**Recommendation: A**, with the TopBar crumb "Outside vault" and pill from B. The sidebar adds a
small "Opened files" section (only while there are previews). Every button also has a ⌘K command
(`file.*`), per Anton's rule; no new shortcuts.

---

## 8. Ordered implementation (one commit each, gate after each)

Gate = §6.1 per-commit gate. Bindings regenerated with `mygo generate` on the devbox and copied back.

| # | Commit | Files | Extra check |
|---|---|---|---|
| 1 | `feat(vault): locate OS paths in the vault and import files by name` | `internal/vault/locate.go`, `import.go`, tests | Go tests above |
| 2 | `feat(desktop): receive files opened with the app` | `apps/desktop/open.go`, `open_test.go`, `main.go` (OnOpenFile before Run, Bind), `onboarding.go` (Relaunch with pending), `mygo.config.ts` (fileAssociations + infoPlist override), `src/mygo.ts` (generated) | `mygo build -platform linux/amd64` on devbox: `.desktop` has `MimeType=text/markdown` + `%U`; Xvfb cold/warm argv checks print the `opened file` log |
| 3 | `feat(client): opener API for desktop file opens` | `packages/client/src/{types,client,mygo}.ts`, `apps/desktop/src/main.tsx`, client tests | `vp check` |
| 4 | `feat(app): open vault notes from Finder in reading mode` | `state.ts` (route `via`, `noteModeFor`, `opened`, partialize), `lib/opened.ts`, `Shell.tsx` (hook, OpenRecent), `FolderTree.tsx` (scroll to active), `NotePage.tsx`/`NoteEditor.tsx` (mode source), tests | Xvfb: warm open of a vault note → screenshot tree + Reading |
| 5 | `feat(app): preview Markdown files outside the vault` | `ExternalPage.tsx`, `PreviewBanner.tsx`, sidebar section, `Palette.tsx` (`saveExternal`), `builtins/index.tsx` (`file.*`), tests | Xvfb: cold open of an outside file, Save via ⌘K, `sha256sum` of the original unchanged; screenshots light/dark |
| 6 | `feat(desktop): follow a previewed file's changes` | `open.go` (per-preview dir watch, `externalChanged` event), client + page reload | Xvfb: `echo >> outside.md` → preview updates |
| 7 | `feat(cli): nota open --app` | `internal/cli/commands.go` (flag on `openCmd`), `docs/cli.md`, testscript | testscript with fake `open` |
| 8 | `build(release): Markdown document type checks, docs` | `.github/workflows/release.yml` (plutil asserts + argv smoke), `packaging/homebrew/nota.rb` (optional lsregister), `docs/release.md`, README line | `actionlint` if available; real proof = rc tag run (needs Anton's ok to tag) |

Then: rc tag (Anton's ok), Anton runs the §6.2 checklist, PR to `main`.

---

## 9. Risks

* **Launch Services quirks with ad hoc apps** (rank honoured? duplicates from dev builds? Open
  With listing before first launch?). I can't test any of this off a Mac → the checklist plus the
  optional `lsregister -f` in the cask.
* **The cold-start ordering assumption.** If AppKit delivered `openURLs` *after* our
  `WhenReady` window but before the page loaded, the queue+pull design still works. If it arrived
  before `OnOpenFile` is registered, nothing could catch it, but we register before `Run`, so
  that case is ruled out.
* **`ShowMain` when the main window was closed on macOS** (app keeps running) may do nothing →
  the file "doesn't open". Needs a check, and possibly a "recreate the main window" path (it would
  also help `nota://auth`).
* **The override drifts from `fileAssociations`**: two places list the extensions. The release
  plutil assert and a comment in the config reduce the risk; the upstream MyGo PR removes it.
* **Linux default hijack** via `MimeType=text/markdown` in the `.deb` desktop entry.
* Palette/state growth: keep `saveExternal` inside the existing `move` branch to stay small.
* MyGo upgrades: `bundle.go:294`/`:315` behaviour could change; the plist assert would catch it.

## 10. Open questions for Anton (answered, see Decisions at the top)

1. **CLI name:** `nota open` already means "open a vault note in `$EDITOR`" (`internal/cli/commands.go:323-360`). OK with `nota open --app <file|note>` (short `-a`)? Or should `nota open <path-to-existing-file>` go to the app automatically when the argument is a file path rather than a note name?
2. **Default handler:** confirm "Open With only, rank Alternate, never set the default". Should Settings show which app is the current default (read-only)?
3. **"Open in editor"**: which of these? (a) **Edit in Nota** = save a copy to the inbox and switch to Live (recommended, no writes to the original); (b) edit the original file in place, explicitly, with hash-checked saves (more work, a second save path); (c) open in the system text editor (`open -t`).
4. Size limit for previews: 5 MiB OK?
5. **During onboarding**: queue the file and open it after setup (recommended), or show a vault-less preview straight away?
6. Finder role **Viewer** (recommended, matches "preview") or **Editor**?
7. **Move into vault**: include it in v1 (original → Trash after a confirmation), or copy only?
8. Should there be a **File ▸ Open…** menu item (native picker → same preview)? Should ⌘O be bound, or stay ⌘K-only?
9. Relative `.md` links inside a preview: follow them as new previews (recommended) or ignore?
10. Should a saved/imported note get Nota frontmatter (`created`), or keep the bytes exactly (recommended: exact)?
11. Want the 3 banner variants as a `proto/open-markdown` prototype, or go with A?

## 11. Effort

| Part | Estimate |
|---|---|
| vault `Locate`/`Import` + tests | 0.5 d |
| desktop `Opener` + wiring + config + tests | 1 d |
| client | 0.25 d |
| app: vault-note path (route, reading override, reveal/scroll, OpenRecent) | 0.75 d |
| app: preview page, banner, sidebar, palette mode, commands | 1–1.5 d |
| live reload | 0.5 d (optional) |
| CLI `--app` | 0.25 d |
| release/cask/docs | 0.25 d |
| Xvfb verification + screenshots | 0.5 d |
| **Total** | **≈ 4.5–5 subagent-days**, plus Anton's Mac checklist (30–45 min) and one rc release |

## 12. What I could not verify

* Any real macOS behaviour: event order on cold start, the effect of `LSHandlerRank`, Open With
  listing, Dock-drop highlighting, `lsregister` with ad hoc signing, whether the
  `net.daringfireball.markdown` UTI is system-declared on macOS 12–15. I took these from AppKit/LS
  knowledge and MyGo source, not from running them.
* Whether `open -a` works on a GitHub macOS runner (no GUI login session).
* The Linux desktop-file id for `gtk-launch`.
* What `ShowMain` does after the main window was closed on macOS.
* How `internal/frontmatter` handles a UTF-8 BOM.
* Nothing was built or tested for this plan; it comes from reading the code.
