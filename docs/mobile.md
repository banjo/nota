# Nota on a phone

The web app and the MyGo phone shell run the same UI (`packages/app`). Below
720px it is a navigation stack (sidebar, then the page with a back button);
under a coarse pointer every target is at least 44px. Audited with Playwright
at 390×844 and 360×780 (@3x, touch, mobile UA), 844×390 and 1280×800, light
and dark.

## Audit (before)

1. Safe areas: `viewport-fit=cover` was set, but nothing kept clear of the
   notch, the home indicator or landscape's sides.
2. Shell at `100%` height, no `dvh`; nothing followed the keyboard.
3. No search on the note page: ⌘K was the only way to the palette there.
4. The palette was a desktop modal with 36px rows and a 15px field.
5. Folder rows showed two icons and every note row a "…", always: the
   quiet tree got noisy under a finger.
6. Desktop popovers with 32px items (the note menu covered the title);
   vault dialogs mid-screen with 32px buttons.
7. Fields under 16px (editor, palette, properties, vaults): iOS zooms in.
8. 34–36px targets in the top bar, 32px tag chips, 28px Settings segments;
   `Ctrl+K`/`Ctrl+N` hints on a phone.
9. Pull-to-refresh and page rubber band on; inner scrollers chained.
10. No press feedback on touch (hover only), no `touch-action: manipulation`.
11. Toasts bottom right, under the home indicator and the keyboard.
12. Reduced motion cut every animation to 1ms instead of fading.

## What changed

- `packages/ui`: `Sheet` (bottom sheet above the keyboard, drag or flick
  down to close); dialogs are sheets below 720px; 44px buttons, inputs,
  menu and palette rows under a coarse pointer; `--safe-*` variables;
  `#root` at `100dvh`; 16px fields on touch; no page overscroll.
- `lib/viewport.ts` sets `--visual-height`/`--keyboard-inset` from
  `visualViewport` (not on pinch zoom) and fits the shell to the visible
  area while the keyboard is up; `interactive-widget=resizes-content`.
- Phone top bar: back, title, search, "…"; the "…" sheet holds the view
  modes and the note's actions. The palette is a full-height sheet.
- Tree under a finger: one faint "…" per folder, none on notes; a long press
  on any row opens its sheet. Every action is still a ⌘K command.
- `theme-color` follows the pane on screen; toasts come from the top.
- Motion: sheets in 300ms on the iOS drawer curve, out in 200ms; a dragged
  sheet follows the finger and the overlay lightens with it; panes slide
  16px in the direction you go; reduced motion fades instead.

## Skipped, and why

- Swipe actions on rows: they fight the tree's scroll and virtualizer;
  long press and "…" reach every action.
- Animating the palette: opened many times a day, so it stays instant.
- Springs or a motion library: CSS covers a few sheets and two panes.
- PWA manifest and standalone mode: not a target yet.

## Needs a real phone

Keyboard overlap and caret position, input zoom, sticky hover, tap delay,
rubber banding, safe areas and the drag feel don't reproduce in emulation.
