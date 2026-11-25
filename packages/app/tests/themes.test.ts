import { describe, expect, test } from "vite-plus/test";
import { readFileSync } from "node:fs";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { createRegistry } from "../src/registry.ts";
import { CLASSIC_THEMES } from "../src/theme/classics.ts";
import { contrastIssues, mix } from "../src/theme/custom.ts";
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

/** The colours code is drawn in, with the fallbacks the editor uses. */
function syntaxColors(v: Record<string, string>): Record<string, string | undefined> {
  return {
    keyword: v["--md-syntax-keyword"] ?? v["--ink-strong"],
    string: v["--md-syntax-string"],
    number: v["--md-syntax-number"],
    comment: v["--md-syntax-comment"] ?? v["--muted-foreground"],
    function: v["--md-syntax-function"] ?? v["--ink"],
    type: v["--md-syntax-type"] ?? v["--md-tag"] ?? v["--ink-strong"],
  };
}

/** Strings and numbers ≥ 4.5:1 on code; the accents, like ink, ≥ 3:1. */
function expectReadableCode(v: Record<string, string>) {
  for (const [k, c] of Object.entries(syntaxColors(v))) {
    expect(c, k).toBeDefined();
    const min = k === "string" || k === "number" ? 4.5 : 3;
    expect(contrast(c!, v["--code"]!), k).toBeGreaterThanOrEqual(min);
  }
}

/**
 * Callout titles and icons are accents on their own tint of the page:
 * ≥ 3:1. Status colours a theme doesn't set come from tokens.css.
 */
function expectReadableCallouts(v: Record<string, string>) {
  const danger = v["--danger"] ?? block(tokensCss, ":root")["--danger"]!;
  const colors = {
    note: v["--md-callout-note"] ?? v["--ink"],
    tip: v["--md-callout-tip"] ?? v["--md-syntax-string"],
    important: v["--md-callout-important"] ?? v["--ink-strong"],
    warning: v["--md-callout-warning"] ?? v["--md-syntax-number"],
    caution: v["--md-callout-caution"] ?? danger,
  };
  for (const [k, c] of Object.entries(colors)) {
    expect(c, k).toBeDefined();
    const tint = mix(c!, v["--background"]!, 0.08);
    expect(contrast(c!, tint), `callout ${k}`).toBeGreaterThanOrEqual(3);
  }
}

/** Body text on a ==highlight==: ≥ 4.5:1 on warning at 30% over the page. */
function expectReadableHighlight(v: Record<string, string>) {
  const warning = v["--warning"] ?? block(tokensCss, ":root")["--warning"]!;
  const bg = v["--md-highlight"] ?? mix(warning, v["--background"]!, 0.3);
  expect(contrast(v["--text"]!, bg), "highlight").toBeGreaterThanOrEqual(4.5);
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
    expectReadableCode(vars);
    expectReadableCallouts(vars);
    expectReadableHighlight(vars);
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
      // Table headers are body text on their own background.
      const header = tk["--md-table-header"] ?? tk["--code"]!;
      const headerText = tk["--foreground"]!;
      expect(contrast(headerText, header), "table header").toBeGreaterThanOrEqual(4.5);
      expectReadableCode(tk);
      expectReadableCallouts(tk);
      expectReadableHighlight(tk);
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
