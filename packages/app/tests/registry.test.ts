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
    expect(r.get("sidebar").map((s) => s.id)).toEqual([
      "nota.nav",
      "nota.pinned",
      "nota.opened",
      "nota.views",
      "nota.folders",
    ]);
    expect(r.get("panel").map((p) => p.id)).toEqual(["nota.backlinks"]);
    expect(r.get("inboxSuggester").map((p) => p.id)).toEqual(["nota.rules"]);
    expect(r.command("note.new")?.shortcut).toBe("mod+n");
    expect(r.command("sidebar.toggle")?.shortcut).toBe("mod+\\");
    // Pin and Unpin share ⌘⇧P; only one applies to a note at a time.
    expect(r.command("note.pin")?.shortcut).toBe("mod+shift+p");
    expect(r.command("note.unpin")?.shortcut).toBe("mod+shift+p");
    // The tree ships without decorations: status lives on the page.
    expect(r.get("treeDecorator")).toEqual([]);
    off();
    expect(r.get("command")).toEqual([]);
  });
});
