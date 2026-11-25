import type { PluginInfo } from "@nota/client";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { describe, expect, test } from "vite-plus/test";
import { createRegistry } from "../src/registry.ts";
import {
  contrastIssues,
  deriveTokens,
  mix,
  pluginThemes,
  THEME_TOKENS,
  themeFromSpec,
  VAULT_GROUP,
} from "../src/theme/custom.ts";
import { chooseTheme, registerThemes } from "../src/theme/register.ts";
import { themeStore } from "../src/theme/store.ts";
import { contrast } from "../src/theme/themes.ts";

const night: PluginInfo = {
  id: "night",
  name: "Night",
  dir: ".nota/plugins/night",
  contributes: {
    themes: [
      {
        id: "owl",
        name: "Owl",
        dark: { background: "#011627", text: "#d6deeb", ink: "#82aaff", "md-tag": "#c792ea" },
      },
      {
        id: "dim",
        name: "Dim",
        light: { background: "#f0f0f0", text: "#999999", ink: "#bbbbbb" },
        dark: { background: "#222", text: "#ddd", ink: "#8be9fd" },
      },
    ],
  },
};

describe("themes made of tokens", () => {
  test("three colours make every token, readable", () => {
    const t = deriveTokens({ background: "#fff", text: "#2a2c31", ink: "#1e7363" });
    expect(Object.keys(t).sort()).toEqual(
      THEME_TOKENS.filter(
        (k) => !/^(success|warning|danger|md-(?!syntax-|callout-|highlight))/.test(k),
      ).sort(),
    );
    expect(t.background).toBe("#ffffff");
    expect(contrast(t["muted-foreground"]!, "#ffffff")).toBeGreaterThanOrEqual(4.5);
    expect(t["ink-foreground"]).toBe("#ffffff");
    // Light: the sidebar is a touch darker than the page.
    expect(contrast(t.sidebar!, "#ffffff")).toBeGreaterThan(1);
    expect(contrast(t.sidebar!, "#ffffff")).toBeLessThan(1.15);
  });

  test("a dark theme gets dark button labels on a light ink, and its own tokens win", () => {
    const t = deriveTokens({
      background: "#282a36",
      text: "#f8f8f2",
      ink: "#bd93f9",
      sidebar: "#21222c",
      "md-link": "#8be9fd",
    });
    expect(t["ink-foreground"]).toBe("#282a36");
    expect(t.sidebar).toBe("#21222c");
    expect(t["md-link"]).toBe("#8be9fd");
    expect(t.overlay).toBe("rgba(0, 0, 0, 0.5)");
  });

  test("mix blends in sRGB", () => {
    expect(mix("#000000", "#ffffff", 0.5)).toBe("#808080");
    expect(mix("#ff0000", "#0000ff", 1)).toBe("#ff0000");
  });

  test("a theme with one mode uses it for both swatches and asks for that mode", () => {
    const t = themeFromSpec({
      id: "x",
      name: "X",
      dark: { background: "#000000", text: "#ffffff", ink: "#8be9fd" },
    });
    expect(t.only).toBe("dark");
    expect(t.light).toEqual(t.dark);
    expect(t.tokens?.light).toEqual(t.tokens?.dark);
    expect(t.tokens?.dark?.["--background"]).toBe("#000000");
  });

  test("plugin themes are namespaced, grouped and skip broken plugins", () => {
    const broken: PluginInfo = {
      id: "broken",
      name: "broken",
      dir: ".nota/plugins/broken",
      contributes: { themes: [] },
      error: "manifest.json: unexpected end of JSON input",
    };
    const ts = pluginThemes([night, broken]);
    expect(ts.map((t) => t.id)).toEqual(["night.owl", "night.dim"]);
    expect(ts.every((t) => t.group === VAULT_GROUP && t.order === 1000)).toBe(true);
    expect(ts[0]!.only).toBe("dark");
    expect(ts[0]!.tokens?.dark?.["--md-tag"]).toBe("#c792ea");
    for (const t of ts) expect(t.id).toMatch(/^[a-z0-9][a-z0-9.-]*$/);
  });

  test("hard-to-read themes are reported to their author", () => {
    const [owl, dim] = pluginThemes([night]);
    expect(contrastIssues(owl!)).toEqual([]);
    const issues = contrastIssues(dim!);
    expect(issues).toContain("light: text on background is 2.5:1, under 4.5:1");
    expect(issues.some((i) => i.startsWith("light: ink on background"))).toBe(true);
    expect(issues.some((i) => i.startsWith("dark:"))).toBe(false);
  });

  test("plugin themes register with their commands and leave again", () => {
    const r = createRegistry();
    const off = registerThemes(r, pluginThemes([night]));
    expect(r.get("theme").map((t) => t.id)).toEqual(["night.owl", "night.dim"]);
    expect(r.command("theme.palette.night.owl")?.title).toBe("Theme: Owl");
    off();
    expect(r.get("theme")).toEqual([]);
    expect(r.command("theme.palette.night.owl")).toBeUndefined();
  });

  test("choosing a dark-only theme switches to dark", () => {
    const store = themeStore();
    store.setMode("light");
    chooseTheme(pluginThemes([night])[0]!);
    expect(store.get()).toMatchObject({ palette: "night.owl", mode: "dark", resolved: "dark" });
    chooseTheme(pluginThemes([night])[1]!);
    expect(store.get()).toMatchObject({ palette: "night.dim", mode: "dark" });
  });
});

/** Runs an index.html's pre-paint script against a fake page. */
function prePaint(app: "desktop" | "web", stored: Record<string, string>, systemDark = false) {
  const html = readFileSync(new URL(`../../../apps/${app}/index.html`, import.meta.url), "utf8");
  const script = html.match(/<script>([\s\S]*?)<\/script>/)![1]!;
  const props = new Map<string, string>();
  const documentElement = {
    dataset: {} as Record<string, string>,
    style: { setProperty: (k: string, v: string) => void props.set(k, v) },
  };
  const localStorage = { getItem: (k: string) => stored[k] ?? null };
  const matchMedia = () => ({ matches: systemDark });
  runInNewContext(script, { document: { documentElement }, localStorage, matchMedia });
  return { dataset: documentElement.dataset, props };
}

describe.each(["desktop", "web"] as const)("%s pre-paint script", (app) => {
  const cache = JSON.stringify({
    id: "night.owl",
    tokens: {
      light: { "--background": "#ffffff" },
      dark: { "--background": "#011627", "--ink": "#82aaff", "bad;": "x" },
    },
  });

  test("sets the cached tokens of the chosen theme for the mode in effect", () => {
    const r = prePaint(app, { "nota:palette": "night.owl", "nota:palette-tokens": cache }, true);
    expect(r.dataset).toMatchObject({ mode: "dark", palette: "night.owl" });
    expect(Object.fromEntries(r.props)).toEqual({ "--background": "#011627", "--ink": "#82aaff" });
  });

  test("ignores a cache for another theme, and a broken one", () => {
    expect(
      prePaint(app, { "nota:palette": "paper", "nota:palette-tokens": cache }).props.size,
    ).toBe(0);
    const broken = prePaint(app, { "nota:palette": "night.owl", "nota:palette-tokens": "{" });
    expect(broken.props.size).toBe(0);
    expect(broken.dataset.palette).toBe("night.owl");
  });
});
