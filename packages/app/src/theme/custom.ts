import type { PluginInfo, ThemeTokens } from "@nota/client";
import { type ColorTheme, contrast, luminance, type ThemeColors } from "./themes.ts";

// Themes made of tokens: the classics Nota ships (theme/classics.ts) and
// the ones plugins bring (.nota/plugins, docs/themes.md). A theme gives
// background, text and ink per mode and may set any other token; the
// rest is derived from those three, so a handful of colours makes a
// whole theme.

/**
 * The tokens a theme may set, as in packages/ui/src/tokens.css without
 * the "--". The md-* tokens colour Markdown in the editor and fall back
 * to the others. internal/plugins keeps the same list.
 */
export const THEME_TOKENS = [
  "background",
  "foreground",
  "text",
  "text-soft",
  "muted-foreground",
  "faint",
  "sidebar",
  "hairline",
  "divider",
  "hover",
  "selected",
  "chip",
  "chip-foreground",
  "file",
  "code",
  "surface",
  "overlay",
  "ink",
  "ink-soft",
  "ink-soft-hover",
  "ink-strong",
  "ink-foreground",
  "success",
  "warning",
  "danger",
  "icon-red",
  "icon-orange",
  "icon-yellow",
  "icon-green",
  "icon-blue",
  "icon-purple",
  "icon-pink",
  "icon-gray",
  "md-heading",
  "md-bold",
  "md-italic",
  "md-link",
  "md-tag",
  "md-code",
  "md-quote",
  "md-table-border",
  "md-table-header",
  "md-syntax-keyword",
  "md-syntax-string",
  "md-syntax-number",
  "md-syntax-comment",
  "md-syntax-function",
  "md-syntax-type",
  "md-callout-note",
  "md-callout-tip",
  "md-callout-important",
  "md-callout-warning",
  "md-callout-caution",
  "md-highlight",
] as const;

export type ThemeToken = (typeof THEME_TOKENS)[number];

/** One mode of a theme: background, text and ink, and any other token. */
export type TokenInput = { background: string; text: string; ink: string } & Partial<
  Record<ThemeToken, string>
>;

export interface ThemeSpec {
  id: string;
  name: string;
  order?: number;
  group?: string;
  light?: TokenInput;
  dark?: TokenInput;
}

type Rgb = [number, number, number];

const HEX = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i;

function parseHex(hex: string): Rgb | undefined {
  if (!HEX.test(hex)) {
    return undefined;
  }
  const h =
    hex.length === 4
      ? hex
          .slice(1)
          .split("")
          .map((c) => c + c)
          .join("")
      : hex.slice(1);
  const n = Number.parseInt(h, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

const toHex = (c: Rgb) =>
  `#${c
    .map((x) =>
      Math.round(Math.min(255, Math.max(0, x)))
        .toString(16)
        .padStart(2, "0"),
    )
    .join("")}`;

/** #rrggbb for a #rgb or #rrggbb colour. */
const longHex = (hex: string) => toHex(parseHex(hex)!);

/** `weight` of a and the rest of b, in sRGB. Both are hex colours. */
export function mix(a: string, b: string, weight: number): string {
  const x = parseHex(a)!;
  const y = parseHex(b)!;
  return toHex([0, 1, 2].map((i) => x[i]! * weight + y[i]! * (1 - weight)) as Rgb);
}

/** The colour between text and bg closest to bg that still has `min` contrast on bg. */
function readable(text: string, bg: string, weight: number, min: number): string {
  for (let w = weight; w < 1; w += 0.02) {
    const c = mix(text, bg, w);
    if (contrast(c, bg) >= min) {
      return c;
    }
  }
  return longHex(text);
}

/** color, moved toward `toward` until it has `min` contrast on bg. */
function legible(color: string, toward: string, bg: string, min: number): string {
  for (let w = 1; w > 0; w -= 0.02) {
    const c = mix(color, toward, w);
    if (contrast(c, bg) >= min) {
      return c;
    }
  }
  return longHex(toward);
}

/**
 * warning at 30%, fainter until text reads on it at 4.5:1. Over white on
 * a light page, so a greyish page doesn't darken the marker.
 */
function highlight(warning: string, text: string, bg: string, dark: boolean): string {
  const under = dark ? bg : "#ffffff";
  for (let w = 0.3; w > 0.06; w -= 0.02) {
    const c = mix(warning, under, w);
    if (contrast(text, c) >= 4.5) {
      return c;
    }
  }
  return mix(warning, under, 0.06);
}

// The status colours of tokens.css, for themes that don't set their own.
const SUCCESS = "#37b26c";
const WARNING = "#e2a93b";
const DANGER = "#d4483b";

/** Every token of one mode: what the theme sets, and the rest derived. */
export function deriveTokens(input: TokenInput): Partial<Record<ThemeToken, string>> {
  const bg = longHex(input.background);
  const text = longHex(input.text);
  const ink = longHex(input.ink);
  const dark = luminance(bg) < luminance(text);
  const toBg = (w: number) => mix(text, bg, w);
  const code = toBg(dark ? 0.07 : 0.055);
  const hex = (v: string | undefined, fallback: string) => (v && parseHex(v) ? v : fallback);
  const codeBg = hex(input.code, code);
  const extreme = dark ? "#ffffff" : "#000000";
  const inkStrong = mix(ink, text, 0.7);
  const muted = readable(text, bg, 0.6, 4.5);
  const inkForeground =
    [dark ? bg : "#ffffff", dark ? "#ffffff" : bg, "#ffffff", "#000000"].find(
      (c) => contrast(c, ink) >= 4.5,
    ) ?? (contrast("#ffffff", ink) > contrast("#000000", ink) ? "#ffffff" : "#000000");
  const derived: Partial<Record<ThemeToken, string>> = {
    background: bg,
    text,
    ink,
    foreground: mix(text, dark ? "#ffffff" : "#000000", 0.6),
    "text-soft": toBg(0.88),
    "muted-foreground": muted,
    faint: readable(text, bg, 0.4, 2.2),
    sidebar: toBg(0.035),
    hairline: toBg(0.11),
    divider: toBg(0.075),
    hover: toBg(0.075),
    selected: toBg(0.11),
    chip: toBg(0.08),
    "chip-foreground": toBg(0.8),
    file: toBg(0.78),
    code,
    surface: dark ? toBg(0.045) : bg,
    overlay: dark ? "rgba(0, 0, 0, 0.5)" : "rgba(20, 21, 24, 0.28)",
    "ink-soft": mix(ink, bg, 0.14),
    "ink-soft-hover": mix(ink, bg, 0.22),
    "ink-strong": inkStrong,
    "ink-foreground": inkForeground,
    // Code colours, each moved toward black or white until it reads on
    // code: strings and numbers like body text, the accents like ink.
    "md-syntax-keyword": legible(hex(input["ink-strong"], inkStrong), extreme, codeBg, 3),
    "md-syntax-string": legible(hex(input.success, SUCCESS), extreme, codeBg, 4.5),
    "md-syntax-number": legible(hex(input.warning, WARNING), extreme, codeBg, 4.5),
    "md-syntax-comment": legible(hex(input["muted-foreground"], muted), extreme, codeBg, 3),
    "md-syntax-function": legible(ink, extreme, codeBg, 3),
    "md-syntax-type": legible(
      hex(input["md-tag"], hex(input["ink-strong"], inkStrong)),
      extreme,
      codeBg,
      3,
    ),
  };
  // Callouts are accents on an 8% tint of their own colour over the page:
  // ≥ 3:1 there, which 3.4:1 on the page leaves room for.
  const callout = (c: string) => legible(c, extreme, bg, 3.4);
  Object.assign(derived, {
    "md-callout-note": callout(ink),
    "md-callout-tip": callout(hex(input["md-syntax-string"], derived["md-syntax-string"]!)),
    "md-callout-important": callout(hex(input["ink-strong"], inkStrong)),
    "md-callout-warning": callout(hex(input["md-syntax-number"], derived["md-syntax-number"]!)),
    "md-callout-caution": callout(hex(input.danger, DANGER)),
    "md-highlight": highlight(hex(input.warning, WARNING), text, bg, dark),
  });
  const out: Partial<Record<ThemeToken, string>> = { ...derived };
  for (const k of THEME_TOKENS) {
    const v = input[k];
    if (v) {
      out[k] = k === "background" || k === "text" || k === "ink" ? longHex(v) : v;
    }
  }
  return out;
}

const cssTokens = (t: Partial<Record<ThemeToken, string>>) =>
  Object.fromEntries(Object.entries(t).map(([k, v]) => [`--${k}`, v]));

const swatch = (t: Partial<Record<ThemeToken, string>>): ThemeColors => ({
  background: t.background!,
  sidebar: t.sidebar!,
  text: t.text!,
  ink: t.ink!,
  inkForeground: t["ink-foreground"]!,
});

/**
 * A ColorTheme from tokens. A theme with one mode shows that mode's
 * swatch in both, and choosing it switches the app to that mode.
 */
export function themeFromSpec(spec: ThemeSpec): ColorTheme {
  const light = spec.light && deriveTokens(spec.light);
  const dark = spec.dark && deriveTokens(spec.dark);
  const either = (light ?? dark)!;
  let only: ColorTheme["only"];
  if (!light) {
    only = "dark";
  } else if (!dark) {
    only = "light";
  }
  return {
    id: spec.id,
    name: spec.name,
    order: spec.order,
    group: spec.group,
    only,
    light: swatch(light ?? either),
    dark: swatch(dark ?? either),
    tokens: {
      light: cssTokens(light ?? either),
      dark: cssTokens(dark ?? either),
    },
  };
}

/** The group plugin themes are listed under in Settings. */
export const VAULT_GROUP = "This vault";

/** The themes of the vault's plugins, as ColorThemes with ids "<plugin>.<theme>". */
export function pluginThemes(plugins: PluginInfo[]): ColorTheme[] {
  return plugins
    .filter((p) => !p.error)
    .flatMap((p) =>
      p.contributes.themes.map((t) =>
        themeFromSpec({
          id: `${p.id}.${t.id}`,
          name: t.name,
          order: 1000,
          group: VAULT_GROUP,
          light: asInput(t.light),
          dark: asInput(t.dark),
        }),
      ),
    );
}

function asInput(t: ThemeTokens | undefined): TokenInput | undefined {
  if (!t || !t.background || !t.text || !t.ink) {
    return undefined;
  }
  return t as TokenInput;
}

/** Readability problems of a theme, worded for its author; none for a good theme. */
export function contrastIssues(theme: ColorTheme): string[] {
  const out: string[] = [];
  const modes = theme.only ? [theme.only] : (["light", "dark"] as const);
  for (const mode of modes) {
    const c = theme[mode];
    const check = (what: string, a: string, b: string, min: number) => {
      if (!HEX.test(a) || !HEX.test(b)) {
        return;
      }
      const r = contrast(longHex(a), longHex(b));
      if (r < min) {
        out.push(`${mode}: ${what} is ${r.toFixed(1)}:1, under ${min}:1`);
      }
    };
    check("text on background", c.text, c.background, 4.5);
    check("ink on background", c.ink, c.background, 3);
    check("ink on sidebar", c.ink, c.sidebar, 3);
    check("ink-foreground on ink", c.inkForeground, c.ink, 4.5);
  }
  return out;
}
