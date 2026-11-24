import { Palette } from "lucide-react";
import { useEffect, useMemo } from "react";
import { usePlugins } from "../queries.ts";
import { type Registry, useRegistry } from "../registry.ts";
import { pluginThemes } from "./custom.ts";
import { themeStore } from "./store.ts";
import type { ColorTheme } from "./themes.ts";

/** Picks a theme; one with a single mode switches the app to that mode too. */
export function chooseTheme(theme: ColorTheme) {
  const store = themeStore();
  store.setPalette(theme.id);
  if (theme.only && store.get().resolved !== theme.only) {
    store.setMode(theme.only);
  }
}

/** Adds a theme to Settings and a "Theme: …" command to ⌘K; returns the disposer. */
export function registerTheme(r: Registry, theme: ColorTheme): () => void {
  const off = [
    r.register("theme", theme),
    r.register("command", {
      id: `theme.palette.${theme.id}`,
      title: `Theme: ${theme.name}`,
      group: "View",
      keywords: ["colour", "color", "palette", "appearance"],
      icon: Palette,
      run: () => chooseTheme(theme),
    }),
  ];
  return () => {
    for (const f of off) {
      f();
    }
  };
}

/** Registers themes, and takes them away again. */
export function registerThemes(r: Registry, themes: ColorTheme[]): () => void {
  const off = themes.map((t) => registerTheme(r, t));
  return () => {
    for (const f of off) {
      f();
    }
  };
}

/**
 * Keeps the registry in step with .nota/plugins. Returns the plugin
 * themes right away, a render before the registry has them, and whether
 * the plugins are still unknown (loading, or failed to load).
 */
export function usePluginThemes(): { themes: ColorTheme[]; pending: boolean } {
  const r = useRegistry();
  const { data, isPending, isError } = usePlugins();
  const themes = useMemo(() => pluginThemes(data ?? []), [data]);
  useEffect(() => registerThemes(r, themes), [r, themes]);
  return { themes, pending: isPending || isError };
}
