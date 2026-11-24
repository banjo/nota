# Nota on a phone

The web app and the MyGo phone shell run the same UI (`packages/app`). Below
720px it is a navigation stack (sidebar, then the page with a back button);
under a coarse pointer every target is at least 44px. Audited at 390×844 and
360×780 (@3x, touch, mobile UA), 844×390 landscape and 1280×800 desktop, light
and dark, with Playwright; the shots live outside the repo.

## Audit (before)

1. No safe areas: `viewport-fit=cover` was set but nothing padded for the
   notch, the home indicator or landscape's sides.
2. Shell height was `100%` (layout viewport): no `dvh`, nothing followed the
   keyboard, which covers the bottom of the editor on iOS.
3. No visible search on the note page: ⌘K was the only way to the palette
   (and to most commands) there.
4. The palette was a desktop modal pinned to the top with 36px rows; its
   field was 15px, so iOS zoomed in on focus.
5. Folder rows showed two icons each and every note row a "…", always: the
   quiet tree turned noisy under a finger.
6. Menus were desktop popovers with 32px items; the note menu opened over
   the title. Dialogs (new/rename vault) sat mid-screen with 32px buttons.
7. Fields under 16px (editor 15px, palette 15px, property, tag, vault and
   capture fields 13–15px): iOS zooms and stays zoomed.
8. 34–36px targets in the top bar (mode switch, "…"), 32px tag chips and
   remove buttons, 28px Settings segments; `Ctrl+K`/`Ctrl+N` hints on a phone.
9. Pull-to-refresh and page rubber band were on; inner scrollers chained.
10. Rows gave no press feedback on touch (hover only); buttons had no
    `touch-action: manipulation`.
11. Toasts at the bottom right, under the home indicator and the keyboard.
12. Reduced motion cut every animation to 1ms instead of fading.

## What changed

- `packages/ui`: `Sheet` (bottom sheet: drag or flick down to close,
  pinned above the keyboard, safe-area padded); dialogs become sheets below
  720px; 44px buttons, inputs, menu and palette items under a coarse pointer;
  safe-area variables (`--safe-*`); `#root` is `100dvh`; 16px fields on touch;
  `overscroll-behavior: none` on the page, `contain` on panes.
- `lib/viewport.ts` writes `--visual-height`/`--keyboard-inset` from
  `visualViewport` (ignoring pinch zoom); the shell shrinks to the visible
  area while the keyboard is up. Viewport meta adds
  `interactive-widget=resizes-content` (Android resizes like iOS).
- Top bar on a phone: back, title, search, "…". The "…" sheet holds the view
  modes (Live/Reading/Source) and the note's actions. The palette is a
  full-height sheet with a Cancel button.
- Tree under a finger: folders get one faint "…"; notes none. A long press on
  any row opens its sheet. Desktop keeps the hover buttons.
- `theme-color` follows the pane on screen (sidebar vs page); toasts drop in
  from the top on a phone.

## Skipped, and why

- Swipe actions on rows: they fight the tree's vertical scroll and the
  virtualizer, and a long press plus "…" already reaches every action.
- Animating the palette: it opens many times a day; instant stays.
- Springs/a motion library: CSS transitions cover the few sheets and panes.
- PWA manifest/standalone mode: not a target yet.

## Needs a real phone

Keyboard overlap and caret visibility (iOS and Android), input zoom, sticky
hover, tap delay, rubber banding, safe areas and the drag-to-close feel do not
reproduce in emulation. Check them on hardware before calling them done.
