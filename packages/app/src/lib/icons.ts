import { ICON_COLORS, type IconLook } from "@nota/client";

// Icons and colours of folders and saved views (.nota/folders.yaml, a
// view's icon and color).

/** The CSS colour of an icon colour: the theme's `--icon-<colour>`, ink for accent. */
export function iconColorVar(color?: string): string | undefined {
  if (!color || !(ICON_COLORS as readonly string[]).includes(color)) return undefined;
  return color === "accent" ? "var(--ink)" : `var(--icon-${color})`;
}

/** Just the look of a folder or view: what a row needs to draw it. */
export function lookOf(x: IconLook): IconLook {
  const out: IconLook = {};
  if (x.icon) out.icon = x.icon;
  if (x.color) out.color = x.color;
  if (x.svg) out.svg = x.svg;
  return out;
}

/** `item` with the look it has now in place of the one it had. */
export function withLook<T extends IconLook>(item: T, look: IconLook): T {
  const out = { ...item };
  delete out.icon;
  delete out.color;
  delete out.svg;
  return { ...out, ...lookOf(look) };
}

/** What the icon picker changes: a folder or a saved view. */
export type IconTarget =
  | { kind: "folder"; path: string; name: string }
  | { kind: "view"; id: string; name: string };

/** An icon id that is an emoji rather than "<set>:<name>". */
export const isEmoji = (icon: string | undefined): icon is string => !!icon && !icon.includes(":");

/**
 * Where an arrow key moves in a grid of `count` cells, `cols` wide:
 * Left and Right run on across rows, Up and Down keep the column, Home and
 * End go to the ends. Stays put at an edge; null for other keys.
 */
export function gridMove(index: number, key: string, count: number, cols: number): number | null {
  if (count <= 0) return null;
  const last = count - 1;
  const c = Math.max(1, cols);
  switch (key) {
    case "ArrowLeft":
      return Math.max(0, index - 1);
    case "ArrowRight":
      return Math.min(last, index + 1);
    case "ArrowUp":
      return index - c >= 0 ? index - c : index;
    case "ArrowDown":
      return index + c <= last ? index + c : index;
    case "Home":
      return 0;
    case "End":
      return last;
    default:
      return null;
  }
}
