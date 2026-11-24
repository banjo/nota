import { describe, expect, test } from "vite-plus/test";
import { readFileSync } from "node:fs";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { createRegistry } from "../src/registry.ts";
import { CLASSIC_THEMES } from "../src/theme/classics.ts";
import { contrastIssues } from "../src/theme/custom.ts";
import { BUILTIN_THEMES, type ColorTheme, contrast } from "../src/theme/themes.ts";

// Read from disk: the test runner turns CSS imports into empty modules.
const css = (name: string) =>
  readFileSync(new URL(`../../ui/src/${name}`, import.meta.url), "utf8");
const tokensCss = css("tokens.css");
const palettesCss = css("palettes.css");

/** The custom properties of the first rule whose selector is exactly `selector`. */
function block(css: string, selector: string): Record<string, string> {
  const at = css.indexOf(`${selector} {`);
  if (at < 0) return {};
  const body = css.slice(css.indexOf("{", at) + 1, css.indexOf("}", at));
  const out: Record<string, string> = {};
  for (const m of body.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out[m[1]!] = m[2]!.trim();
  return out;
}

/** What a theme's CSS resolves to in a mode: tokens.css, then its palette rule. */
function cssColors(id: string, mode: "light" | "dark") {
  const base =
    mode === "light"
      ? block(tokensCss, ":root")
      : block(tokensCss, ':root[data-theme="dark"],\n:root[data-mode="dark"]');
  const sel =
    mode === "light"
      ? `:root[data-palette="${id}"]:not([data-mode="dark"])`
      : `:root[data-palette="${id}"][data-mode="dark"]`;
  return { ...base, ...(id === "nota" ? {} : block(palettesCss, sel)) };
}

describe("colour themes", () => {
  const cases: [string, "light" | "dark"][] = BUILTIN_THEMES.flatMap((t) => [
    [t.id, "light"],
    [t.id, "dark"],
  ]);
  test.each(cases)("%s %s is readable and matches its CSS", (id, mode) => {
    const theme = BUILTIN_THEMES.find((t) => t.id === id)!;
    const c = theme[mode];
    const vars = cssColors(id, mode);
    // The swatch draws what the CSS paints.
    expect(vars["--background"]).toBe(c.background);
    expect(vars["--sidebar"]).toBe(c.sidebar);
    expect(vars["--text"]).toBe(c.text);
    expect(vars["--ink"]).toBe(c.ink);
    expect(vars["--ink-foreground"]).toBe(c.inkForeground);
    // Body text ≥ 4.5:1 (AA); ink, as links and accents, ≥ 3:1 on both
    // the page and the sidebar; button labels on ink ≥ 4.5:1.
    expect(contrast(c.text, c.background)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(c.ink, c.background)).toBeGreaterThanOrEqual(3);
    expect(contrast(c.ink, c.sidebar)).toBeGreaterThanOrEqual(3);
    expect(contrast(c.inkForeground, c.ink)).toBeGreaterThanOrEqual(4.5);
    // Muted labels too, where the theme sets them.
    if (vars["--muted-foreground"]?.startsWith("#"))
      expect(contrast(vars["--muted-foreground"], c.background)).toBeGreaterThanOrEqual(4.5);
  });

  test.each(CLASSIC_THEMES.flatMap((t) => [[t.id, "light"] as const, [t.id, "dark"] as const]))(
    "classic %s %s is readable",
    (id, mode) => {
      const theme = CLASSIC_THEMES.find((t) => t.id === id)!;
      expect(contrastIssues(theme)).toEqual([]);
      const tk = theme.tokens![mode]!;
      const c = theme[mode];
      // The swatch draws what the tokens paint.
      expect(tk["--background"]).toBe(c.background);
      expect(tk["--sidebar"]).toBe(c.sidebar);
      expect(tk["--ink"]).toBe(c.ink);
      expect(contrast(tk["--muted-foreground"]!, c.background)).toBeGreaterThanOrEqual(4.5);
      // Markdown colours are accents, like ink: ≥ 3:1 where they are drawn.
      for (const k of ["heading", "bold", "italic", "link", "tag", "quote"]) {
        const v = tk[`--md-${k}`];
        if (v) expect(contrast(v, c.background), k).toBeGreaterThanOrEqual(3);
      }
      if (tk["--md-code"])
        expect(contrast(tk["--md-code"], tk["--code"]!)).toBeGreaterThanOrEqual(3);
    },
  );

  test("classics have both modes, unique ids and a group", () => {
    const ids = [...BUILTIN_THEMES, ...CLASSIC_THEMES].map((t) => t.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(CLASSIC_THEMES.length).toBeGreaterThanOrEqual(5);
    expect(CLASSIC_THEMES.length).toBeLessThanOrEqual(10);
    for (const t of CLASSIC_THEMES) {
      expect(t.only).toBeUndefined();
      expect(t.group).toBe("Classics");
    }
    const dracula = CLASSIC_THEMES.find((t) => t.id === "dracula")!;
    expect(dracula.dark.background).toBe("#282a36");
    expect(dracula.tokens?.dark?.["--md-tag"]).toBe("#ff79c6");
  });

  test("contrast is WCAG's", () => {
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21);
    expect(contrast("#ffffff", "#ffffff")).toBe(1);
    expect(contrast("#4256d6", "#ffffff")).toBeCloseTo(5.94, 1);
    expect(contrast("#1e7363", "#ffffff")).toBeCloseTo(5.69, 1);
  });

  test("the default palette is the brand's Jade", () => {
    expect(BUILTIN_THEMES[0]!.id).toBe("nota");
    expect(BUILTIN_THEMES[0]!.light.ink).toBe("#1e7363");
    expect(BUILTIN_THEMES[0]!.dark.ink).toBe("#5fd9c4");
    expect(BUILTIN_THEMES.find((t) => t.id === "jade")).toBeUndefined();
    expect(BUILTIN_THEMES.find((t) => t.id === "forest")?.light.ink).toBe("#256b4d");
  });

  test("ids are valid data-palette values and unique", () => {
    const ids = BUILTIN_THEMES.map((t) => t.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ids) expect(id).toMatch(/^[a-z0-9][a-z0-9.-]*$/);
  });

  test("themes are registry contributions, sorted, and a plugin can add one", () => {
    const r = createRegistry();
    const off = registerBuiltins(r);
    const ids = () => r.get("theme").map((t) => t.id);
    const classics = CLASSIC_THEMES.map((t) => t.id);
    expect(ids()).toEqual(["nota", "indigo", "graphite", "paper", "forest", "rose", ...classics]);
    const mine: ColorTheme = {
      id: "plugin.sea",
      name: "Sea",
      order: 25,
      light: { ...BUILTIN_THEMES[0]!.light, ink: "#1d6f8a" },
      dark: { ...BUILTIN_THEMES[0]!.dark, ink: "#7cc4dc" },
      tokens: { light: { "--ink": "#1d6f8a" }, dark: { "--ink": "#7cc4dc" } },
    };
    const offMine = r.register("theme", mine);
    expect(ids()).toEqual([
      "nota",
      "indigo",
      "graphite",
      "paper",
      "plugin.sea",
      "forest",
      "rose",
      ...classics,
    ]);
    offMine();
    expect(r.get("theme")).toHaveLength(6 + classics.length);
    // ⌘K: one toggle and a command per theme.
    expect(r.command("theme.toggleDark")?.title).toBe("Toggle dark mode");
    expect(r.command("theme.toggleDark")?.shortcut).toBeUndefined();
    expect(r.command("theme.palette.paper")?.title).toBe("Theme: Paper");
    expect(r.command("theme.palette.dracula")?.title).toBe("Theme: Dracula");
    off();
    expect(r.get("theme")).toHaveLength(0);
  });
});
