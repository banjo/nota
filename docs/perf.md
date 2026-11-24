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
