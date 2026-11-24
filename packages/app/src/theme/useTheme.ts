import type { ShellApi } from "@nota/client";
import { useEffect, useMemo, useSyncExternalStore } from "react";
import { THEME_TOKENS } from "./custom.ts";
import { type ThemeState, TOKENS_KEY, themeStore } from "./store.ts";
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

type CachedTokens = Pick<ColorTheme, "id" | "tokens">;

function readCache(): CachedTokens | undefined {
  try {
    const c: unknown = JSON.parse(localStorage.getItem(TOKENS_KEY) ?? "null");
    return c && typeof c === "object" && "id" in c ? (c as CachedTokens) : undefined;
  } catch {
    return undefined;
  }
}

function writeCache(theme: ColorTheme | undefined) {
  try {
    if (theme?.tokens) {
      localStorage.setItem(TOKENS_KEY, JSON.stringify({ id: theme.id, tokens: theme.tokens }));
    } else {
      localStorage.removeItem(TOKENS_KEY);
    }
  } catch {
    // No storage: the theme still applies, a moment after the first paint.
  }
}

const TOKEN_PROPS = THEME_TOKENS.map((t) => `--${t}`);

/**
 * Sets the chosen theme's tokens on <html> (Nota's own themes have
 * theirs in palettes.css), and takes them off again. While the vault's
 * plugins are still loading (`pending`), the tokens the pre-paint script
 * set from the cache stay, so a plugin theme doesn't flash.
 */
export function useThemeTokens(theme: ThemeState, themes: ColorTheme[], pending = false) {
  const chosen = themes.find((t) => t.id === theme.palette);
  const cached = useMemo(() => {
    const c = readCache();
    return c?.id === theme.palette ? c : undefined;
  }, [theme.palette]);
  const waiting = !chosen && pending;
  const tokens = (chosen ?? (waiting ? cached : undefined))?.tokens?.[theme.resolved];
  useEffect(() => {
    if (!waiting) {
      writeCache(chosen);
    }
  }, [chosen, waiting]);
  useEffect(() => {
    const style = document.documentElement.style;
    for (const k of TOKEN_PROPS) style.removeProperty(k);
    if (tokens) {
      for (const [k, v] of Object.entries(tokens)) {
        if (k.startsWith("--")) style.setProperty(k, v);
      }
    }
    return () => {
      for (const k of TOKEN_PROPS) style.removeProperty(k);
    };
  }, [tokens]);
}
