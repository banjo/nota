# UI performance: render counts

A reproducible harness that counts what React actually renders while a
browser drives the real `packages/app` UI through fixed scenarios. Use it
before and after a change that could touch render behaviour.

## How it works

- **App**: `scripts/perf/app` mounts `NotaApp` from `packages/app` on an
  in-memory `NotesClient` (`memory-client.ts`), so no server is needed.
  It behaves like the desktop client: writes resolve after ~2 ms and a
  "watcher" emits the changed paths 30 ms later, so the change
  subscription and query invalidation run as in the app. Vaults are
  deterministic (`seed.ts`): a small vault (41 notes with links, tags,
  callouts, tables, code), `?vault=large` (2001 notes in 61 folders) and
  `?big=1` (adds a ~5000-line note).
- **Build**: `scripts/perf/vite.config.mjs` builds it like `apps/web`
  (React Compiler, Tailwind) with **production React** (react-dom's
  production bundle) and unminified app code, so component names survive.
  Production React still calls the DevTools hook and sets the
  PerformedWork flag, so no dev or profiling build is needed, and the
  timings are not inflated by dev-only checks.
  Scenario d's commit count jitters by ±3 between runs (the 120 ms search
  debounce against typing speed); everything else repeats exactly.
- **Counting** (`scripts/perf/hook.js`, injected with `addInitScript`
  before any page script): a stub `__REACT_DEVTOOLS_GLOBAL_HOOK__` whose
  `onCommitFiberRoot` walks the committed fiber tree. A component counts
  as rendered in a commit when its fiber has `PerformedWork` and lies in
  the part React reconciled (a subtree React bailed out of keeps its old
  fibers, `fiber.child === fiber.alternate.child`, and is skipped). A
  component whose function ran but bailed out (same props and state) is
  not counted, the same rule the React DevTools profiler uses. Names are
  `displayName || name`; Radix internals show as `Primitive.div`,
  `ForwardRef`, … and lucide icons by icon name (`Palette`, `Ellipsis`).
- **Also recorded**: `longtask` entries (PerformanceObserver) and
  scripting time (CDP `Performance.getMetrics` `ScriptDuration` delta).
- **Runs**: each scenario runs in a fresh browser context (fresh
  localStorage), the page loads and settles, counters reset, the scenario
  runs, then it waits until no commit for 1 s. Three runs, the median is
  reported. Headless Chromium 1440×900 on the devbox.

## Scenarios

| id | what |
| --- | --- |
| a | cold load until the first note (the newest, opened by `OpenRecent`) shows; counted from `goto` |
| b / b-2000 | open 10 different notes by clicking them in the sidebar tree (small / 2000-note vault) |
| c | type 60 characters into a note body (30 ms apart), including the debounced save |
| d | ⌘K, type `note 1`, ↓ ×5, Esc |
| e / e-2000 | expand 5 folders, collapse them, toggle the sidebar twice |
| f | properties shown: add a tag chip, change the status |
| g | Reading → Source → Live, then via ⌘K: dark mode, Theme: Graphite, dark mode, Theme: Nota |
| h | idle 5 s after load (target: 0 commits) |
| i | open the 5000-line note, scroll 20 × 1500 px, type 29 characters |

Scenarios c and i check that the typed text reached the client.

## Running

On the devbox (never on the Hermes host):

```sh
bunx vp build --config scripts/perf/vite.config.mjs
PLAYWRIGHT=/tmp/pw/node_modules/playwright/index.mjs \
CHROME=$HOME/.cache/ms-playwright/chromium-1255/chrome-linux64/chrome \
  bun scripts/perf/run.mjs --runs 3 --out perf/after.json [--only a,b] [--commit <sha>]
bun scripts/perf/report.mjs perf/baseline.json perf/after.json
sh scripts/perf/bundle.sh   # apps/web build sizes
```

`--budget perf/budget.json` is the render-count regression test: it fails
(exit 1) when a scenario's median commits or renders exceed
`perf/budget.json` (the numbers after the perf work plus headroom; scenario
h stays at 0). Raise a budget only with a reason in the commit.

`--trace` prints every commit with what rendered in it (top 8 names), for
finding what drives a scenario; it does not change the numbers. Results
are plain `JSON.stringify` output: run `bunx vp check --fix` before
committing them.

## Baseline (b0fe4b8)

`perf/baseline.json`, median of 3 runs.

| Scenario | Commits | Component renders | Long tasks (n / ms) | Scripting ms | Top 5 |
| --- | --- | --- | --- | --- | --- |
| **a** cold load → first note visible (first note 188 ms) | 11 | 873 | 0 / 0 | 111 | ForwardRef 60, Primitive.div 42, Presence 40, Popper 39, PopperProvider 39 |
| **b** open 10 notes via sidebar | 30 | 1520 | 0 / 0 | 70 | Row 310, NoteRow 210, FolderRow 100, ForwardRef 70, DropdownMenu 60 |
| **b-2000** open 10 notes via sidebar (2000 notes) | 30 | 1604 | 0 / 0 | 65 | Row 360, NoteRow 320, ForwardRef 70, DropdownMenu 58, DropdownMenuTrigger 58 |
| **c** type 60 chars in a note | 7 | 104 | 0 / 0 | 133 | Row 9, ContextMenu 8, MenuProvider 8, FolderRow 5, ContextMenuContent 4 |
| **d** ⌘K: open, type query, ↓×5, close | 53 | 1991 | 0 / 0 | 55 | ForwardRef 536, Primitive.div 411, CommandItem 213, NoteItem 90, Presence 66 |
| **e** expand/collapse 5 folders, toggle sidebar | 44 | 1568 | 0 / 0 | 44 | Row 253, NoteRow 153, FolderRow 100, ForwardRef 80, Primitive.div 76 |
| **e-2000** expand/collapse 5 folders, toggle sidebar (2000 notes) | 21 | 10542 | 0 / 0 | 77 | ForwardRef 665, Popper 616, PopperAnchor 616, PopperProvider 616, Primitive.button 616 |
| **f** properties: add tag chip, change status | 36 | 631 | 0 / 0 | 30 | Primitive.div 41, MenuProvider 28, Primitive.div.Slot 22, Presence 21, Row 18 |
| **g** modes Live/Reading/Source, dark mode, palette | 193 | 7541 | 0 / 0 | 158 | ForwardRef 1674, Primitive.div 1349, CommandItem 507, Presence 400, Palette 375 |
| **h** idle 5 s after load | 0 | 0 | 0 / 0 | 0 | — |
| **i** 5000-line note: open, scroll, type | 10 | 238 | 0 / 0 | 310 | Row 32, NoteRow 18, FolderRow 14, MenuProvider 14, ContextMenu 8 |

Top 15 per scenario are in `perf/baseline.json` (`scenarios.<id>.top`).

Bundle (`sh scripts/perf/bundle.sh`, apps/web): all JS+CSS 2,460,626 B
(840,122 B gzip, 115 files); loaded by `index.html` 1,440,462 B
(449,659 B gzip); main chunk `index-*.js` 1,077,104 B (341,649 B gzip).

## After (f8b4bbe)

`perf/after.json`, median of 3 runs, same harness and machine.

| Scenario | Commits | Component renders | Long tasks (n / ms) | Scripting ms |
| --- | --- | --- | --- | --- |
| **a** cold load → first note visible (first note 188 → 186 (-1%) ms) | 11 → 10 (-9%) | 873 → 357 (-59%) | 0 / 0 → 0 / 0 | 111 → 106 (-5%) |
| **b** open 10 notes via sidebar | 30 → 30 (0%) | 1520 → 600 (-61%) | 0 / 0 → 0 / 0 | 70 → 64 (-9%) |
| **b-2000** open 10 notes via sidebar (2000 notes) | 30 → 30 (0%) | 1604 → 597 (-63%) | 0 / 0 → 0 / 0 | 65 → 58 (-11%) |
| **c** type 60 chars in a note | 7 → 4 (-43%) | 104 → 11 (-89%) | 0 / 0 → 0 / 0 | 133 → 137 (+3%) |
| **d** ⌘K: open, type query, ↓×5, close | 53 → 49 (-8%) | 1991 → 865 (-57%) | 0 / 0 → 0 / 0 | 55 → 48 (-13%) |
| **e** expand/collapse 5 folders, toggle sidebar | 44 → 36 (-18%) | 1568 → 394 (-75%) | 0 / 0 → 0 / 0 | 44 → 40 (-9%) |
| **e-2000** expand/collapse 5 folders, toggle sidebar (2000 notes) | 21 → 11 (-48%) | 10542 → 2286 (-78%) | 0 / 0 → 0 / 0 | 77 → 45 (-42%) |
| **f** properties: add tag chip, change status | 36 → 30 (-17%) | 631 → 445 (-29%) | 0 / 0 → 0 / 0 | 30 → 27 (-10%) |
| **g** modes Live/Reading/Source, dark mode, palette | 193 → 191 (-1%) | 7541 → 3337 (-56%) | 0 / 0 → 0 / 0 | 158 → 132 (-16%) |
| **h** idle 5 s after load | 0 → 0 (±0%) | 0 → 0 (±0%) | 0 / 0 → 0 / 0 | 0 → 0 (±0%) |
| **i** 5000-line note: open, scroll, type | 10 → 7 (-30%) | 238 → 69 (-71%) | 0 / 0 → 0 / 0 | 310 → 316 (+2%) |

### Top 15 components by renders, before → after (median of runs)

- **a** before: ForwardRef 60, Primitive.div 42, Presence 40, Popper 39, PopperProvider 39, Tooltip 39, TooltipTrigger 39, PopperAnchor 38, Primitive.button 36, Primitive.div.Slot 36, TooltipProvider 28, Primitive.button.Slot 26, TooltipPortal 26, TooltipPortalProvider 26, Row 24
  - after: ForwardRef 60, Row 15, IconButton 13, ChevronRight 11, Ellipsis 10, Presence 9, Primitive.div 8, FolderPlus 7, Dialog 6, FilePlusCorner 6, FolderRow 6, MenuProvider 6, Folder 5, FolderActions 5, FolderRowActions 5
- **b** before: Row 310, NoteRow 210, FolderRow 100, ForwardRef 70, DropdownMenu 60, DropdownMenuTrigger 60, MenuProvider 60, Popper 40, PopperAnchor 40, PopperProvider 40, Primitive.button 40, Primitive.div 40, Primitive.div.Slot 40, DropdownMenuProvider 30, Menu 30
  - after: ForwardRef 70, NoteMenu 30, DropdownMenu 20, DropdownMenuTrigger 20, MenuProvider 20, NotePage 20, NoteRow 20, Popper 20, PopperAnchor 20, PopperProvider 20, Primitive.button 20, Primitive.div 20, Primitive.div.Slot 20, Row 20, TopBar 20
- **b-2000** before: Row 360, NoteRow 320, ForwardRef 70, DropdownMenu 58, DropdownMenuTrigger 58, MenuProvider 58, FolderRow 40, Popper 39, PopperAnchor 39, PopperProvider 39, Primitive.button 39, Primitive.div 39, Primitive.div.Slot 39, DropdownMenuProvider 29, Menu 29
  - after: ForwardRef 70, NoteMenu 29, DropdownMenu 20, DropdownMenuTrigger 20, MenuProvider 20, NotePage 20, Popper 20, PopperAnchor 20, PopperProvider 20, Primitive.button 20, Primitive.div 20, Primitive.div.Slot 20, TopBar 20, NoteRow 19, Row 19
- **c** before: Row 9, ContextMenu 8, MenuProvider 8, FolderRow 5, ContextMenuContent 4, ContextMenuPortal 4, ContextMenuProvider 4, Dialog 4, DialogProvider 4, Menu 4, MenuPortal 4, MenuPortalProvider 4, NoteRow 4, Palette 4, Popper 4
  - after: BacklinksPanel 2, NavSection 2, FolderTree 1, NoteEditor 1, NotePage 1, NoteRow 1, PropertiesSection 1, Row 1, TopBar 1
- **d** before: ForwardRef 536, Primitive.div 411, CommandItem 213, NoteItem 90, Presence 66, Palette 62, Primitive.div.Slot 45, Portal 32, CommandShortcut 31, Dialog 30, DialogOverlay 30, CommandGroup 28, DialogContent 27, FileText 25, Primitive.input 20
  - after: ForwardRef 269, Primitive.div 194, CommandItem 63, CommandRow 31, NoteItem 25, FileText 24, CommandGroup 22, Primitive.input 20, Presence 18, Palette 17, CommandShortcut 14, Command 13, PaletteBody 13, SearchMode 10, Primitive.div.Slot 9
- **e** before: Row 253, NoteRow 153, FolderRow 100, ForwardRef 80, Primitive.div 76, Popper 54, PopperAnchor 54, PopperProvider 54, Presence 54, Primitive.button 54, Primitive.div.Slot 54, DropdownMenu 34, DropdownMenuTrigger 34, MenuProvider 34, Tooltip 30
  - after: ForwardRef 80, Row 32, Ellipsis 22, Primitive.div 22, FileText 17, NoteMenu 17, NoteRow 17, Presence 17, ChevronRight 15, FolderRow 15, ScrollAreaScrollbarImpl 13, ScrollAreaScrollbarProvider 13, ScrollAreaScrollbarVisible 13, ScrollAreaScrollbarY 13, FolderTree 12
- **e-2000** before: ForwardRef 665, Popper 616, PopperAnchor 616, PopperProvider 616, Primitive.button 616, Primitive.div 616, Primitive.div.Slot 616, DropdownMenu 504, DropdownMenuTrigger 504, MenuProvider 504, Presence 364, Row 330, Ellipsis 280, DropdownMenuContent 252, DropdownMenuPortal 252
  - after: ForwardRef 665, Row 290, Ellipsis 280, FileText 252, NoteMenu 252, NoteRow 252, IconButton 56, ChevronRight 38, FolderRow 38, Folder 29, FilePlusCorner 28, FolderActions 28, FolderPlus 28, FolderRowActions 28, FolderTree 10
- **f** before: Primitive.div 41, MenuProvider 28, Primitive.div.Slot 22, Presence 21, Row 18, ContextMenu 16, Popper 16, PopperProvider 16, ForwardRef 14, Menu 14, MenuPortal 14, MenuPortalProvider 14, Primitive.span 12, Primitive.span.Slot 12, RovingFocusGroupCollectionItemSlot 12
  - after: Primitive.div 41, Primitive.div.Slot 22, ForwardRef 14, Presence 13, MenuProvider 12, Primitive.span 12, Primitive.span.Slot 12, RovingFocusGroupCollectionItemSlot 12, RovingFocusGroupCollectionItemSlot.Slot 12, RovingFocusGroupItem 12, MenuCollectionItemSlot 9, MenuCollectionItemSlot.Slot 9, MenuItemImpl 9, AddInline 8, DropdownMenuItem 8
- **g** before: ForwardRef 1674, Primitive.div 1349, CommandItem 507, Presence 400, Palette 375, Primitive.div.Slot 282, Portal 196, Dialog 194, DialogOverlay 188, DialogContent 179, CommandGroup 101, DialogPortal 101, DialogPortalProvider 101, DialogProvider 101, DismissableLayer 95
  - after: ForwardRef 995, Primitive.div 725, CommandItem 203, CommandRow 124, CommandGroup 91, Primitive.input 88, Command 86, PaletteBody 86, Presence 72, Palette 68, CommandShortcut 65, SearchMode 46, CommandInput 45, CommandList 45, NoteItem 42
- **h** before: —
  - after: —
- **i** before: Row 32, NoteRow 18, FolderRow 14, MenuProvider 14, ContextMenu 8, Popper 8, PopperProvider 8, Menu 7, DropdownMenu 6, DropdownMenuTrigger 6, ForwardRef 6, MenuPortal 5, MenuPortalProvider 5, Presence 5, ContextMenuContent 4
  - after: ForwardRef 6, BacklinksPanel 3, NoteMenu 3, NotePage 3, NoteRow 3, Row 3, TopBar 3, DropdownMenu 2, DropdownMenuTrigger 2, MenuProvider 2, NavSection 2, NoteEditor 2, Popper 2, PopperAnchor 2, PopperProvider 2

### What won

| Commit | Fix | Biggest effect |
| --- | --- | --- |
| 239fccb | tree, nav and opened rows select what they show instead of the whole route; tree rows memoised | open 10 notes 1520 → 900 renders |
| 93da8c9 | tree "…" menus and folder-row tooltips mount on first pointer/focus (`lib/armed.ts`) | cold load 873 → 371, 2000-note expand/collapse 10542 → 2286 renders and 77 → 47 ms |
| 918cf73 | ⌘K query lives in a body inside the dialog; rows memoised | palette 1839 → 1180 |
| 5c17a17 | closed overlays select one value from the tree (`useTreeValue`) | typing 104 → 11, 5000-line note 146 → 69 |
| 6d33dec | Settings and the vault wizard are lazy chunks, prefetched at idle | initial JS+CSS −12.9 kB gzip |
| f8b4bbe | stable command context in the palette | palette 1069 → 865, modes/themes 4745 → 3287 |

Long tasks were 0 in nearly every run before and after on this machine
(cold load showed one 50 ms task in some runs, before and after), so
render counts and scripting time are the useful signal here.

### Bundle (apps/web, `sh scripts/perf/bundle.sh`)

| | before (b0fe4b8) | after |
| --- | --- | --- |
| main chunk `index-*.js` | 1,077,104 B / 341,649 B gz | 1,030,248 B / 328,787 B gz |
| loaded by `index.html` | 1,440,462 B / 449,659 B gz | 1,393,606 B / 436,797 B gz (−2.9%) |
| all JS+CSS | 2,460,626 B / 840,122 B gz | 2,464,985 B / 844,515 B gz |
| new lazy chunks | — | Settings 10,268 B gz, Onboarding 6,819 B gz |

### Deliberately not changed

- **Editor**: the CodeMirror view is created once per note, modes switch
  through a compartment, external reloads replace only the changed span,
  live-preview decorations already cover only the viewport, and saves are
  debounced (600 ms). Profiling typing showed the time in CodeMirror's own
  DOM/selection work. The per-keystroke draft copy in localStorage costs
  ~1.4 ms a keystroke on the 5000-line note; throttling it would weaken
  "text is never lost until a save lands", so it stays.
- **Tree virtualisation**: already virtualised (@tanstack/react-virtual).
- **Query invalidation**: change events still refetch tree, inbox, tags,
  views, lists and the touched notes (structural sharing keeps unchanged
  data stable, so the cost is fetches, not renders); narrowing it risks
  stale views on the web, where events carry no paths.
- **Palette**: stays in the main chunk (it must open instantly). The
  remaining per-keystroke commits are cmdk's own store plus
  `useDeferredValue`. Pre-existing quirk, unchanged: right after typing,
  no item is selected, so Enter does nothing until an arrow key.
