import { describe, expect, test, vi } from "vite-plus/test";
import {
  createThemeStore,
  PALETTE_KEY,
  readMode,
  readPalette,
  resolveMode,
  THEME_KEY,
} from "../src/theme/store.ts";

function fakeEnv(stored: Record<string, string> = {}, systemDark = false) {
  const data = new Map(Object.entries(stored));
  const storage = {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
    removeItem: (k: string) => void data.delete(k),
  };
  const mediaFns = new Set<(e: { matches: boolean }) => void>();
  const media = {
    matches: systemDark,
    addEventListener: (_: "change", fn: (e: { matches: boolean }) => void) => mediaFns.add(fn),
    removeEventListener: (_: "change", fn: (e: { matches: boolean }) => void) =>
      mediaFns.delete(fn),
  };
  const storageFns = new Set<(e: { key: string | null }) => void>();
  const win = {
    addEventListener: (_: "storage", fn: (e: { key: string | null }) => void) => storageFns.add(fn),
    removeEventListener: (_: "storage", fn: (e: { key: string | null }) => void) =>
      storageFns.delete(fn),
  };
  const root = { dataset: {} as DOMStringMap };
  return {
    env: { storage, media, root, window: win },
    data,
    root,
    /** The OS switches light/dark. */
    system(dark: boolean) {
      media.matches = dark;
      for (const fn of mediaFns) fn({ matches: dark });
    },
    /** Another window wrote to the shared storage. */
    otherWindow(key: string, value: string | null) {
      if (value === null) data.delete(key);
      else data.set(key, value);
      for (const fn of storageFns) fn({ key });
    },
    listeners: () => mediaFns.size + storageFns.size,
  };
}

describe("theme store", () => {
  test("reads and resolves modes", () => {
    expect(readMode("dark")).toBe("dark");
    expect(readMode("light")).toBe("light");
    expect(readMode(null)).toBe("system");
    expect(readMode("auto")).toBe("system");
    expect(resolveMode("system", true)).toBe("dark");
    expect(resolveMode("system", false)).toBe("light");
    expect(resolveMode("light", true)).toBe("light");
    expect(resolveMode("dark", false)).toBe("dark");
    expect(readPalette("paper")).toBe("paper");
    expect(readPalette('x"]{}')).toBe("nota");
    expect(readPalette(null)).toBe("nota");
  });

  test("defaults to System and the nota palette, applied to <html>", () => {
    const f = fakeEnv({}, true);
    const s = createThemeStore(f.env);
    expect(s.get()).toEqual({ mode: "system", palette: "nota", resolved: "dark" });
    expect(f.root.dataset).toEqual({ mode: "dark", palette: "nota" });
  });

  test("an explicit choice persists and wins over the system", () => {
    const f = fakeEnv({}, false);
    const s = createThemeStore(f.env);
    const seen = vi.fn();
    s.subscribe(seen);
    s.setMode("dark");
    expect(f.data.get(THEME_KEY)).toBe("dark");
    expect(f.root.dataset).toMatchObject({ theme: "dark", mode: "dark" });
    f.system(false); // no change: Dark is explicit
    expect(s.get().resolved).toBe("dark");
    s.setMode("system");
    expect(f.data.has(THEME_KEY)).toBe(false); // absent = System, as on the landing page
    expect(f.root.dataset.theme).toBeUndefined();
    expect(f.root.dataset.mode).toBe("light");
    expect(seen).toHaveBeenCalledTimes(2);

    s.setPalette("forest");
    expect(f.data.get(PALETTE_KEY)).toBe("forest");
    expect(f.root.dataset.palette).toBe("forest");
    s.setPalette("nota");
    expect(f.data.has(PALETTE_KEY)).toBe(false);

    // A new window starts from what was stored.
    s.setMode("light");
    s.setPalette("rose");
    const again = createThemeStore(fakeEnv(Object.fromEntries(f.data), true).env);
    expect(again.get()).toEqual({ mode: "light", palette: "rose", resolved: "light" });
  });

  test("System follows the OS live", () => {
    const f = fakeEnv({}, false);
    const s = createThemeStore(f.env);
    const seen = vi.fn();
    s.subscribe(seen);
    f.system(true);
    expect(s.get().resolved).toBe("dark");
    expect(f.root.dataset.mode).toBe("dark");
    f.system(true); // same value: no notification
    expect(seen).toHaveBeenCalledTimes(1);
  });

  test("toggleDark flips the mode in effect", () => {
    const f = fakeEnv({}, true);
    const s = createThemeStore(f.env);
    s.toggleDark(); // System showing dark → Light
    expect(s.get()).toMatchObject({ mode: "light", resolved: "light" });
    s.toggleDark();
    expect(s.get()).toMatchObject({ mode: "dark", resolved: "dark" });
  });

  test("changes from another window arrive through the storage event", () => {
    const f = fakeEnv({}, false);
    const s = createThemeStore(f.env);
    const seen = vi.fn();
    s.subscribe(seen);
    f.otherWindow(THEME_KEY, "dark");
    f.otherWindow(PALETTE_KEY, "paper");
    expect(s.get()).toEqual({ mode: "dark", palette: "paper", resolved: "dark" });
    expect(f.root.dataset).toEqual({ theme: "dark", mode: "dark", palette: "paper" });
    f.otherWindow("unrelated", "x");
    expect(seen).toHaveBeenCalledTimes(2);
    f.otherWindow(THEME_KEY, null);
    expect(s.get().mode).toBe("system");
    s.dispose();
    expect(f.listeners()).toBe(0);
  });

  test("works without storage", () => {
    const f = fakeEnv();
    const broken = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
      removeItem: () => {
        throw new Error("denied");
      },
    };
    const s = createThemeStore({ ...f.env, storage: broken });
    s.setMode("dark");
    expect(s.get().resolved).toBe("dark");
  });
});
