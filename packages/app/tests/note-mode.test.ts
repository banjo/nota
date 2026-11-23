import { describe, expect, test } from "vite-plus/test";
import { registerBuiltins } from "../src/builtins/index.tsx";
import {
  loadNoteMode,
  NOTE_MODE_KEY,
  readNoteMode,
  saveNoteMode,
  toggledNoteMode,
  watchNoteMode,
} from "../src/lib/noteMode.ts";
import { matches } from "../src/lib/shortcuts.ts";
import { createRegistry } from "../src/registry.ts";

function fakeStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial));
  return {
    data,
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
    removeItem: (k: string) => void data.delete(k),
  };
}

describe("note mode preference", () => {
  test("reads known modes, anything else is Live", () => {
    expect(readNoteMode("reading")).toBe("reading");
    expect(readNoteMode("source")).toBe("source");
    expect(readNoteMode("live")).toBe("live");
    expect(readNoteMode("edit")).toBe("live");
    expect(readNoteMode(null)).toBe("live");
  });

  test("survives a restart: what is saved is what loads", () => {
    const st = fakeStorage();
    expect(loadNoteMode(st)).toBe("live");
    saveNoteMode(st, "reading");
    expect(st.data.get(NOTE_MODE_KEY)).toBe("reading");
    expect(loadNoteMode(st)).toBe("reading");
    saveNoteMode(st, "source");
    expect(loadNoteMode(st)).toBe("source");
    saveNoteMode(st, "live"); // the default is stored as absence
    expect(st.data.has(NOTE_MODE_KEY)).toBe(false);
  });

  test("works without storage", () => {
    expect(loadNoteMode(null)).toBe("live");
    saveNoteMode(null, "reading");
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
    expect(loadNoteMode(broken)).toBe("live");
    saveNoteMode(broken, "reading");
  });

  test("toggle swaps Live and Reading; Source goes back to Live", () => {
    expect(toggledNoteMode("live")).toBe("reading");
    expect(toggledNoteMode("reading")).toBe("live");
    expect(toggledNoteMode("source")).toBe("live");
  });

  test("another window's change is followed, until disposed", () => {
    const st = fakeStorage();
    const fns = new Set<(e: { key: string | null }) => void>();
    const win = {
      addEventListener: (_: "storage", fn: (e: { key: string | null }) => void) => fns.add(fn),
      removeEventListener: (_: "storage", fn: (e: { key: string | null }) => void) =>
        fns.delete(fn),
    };
    const seen: string[] = [];
    const off = watchNoteMode(win, st, (m) => seen.push(m));
    st.setItem(NOTE_MODE_KEY, "reading");
    for (const fn of fns) fn({ key: NOTE_MODE_KEY });
    for (const fn of fns) fn({ key: "nota:theme" }); // unrelated
    st.removeItem(NOTE_MODE_KEY);
    for (const fn of fns) fn({ key: null }); // storage cleared
    expect(seen).toEqual(["reading", "live"]);
    off();
    expect(fns.size).toBe(0);
  });
});

describe("view mode commands", () => {
  test("the palette lists all three modes and the toggle", () => {
    const r = createRegistry();
    const off = registerBuiltins(r);
    expect(r.command("view.mode.live")?.title).toBe("View: Live mode");
    expect(r.command("view.mode.reading")?.title).toBe("View: Reading mode");
    expect(r.command("view.mode.source")?.title).toBe("View: Source mode");
    expect(r.command("view.mode.toggle")?.shortcut).toBe("mod+e");
    // ⌘E is the only shortcut of the four.
    for (const id of ["live", "reading", "source"])
      expect(r.command(`view.mode.${id}`)?.shortcut).toBeUndefined();
    // Only offered while a note is open.
    const ctx = { route: { kind: "home" } } as never;
    expect(r.command("view.mode.toggle")?.when?.(ctx)).toBe(false);
    off();
  });

  test("no other command claims mod+e", () => {
    const r = createRegistry();
    const off = registerBuiltins(r);
    const owners = r.get("command").filter((c) => c.shortcut === "mod+e");
    expect(owners.map((c) => c.id)).toEqual(["view.mode.toggle"]);
    off();
  });

  test("⌘E matches on macOS, Ctrl+E (line end) does not; Ctrl+E matches elsewhere", () => {
    const key = (o: object) => ({
      key: "e",
      code: "KeyE",
      metaKey: false,
      ctrlKey: false,
      shiftKey: false,
      altKey: false,
      ...o,
    });
    expect(matches(key({ metaKey: true }), "mod+e", true)).toBe(true);
    expect(matches(key({ ctrlKey: true }), "mod+e", true)).toBe(false);
    expect(matches(key({ ctrlKey: true }), "mod+e", false)).toBe(true);
    expect(matches(key({}), "mod+e", true)).toBe(false);
  });
});
