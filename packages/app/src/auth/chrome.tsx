import type { ReactNode } from "react";

// The Stroke mark and the "nota" lettering, from assets/brand (the
// paths are the same; the brand files are the source of truth).
const MARK_N = "M7.5 25.5V8.5L20.5 24";
const MARK_TICK =
  "M23.04 24.58L27.58 4.62A1 1 0 0 0 25.67 4.03L18.09 23.04A2.6 2.6 0 1 0 23.04 24.58Z";
const WORDMARK =
  "M4 20V60M4 41a17 17 0 0 1 34 0V60M53 41a17 17 0 1 0 34 0a17 17 0 1 0-34 0M107 8V48a12 12 0 0 0 12 12h3M98 24H121M132 41a17 17 0 1 0 34 0a17 17 0 1 0-34 0M166 20V60";

/** The Nota mark on an ink tile, as in the favicon (assets/brand/favicon.svg). */
export function Mark({ size = 28 }: { size?: number }) {
  return (
    <svg aria-hidden viewBox="0 0 32 32" width={size} height={size} className="shrink-0">
      <rect width="32" height="32" rx="7.2" fill="var(--ink)" />
      <g transform="translate(1.595 2.24) scale(.86)" style={{ color: "var(--ink-foreground)" }}>
        <path
          d={MARK_N}
          fill="none"
          stroke="currentColor"
          strokeWidth="5.2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path d={MARK_TICK} fill="currentColor" />
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
        <path
          d={MARK_N}
          fill="none"
          stroke="currentColor"
          strokeWidth="5.2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path d={MARK_TICK} fill="currentColor" />
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

/**
 * One calm column for everything that happens before the app: the same
 * warm background and hairlines as the vault, one card, no decoration.
 */
export function AuthShell({
  title,
  description,
  children,
  footer,
  width = 380,
}: {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
}) {
  return (
    <div className="flex min-h-full flex-col items-center justify-center bg-background px-5 py-12">
      <div className="flex w-full flex-col items-center" style={{ maxWidth: width }}>
        <a
          href="/app/"
          className="mb-7 flex items-center gap-2 text-foreground outline-none focus-visible:underline"
        >
          <Lockup />
        </a>
        <div className="w-full rounded-xl bg-surface p-7 shadow-card max-[420px]:p-5">
          <h1 className="text-[20px] font-semibold tracking-[-0.015em] text-foreground">{title}</h1>
          {description ? (
            <p className="mt-1.5 text-ui leading-[1.5] text-muted-foreground">{description}</p>
          ) : null}
          <div className="mt-5">{children}</div>
        </div>
        {footer ? (
          <div className="mt-5 w-full text-center text-ui text-muted-foreground">{footer}</div>
        ) : null}
        <p className="mt-8 text-small text-faint">
          Local-first notes in plain Markdown ·{" "}
          <a className="underline-offset-2 hover:text-muted-foreground hover:underline" href="/">
            About Nota
          </a>
        </p>
      </div>
    </div>
  );
}
