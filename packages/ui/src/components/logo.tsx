import { cn } from "../lib/utils.ts";

// The Stroke logo: a one-line N ending in a margin tick. The paths are
// copied from assets/brand (mark.svg, favicon.svg, lockup.svg), which stay
// the source of truth; every place the app draws the logo uses these
// components, painted with tokens so palettes and dark mode follow.

export const MARK_N = "M7.5 25.5V8.5L20.5 24";
export const MARK_TICK =
  "M23.04 24.58L27.58 4.62A1 1 0 0 0 25.67 4.03L18.09 23.04A2.6 2.6 0 1 0 23.04 24.58Z";
const WORDMARK =
  "M4 20V60M4 41a17 17 0 0 1 34 0V60M53 41a17 17 0 1 0 34 0a17 17 0 1 0-34 0M107 8V48a12 12 0 0 0 12 12h3M98 24H121M132 41a17 17 0 1 0 34 0a17 17 0 1 0-34 0M166 20V60";

/** The two strokes in the 32×32 mark grid, in currentColor. */
function Strokes() {
  return (
    <>
      <path
        d={MARK_N}
        fill="none"
        stroke="currentColor"
        strokeWidth="5.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path d={MARK_TICK} fill="currentColor" />
    </>
  );
}

/** The bare mark (assets/brand/mark.svg) in currentColor; the ink by default. */
export function Mark({ size = 20, className }: { size?: number; className?: string }) {
  return (
    <svg
      aria-hidden
      viewBox="0 0 32 32"
      width={size}
      height={size}
      className={cn("shrink-0 text-ink", className)}
    >
      <Strokes />
    </svg>
  );
}

/** The mark on an ink tile, as in the favicon (assets/brand/favicon.svg). */
export function MarkTile({ size = 28, className }: { size?: number; className?: string }) {
  return (
    <svg
      aria-hidden
      viewBox="0 0 32 32"
      width={size}
      height={size}
      className={cn("shrink-0", className)}
    >
      <rect width="32" height="32" rx="7.2" fill="var(--ink)" />
      <g transform="translate(1.595 2.24) scale(.86)" style={{ color: "var(--ink-foreground)" }}>
        <Strokes />
      </g>
    </svg>
  );
}

/** The lockup: the mark in the ink, "nota" in the text colour (assets/brand/lockup.svg). */
export function Lockup({ height = 26 }: { height?: number }) {
  return (
    <svg
      role="img"
      aria-label="Nota"
      viewBox="0 0 216 64"
      height={height}
      width={(height * 216) / 64}
    >
      <g transform="scale(2)" style={{ color: "var(--ink)" }}>
        <Strokes />
      </g>
      <path
        transform="translate(78 7) scale(.8)"
        d={WORDMARK}
        fill="none"
        stroke="currentColor"
        strokeWidth="8"
      />
    </svg>
  );
}
