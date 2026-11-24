// Appearance: the mode (System, Light or Dark) and the colour theme
// ("palette"), stored per device. The keys are shared with the landing
// page, whose toggle writes nota:theme too. The inline script in each
// index.html applies the same attributes before the first paint; this
// store takes over once the app runs, follows the system while the mode
// is System, and picks up changes made in another window (the
// quick-capture panel, another tab) through the storage event.
//
// On <html>:
//   data-theme    the explicit choice ("light" | "dark"), absent for System
//   data-mode     the mode in effect ("light" | "dark"); Tailwind's dark:
//   data-palette  the colour theme id ("nota" by default)

export type ThemeMode = "system" | "light" | "dark";
export type ResolvedMode = "light" | "dark";

export interface ThemeState {
  /** What the user chose. */
  mode: ThemeMode;
  /** The colour theme id. */
  palette: string;
  /** The mode in effect: the choice, or the system's for System. */
  resolved: ResolvedMode;
}

export const THEME_KEY = "nota:theme";
export const PALETTE_KEY = "nota:palette";
/**
 * The tokens of the chosen theme when they are not in CSS ({id, light,
 * dark}), so the pre-paint script in index.html can set them too.
 */
export const TOKENS_KEY = "nota:palette-tokens";
export const DEFAULT_PALETTE = "nota";

const PALETTE_ID = /^[a-z0-9][a-z0-9.-]*$/;

/** The stored mode: "light" or "dark", anything else is System. */
export function readMode(v: string | null | undefined): ThemeMode {
  return v === "light" || v === "dark" ? v : "system";
}

/**
 * Palette ids that were renamed or folded into another one. "jade" was
 * an optional blue-green next to the default; when Jade became Nota's
 * brand colour the default palette took it over, so a device that had
 * picked Jade now simply has the default. The inline pre-paint scripts
 * in index.html map it the same way.
 */
const RENAMED_PALETTES: Record<string, string> = { jade: DEFAULT_PALETTE };

/** The stored palette id, or the default for none or garbage. */
export function readPalette(v: string | null | undefined): string {
  if (v && Object.hasOwn(RENAMED_PALETTES, v)) return RENAMED_PALETTES[v]!;
  return v && PALETTE_ID.test(v) ? v : DEFAULT_PALETTE;
}

export function resolveMode(mode: ThemeMode, systemDark: boolean): ResolvedMode {
  if (mode === "system") return systemDark ? "dark" : "light";
  return mode;
}

type Storage = Pick<globalThis.Storage, "getItem" | "setItem" | "removeItem">;

interface MediaLike {
  matches: boolean;
  addEventListener(type: "change", fn: (e: { matches: boolean }) => void): void;
  removeEventListener(type: "change", fn: (e: { matches: boolean }) => void): void;
}

interface StorageEventLike {
  key: string | null;
}

interface WindowLike {
  addEventListener(type: "storage", fn: (e: StorageEventLike) => void): void;
  removeEventListener(type: "storage", fn: (e: StorageEventLike) => void): void;
}

/** What the store touches, so tests can pass fakes. */
export interface ThemeEnv {
  storage?: Storage | null;
  /** matchMedia("(prefers-color-scheme: dark)"). */
  media?: MediaLike | null;
  /** document.documentElement. */
  root?: { dataset: DOMStringMap } | null;
  /** For the storage event. */
  window?: WindowLike | null;
}

export interface ThemeStore {
  get: () => ThemeState;
  subscribe: (fn: () => void) => () => void;
  setMode: (mode: ThemeMode) => void;
  setPalette: (id: string) => void;
  /** Switches to the opposite of the mode in effect (an explicit choice). */
  toggleDark: () => void;
  dispose: () => void;
}

function read(storage: Storage | null | undefined, key: string): string | null {
  try {
    return storage?.getItem(key) ?? null;
  } catch {
    return null; // storage disabled: System and the default palette
  }
}

function write(storage: Storage | null | undefined, key: string, value: string | null) {
  try {
    if (value === null) storage?.removeItem(key);
    else storage?.setItem(key, value);
  } catch {
    // Private browsing: the choice lasts this page.
  }
}

export function createThemeStore(env: ThemeEnv): ThemeStore {
  const { storage, media, root, window: win } = env;
  const listeners = new Set<() => void>();

  const load = (): ThemeState => {
    const mode = readMode(read(storage, THEME_KEY));
    const palette = readPalette(read(storage, PALETTE_KEY));
    return { mode, palette, resolved: resolveMode(mode, !!media?.matches) };
  };

  let state = load();

  const apply = () => {
    if (!root) return;
    const d = root.dataset;
    if (state.mode === "system") delete d.theme;
    else d.theme = state.mode;
    d.mode = state.resolved;
    d.palette = state.palette;
  };

  const set = (next: ThemeState) => {
    if (
      next.mode === state.mode &&
      next.palette === state.palette &&
      next.resolved === state.resolved
    )
      return;
    state = next;
    apply();
    for (const fn of listeners) fn();
  };

  const onMedia = (e: { matches: boolean }) =>
    set({ ...state, resolved: resolveMode(state.mode, e.matches) });
  const onStorage = (e: StorageEventLike) => {
    // key is null when another window cleared the whole storage.
    if (e.key === null || e.key === THEME_KEY || e.key === PALETTE_KEY) set(load());
  };

  apply();
  media?.addEventListener("change", onMedia);
  win?.addEventListener("storage", onStorage);

  const setMode = (mode: ThemeMode) => {
    write(storage, THEME_KEY, mode === "system" ? null : mode);
    set({ ...state, mode, resolved: resolveMode(mode, !!media?.matches) });
  };

  return {
    get: () => state,
    subscribe: (fn) => {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
    setMode,
    setPalette: (id) => {
      const palette = readPalette(id);
      write(storage, PALETTE_KEY, palette === DEFAULT_PALETTE ? null : palette);
      set({ ...state, palette });
    },
    toggleDark: () => setMode(state.resolved === "dark" ? "light" : "dark"),
    dispose: () => {
      media?.removeEventListener("change", onMedia);
      win?.removeEventListener("storage", onStorage);
      listeners.clear();
    },
  };
}

let shared: ThemeStore | null = null;

/** The app's store over the real browser (one per window). */
export function themeStore(): ThemeStore {
  if (!shared) {
    const browser = typeof window !== "undefined";
    let storage: Storage | null = null;
    try {
      storage = browser ? window.localStorage : null;
    } catch {
      storage = null;
    }
    shared = createThemeStore({
      storage,
      media:
        browser && window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null,
      root: browser ? document.documentElement : null,
      window: browser ? window : null,
    });
  }
  return shared;
}
