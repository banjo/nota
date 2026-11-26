import type { ShareTheme } from "@nota/client";
import { THEME_TOKENS } from "../theme/custom.ts";
import { themeStore } from "../theme/store.ts";

/** A colour the server takes (internal/plugins IsColor): hex, rgb() or hsl(). */
const COLOR =
  /^(#([0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})|(rgb|rgba|hsl|hsla)\([0-9.,%/ deg+-]+\))$/i;

/**
 * The look the app has now, for a shared note's page: every token as the
 * page computes it, in the mode in effect.
 */
export function currentShareTheme(): ShareTheme {
  const style = getComputedStyle(document.documentElement);
  const tokens: Record<string, string> = {};
  for (const t of THEME_TOKENS) {
    const v = style.getPropertyValue(`--${t}`).trim();
    if (COLOR.test(v)) tokens[t] = v;
  }
  return { mode: themeStore().get().resolved, tokens };
}

/** Whether two themes look the same. */
export function sameTheme(a: ShareTheme | undefined, b: ShareTheme | undefined): boolean {
  if ((a?.mode ?? "") !== (b?.mode ?? "")) return false;
  const ta = a?.tokens ?? {};
  const tb = b?.tokens ?? {};
  const ka = Object.keys(ta);
  return ka.length === Object.keys(tb).length && ka.every((k) => ta[k] === tb[k]);
}

/**
 * The look of a printed note: the app's theme when it is light, else
 * Nota's light theme (paper is white; a dark theme would print pale text).
 */
export function printTheme(): ShareTheme {
  const t = currentShareTheme();
  return t.mode === "light" ? t : { mode: "light" };
}
