# Nota brand

The source of truth for Nota's logo and colour. Everything else (the
desktop icon, favicons, the landing page's lockup, the README header) is
copied or rendered from here by `render.sh`; edit these files, then run
`sh assets/brand/render.sh` on a machine with `rsvg-convert`.

## The mark

"Stroke": an N drawn in one line, its last leg flicking up into the tick
you make in a margin next to something worth keeping. Nota means a mark.

| File                                   | What                                                                           | Use                                     |
| -------------------------------------- | ------------------------------------------------------------------------------ | --------------------------------------- |
| `icon.svg`                             | 1024 canvas, 824 tile, Forest gradient, white mark, hairline, shadow           | app icon (desktop, stores)              |
| `icon-mono.svg`                        | the same on graphite                                                           | monochrome contexts, press              |
| `favicon.svg`                          | 32 canvas, flat Forest tile, the mark without shadow or hairline, drawn larger | favicons, small UI                      |
| `touch-icon.svg`                       | full-bleed square (the OS rounds it)                                           | `apple-touch-icon.png`                  |
| `mark.svg`                             | the mark alone in `currentColor`                                               | inline in UI, on any background         |
| `lockup.svg`                           | mark in `var(--ink, #256b4d)`, wordmark in `currentColor`                      | inline in pages that have Nota's tokens |
| `lockup-light.svg` / `lockup-dark.svg` | fixed colours for light and dark backgrounds                                   | README, documents, `<img>`              |

The wordmark is lettering drawn as paths (monoline geometric lowercase),
not a font, so it renders the same everywhere.

## Colour: Forest

| Token              | Light                                | Dark                            |
| ------------------ | ------------------------------------ | ------------------------------- |
| icon tile          | `#2f7d5b` → `#236649` (top → bottom) | same                            |
| `--ink`            | `#256b4d` (6.39:1 on white)          | `#5fd3a0` (9.17:1 on `#1c1c1e`) |
| `--ink-soft`       | `#e8f3ee`                            | `rgba(95, 211, 160, .14)`       |
| `--ink-soft-hover` | `#dbede4`                            | `rgba(95, 211, 160, .22)`       |
| `--ink-strong`     | `#1d5a40`                            | `#9fe6c4`                       |
| `--ink-foreground` | `#ffffff`                            | `#15161a`                       |

White on the tile's darkest stop is 6.84:1. The tokens live in
`packages/ui/src/tokens.css`; `packages/app/tests/themes.test.ts` checks
their contrast. The old indigo is still offered as the "Indigo" palette.

## Clear space and sizes

- Keep clear space around the mark of at least the stroke's width (in the
  32 grid: 5.2 units); around the lockup, half the mark's height.
- The tile icon down to 32 px; below that (16–24 px) use `favicon.svg`.
- The mark alone down to 12 px; the lockup down to 18 px tall, where the
  wordmark's stroke is still a full pixel.
- Don't recolour the mark inside the tile, add effects, outline, rotate or
  stretch it, or set "nota" in a font next to it.
