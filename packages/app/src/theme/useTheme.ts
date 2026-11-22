import { useEffect, useSyncExternalStore } from "react";
import { type ThemeState, themeStore } from "./store.ts";

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
 * theme-color metas (browser and phone chrome).
 */
export function useThemeChrome(theme: ThemeState) {
  useEffect(() => {
    const bg = currentBackground();
    if (!bg) return;
    for (const m of document.querySelectorAll('meta[name="theme-color"]')) {
      m.setAttribute("content", bg);
    }
  }, [theme.mode, theme.palette, theme.resolved]);
}
