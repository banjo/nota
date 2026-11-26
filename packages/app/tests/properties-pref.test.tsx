import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { PropertiesChip } from "../src/components/PropertiesSection.tsx";
import {
  loadPropertiesPref,
  PROPERTIES_KEY,
  propertyCount,
  readPropertiesPref,
  savePropertiesPref,
  toggledPropertiesPref,
  watchPropertiesPref,
} from "../src/lib/propertiesPref.ts";
import { backlinkSources, backlinksPref } from "../src/lib/backlinksPref.ts";
import { createRegistry } from "../src/registry.ts";
import { useUI } from "../src/state.ts";

function fakeStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial));
  return {
    data,
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
    removeItem: (k: string) => void data.delete(k),
  };
}

describe("properties preference", () => {
  test("hidden by default; only show|hide are read", () => {
    expect(readPropertiesPref(null)).toBe("hide");
    expect(readPropertiesPref("show")).toBe("show");
    expect(readPropertiesPref("hide")).toBe("hide");
    expect(readPropertiesPref("yes")).toBe("hide");
  });

  test("survives a restart; the default is stored as absence", () => {
    const st = fakeStorage();
    expect(loadPropertiesPref(st)).toBe("hide");
    savePropertiesPref(st, "show");
    expect(st.data.get(PROPERTIES_KEY)).toBe("show");
    expect(loadPropertiesPref(st)).toBe("show");
    savePropertiesPref(st, "hide");
    expect(st.data.has(PROPERTIES_KEY)).toBe(false);
  });

  test("works without storage", () => {
    expect(loadPropertiesPref(null)).toBe("hide");
    savePropertiesPref(null, "show");
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
    expect(loadPropertiesPref(broken)).toBe("hide");
    savePropertiesPref(broken, "show");
  });

  test("toggles", () => {
    expect(toggledPropertiesPref("hide")).toBe("show");
    expect(toggledPropertiesPref("show")).toBe("hide");
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
    const off = watchPropertiesPref(win, st, (p) => seen.push(p));
    st.setItem(PROPERTIES_KEY, "show");
    for (const fn of fns) fn({ key: PROPERTIES_KEY });
    for (const fn of fns) fn({ key: "nota:note-mode" }); // unrelated
    st.removeItem(PROPERTIES_KEY);
    for (const fn of fns) fn({ key: null }); // storage cleared
    expect(seen).toEqual(["show", "hide"]);
    off();
    expect(fns.size).toBe(0);
  });

  test("counts every frontmatter key", () => {
    expect(propertyCount(undefined)).toBe(0);
    expect(propertyCount({})).toBe(0);
    expect(propertyCount({ tags: ["a"], status: "draft", created: "x", custom: 1 })).toBe(4);
    expect(propertyCount({ created: "x", pinned: 10 })).toBe(1);
  });
});

describe("the store and ⌘K", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", undefined);
    useUI.setState({ properties: "hide" });
  });

  test("Toggle properties flips the shared state", () => {
    const r = createRegistry();
    const off = registerBuiltins(r);
    const cmd = r.command("view.properties.toggle");
    expect(cmd?.title).toBe("Toggle properties");
    expect(cmd?.shortcut).toBeUndefined(); // ⌘K only
    void cmd?.run({} as never);
    expect(useUI.getState().properties).toBe("show");
    void cmd?.run({} as never);
    expect(useUI.getState().properties).toBe("hide");
    off();
  });
});

describe("linked from", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", undefined);
    useUI.setState({ backlinks: "hide" });
  });

  test("hidden by default, kept under its own key", () => {
    const st = fakeStorage();
    expect(backlinksPref.load(st)).toBe("hide");
    backlinksPref.save(st, "show");
    expect(st.data.get("nota:backlinks")).toBe("show");
    expect(st.data.has(PROPERTIES_KEY)).toBe(false);
  });

  test("Toggle linked from flips its own state, not the properties", () => {
    useUI.setState({ properties: "hide" });
    const r = createRegistry();
    const off = registerBuiltins(r);
    void r.command("view.backlinks.toggle")?.run({} as never);
    expect(useUI.getState().backlinks).toBe("show");
    expect(useUI.getState().properties).toBe("hide");
    off();
  });

  test("counts each linking note once, never the note itself", () => {
    const links = [{ from: "a.md" }, { from: "a.md" }, { from: "b.md" }, { from: "self.md" }];
    expect(backlinkSources(links, "self.md")).toEqual(["a.md", "b.md"]);
  });
});

describe("the chip", () => {
  const html = (shown: boolean, count: number) =>
    renderToStaticMarkup(<PropertiesChip shown={shown} count={count} onToggle={() => {}} />);

  test("hidden: says Properties with the count, and offers to show", () => {
    const out = html(false, 3);
    expect(out).toContain('aria-expanded="false"');
    expect(out).toContain("Show properties (3)");
    expect(out).toContain(">Properties<");
    expect(out).toContain(">3<");
  });

  test("shown: no count, offers to hide", () => {
    const out = html(true, 3);
    expect(out).toContain('aria-expanded="true"');
    expect(out).toContain("Hide properties");
    expect(out).not.toContain(">3<");
  });

  test("an empty note has no count but still the chip", () => {
    const out = html(false, 0);
    expect(out).toContain("Show properties (0)");
    expect(out).not.toContain(">0<");
  });

  test("a thumb-sized target under a coarse pointer, and a visible focus ring", () => {
    const out = html(false, 1);
    expect(out).toContain("pointer-coarse:h-10");
    expect(out).toContain("focus-visible:ring-2");
  });
});
