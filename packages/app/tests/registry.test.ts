import { describe, expect, test, vi } from "vite-plus/test";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { createRegistry } from "../src/registry.ts";

describe("contribution registry", () => {
  test("registers, sorts, replaces and disposes", () => {
    const r = createRegistry();
    const seen = vi.fn();
    r.subscribe(seen);
    const A = () => null;
    const offB = r.register("sidebar", { id: "b", order: 20, component: A });
    r.register("sidebar", { id: "a", order: 10, component: A });
    expect(r.get("sidebar").map((s) => s.id)).toEqual(["a", "b"]);
    // Snapshots are stable until something changes (useSyncExternalStore).
    expect(r.get("sidebar")).toBe(r.get("sidebar"));
    r.register("sidebar", { id: "b", order: 5, component: A });
    expect(r.get("sidebar").map((s) => s.id)).toEqual(["b", "a"]);
    offB(); // a stale disposer leaves the replacement alone
    expect(r.get("sidebar").map((s) => s.id)).toEqual(["b", "a"]);
    expect(seen).toHaveBeenCalledTimes(3);
  });

  test("built-in features are contributions", () => {
    const r = createRegistry();
    const off = registerBuiltins(r);
    expect(r.get("sidebar").map((s) => s.id)).toEqual(["nota.nav", "nota.views", "nota.folders"]);
    expect(r.get("panel").map((p) => p.id)).toEqual(["nota.backlinks"]);
    expect(r.command("note.new")?.shortcut).toBe("mod+n");
    expect(r.command("sidebar.toggle")?.shortcut).toBe("mod+\\");
    const deco = r.get("treeDecorator")[0]!;
    expect(
      deco.decorate({
        kind: "note",
        path: "a.md",
        note: {
          path: "a.md",
          folder: "",
          title: "a",
          tags: [],
          properties: { status: "draft" },
          modified: "",
          size: 0,
        },
      }),
    ).toMatchObject({ label: "Status: draft" });
    off();
    expect(r.get("command")).toEqual([]);
  });
});
