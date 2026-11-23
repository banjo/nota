// The colour themes Nota ships. Their tokens live in CSS
// (packages/ui/src/palettes.css) so the pre-paint script in index.html
// can apply them before React runs; this list is what the UI offers,
// with the colours its swatches draw. registerBuiltins registers each as
// a "theme" contribution, the same way a plugin would add its own (a
// plugin theme carries its tokens instead of CSS).

/** The colours a swatch shows, and what the contrast test checks. */
export interface ThemeColors {
  background: string;
  sidebar: string;
  /** Body copy (--text). */
  text: string;
  ink: string;
  /** Text on ink (primary buttons). */
  inkForeground: string;
}

export interface ColorTheme {
  /** data-palette value: lowercase letters, digits, dots and dashes. */
  id: string;
  name: string;
  /** Lower first. */
  order?: number;
  light: ThemeColors;
  dark: ThemeColors;
  /**
   * CSS custom properties to set on <html> while the theme is chosen,
   * per mode ({"--ink": "#…"}). Built-in themes leave it out: theirs are
   * in palettes.css.
   */
  tokens?: { light?: Record<string, string>; dark?: Record<string, string> };
}

const neutral = {
  light: { background: "#ffffff", sidebar: "#f7f7f5", text: "#2a2c31" },
  dark: { background: "#1c1c1e", sidebar: "#232325", text: "#d9d8d4" },
};

export const BUILTIN_THEMES: ColorTheme[] = [
  {
    id: "nota",
    name: "Nota",
    order: 0,
    light: { ...neutral.light, ink: "#256b4d", inkForeground: "#ffffff" },
    dark: { ...neutral.dark, ink: "#5fd3a0", inkForeground: "#15161a" },
  },
  {
    id: "indigo",
    name: "Indigo",
    order: 5,
    light: { ...neutral.light, ink: "#4256d6", inkForeground: "#ffffff" },
    dark: { ...neutral.dark, ink: "#8b9bff", inkForeground: "#15161a" },
  },
  {
    id: "graphite",
    name: "Graphite",
    order: 10,
    light: {
      background: "#ffffff",
      sidebar: "#f6f6f6",
      text: "#2a2a2c",
      ink: "#18181b",
      inkForeground: "#ffffff",
    },
    dark: {
      background: "#171718",
      sidebar: "#1e1e20",
      text: "#dcdcdc",
      ink: "#f4f4f5",
      inkForeground: "#171718",
    },
  },
  {
    id: "paper",
    name: "Paper",
    order: 20,
    light: {
      background: "#f8f4ec",
      sidebar: "#f1ebdf",
      text: "#3b3026",
      ink: "#8a5a2b",
      inkForeground: "#ffffff",
    },
    dark: {
      background: "#221e1a",
      sidebar: "#29241f",
      text: "#e0d5c6",
      ink: "#d9a46a",
      inkForeground: "#221e1a",
    },
  },
  {
    id: "jade",
    name: "Jade",
    order: 30,
    light: { ...neutral.light, ink: "#1e7363", inkForeground: "#ffffff" },
    dark: { ...neutral.dark, ink: "#5fd9c4", inkForeground: "#15161a" },
  },
  {
    id: "rose",
    name: "Rose",
    order: 40,
    light: { ...neutral.light, ink: "#b03a62", inkForeground: "#ffffff" },
    dark: { ...neutral.dark, ink: "#f08fb0", inkForeground: "#15161a" },
  },
];

/** WCAG relative luminance of a #rrggbb colour. */
export function luminance(hex: string): number {
  const n = Number.parseInt(hex.slice(1), 16);
  const ch = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * ch[0]! + 0.7152 * ch[1]! + 0.0722 * ch[2]!;
}

/** WCAG contrast ratio of two #rrggbb colours, 1 to 21. */
export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi! + 0.05) / (lo! + 0.05);
}
