import type { ShellApi } from "@nota/client";
import { useEffect, useSyncExternalStore } from "react";
import { type ThemeState, themeStore } from "./store.ts";
import type { ColorTheme } from "./themes.ts";

/** The appearance in effect, live. */
export function useTheme(): ThemeState {
  const s = themeStore();
  return useSyncExternalStore(s.subscribe, s.get, s.get);
}

/** The page's own colour, as the browser chrome and native windows should show it. */
export function currentBackground(name = "--background"): string {
  if (typeof document === "undefined") return "";
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/**
 * Keeps what lives outside the page's CSS in step with the theme: the
 * theme-color metas (browser and phone chrome) and, on the desktop, the
 * windows' native appearance and the colour behind the page.
 */
export function useThemeChrome(
  theme: ThemeState,
  shell?: ShellApi,
  /** The token at the very top of the page (a phone's sidebar pane is --sidebar). */
  top = "--background",
) {
  useEffect(() => {
    const color = currentBackground(top);
    if (!color) return;
    for (const m of document.querySelectorAll('meta[name="theme-color"]')) {
      m.setAttribute("content", color);
    }
  }, [theme.mode, theme.palette, theme.resolved, top]);
  useEffect(() => {
    const bg = currentBackground();
    void shell
      ?.setAppearance?.({
        mode: theme.mode,
        resolved: theme.resolved,
        background: bg,
        captureBackground: currentBackground("--surface"),
      })
      .catch(() => {}); // cosmetic: the page is already right
  }, [theme.mode, theme.palette, theme.resolved, shell]);
}

/**
 * Sets a contributed theme's tokens on <html> while it is chosen (built-in
 * themes have theirs in palettes.css), and takes them off again.
 */
export function useThemeTokens(theme: ThemeState, themes: ColorTheme[]) {
  const chosen = themes.find((t) => t.id === theme.palette);
  const tokens = chosen?.tokens?.[theme.resolved];
  useEffect(() => {
    if (!tokens) return;
    const style = document.documentElement.style;
    const names = Object.keys(tokens).filter((k) => k.startsWith("--"));
    for (const k of names) style.setProperty(k, tokens[k]!);
    return () => {
      for (const k of names) style.removeProperty(k);
    };
  }, [tokens]);
}
