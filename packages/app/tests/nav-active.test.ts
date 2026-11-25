import { describe, expect, test } from "vite-plus/test";
import { navActive, navKey } from "../src/builtins/sections.tsx";
import type { Route } from "../src/state.ts";

// The nav section subscribes to this key instead of the whole route, so
// opening a note does not re-render it; it must light the same rows.
describe("the nav section's active row", () => {
  const key = (r: Route) => navKey(r);

  test("notes, home and settings light nothing", () => {
    expect(key({ kind: "note", path: "a.md" })).toBe("");
    expect(key({ kind: "note", path: "b.md", via: "open" })).toBe("");
    expect(key({ kind: "home" })).toBe("");
    expect(key({ kind: "settings" })).toBe("");
  });

  test("two notes give the same key", () => {
    expect(key({ kind: "note", path: "a.md" })).toBe(key({ kind: "note", path: "b.md" }));
  });

  test("inbox, recent and a tag", () => {
    expect(key({ kind: "inbox" })).toBe("inbox");
    const recent = key({ kind: "list", list: { type: "recent" } });
    expect(navActive(recent, "recent")).toBe(true);
    expect(navActive(recent, "tag", "x")).toBe(false);
    const tag = key({ kind: "list", list: { type: "tag", tag: "a:b" } });
    expect(navActive(tag, "tag", "a:b")).toBe(true);
    expect(navActive(tag, "tag", "a")).toBe(false);
    expect(navActive(tag, "tag")).toBe(true);
    expect(navActive(tag, "recent")).toBe(false);
  });

  test("a folder or view list is not recent", () => {
    expect(navActive(key({ kind: "list", list: { type: "folder", path: "P" } }), "recent")).toBe(
      false,
    );
    expect(
      navActive(key({ kind: "list", list: { type: "view", id: "v", name: "V" } }), "recent"),
    ).toBe(false);
  });
});
