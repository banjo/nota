import type { NotesClient, Tree } from "@nota/client";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, test } from "vite-plus/test";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { IconGlyph } from "../src/components/IconGlyph.tsx";
import { gridMove, iconColorVar, isEmoji, lookOf, withLook } from "../src/lib/icons.ts";
import { buildTree } from "../src/lib/notes.ts";
import { createRegistry } from "../src/registry.ts";

const SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24"><path d="M0 0h24" stroke="currentColor"/></svg>';

describe("icon colours", () => {
  test.each([
    ["accent", "var(--ink)"],
    ["red", "var(--icon-red)"],
    ["gray", "var(--icon-gray)"],
    ["teal", undefined],
    ["", undefined],
    [undefined, undefined],
  ])("%s → %s", (color, want) => {
    expect(iconColorVar(color)).toBe(want);
  });
});

describe("looks", () => {
  test("lookOf keeps only what is set", () => {
    expect(lookOf({ icon: "lucide:house", svg: SVG, color: "" })).toEqual({
      icon: "lucide:house",
      svg: SVG,
    });
  });

  test("withLook replaces the old look, absent fields too", () => {
    const f = { path: "A", name: "A", notes: 1, icon: "lucide:house", svg: SVG, color: "red" };
    expect(withLook(f, { color: "blue" })).toEqual({
      path: "A",
      name: "A",
      notes: 1,
      color: "blue",
    });
    expect(withLook(f, {})).toEqual({ path: "A", name: "A", notes: 1 });
  });

  test("an emoji is an icon without a set prefix", () => {
    expect(isEmoji("🏠")).toBe(true);
    expect(isEmoji("lucide:house")).toBe(false);
    expect(isEmoji(undefined)).toBe(false);
  });

  test("the tree carries each folder's look", () => {
    const tree: Tree = {
      inbox: "Inbox",
      folders: [
        { path: "Work", name: "Work", notes: 0, icon: "lucide:briefcase", svg: SVG },
        { path: "Work/Old", name: "Old", notes: 0, color: "gray" },
        { path: "Home", name: "Home", notes: 0 },
      ],
      notes: [],
    } as unknown as Tree;
    const root = buildTree(tree);
    const work = root.folders.find((f) => f.path === "Work")!;
    expect(work.look).toEqual({ icon: "lucide:briefcase", svg: SVG });
    expect(work.folders[0]!.look).toEqual({ color: "gray" });
    expect(root.folders.find((f) => f.path === "Home")!.look).toBeUndefined();
  });
});

describe("grid keys", () => {
  // 10 cells, 4 wide:  0 1 2 3 / 4 5 6 7 / 8 9
  test.each([
    [5, "ArrowRight", 6],
    [3, "ArrowRight", 4],
    [9, "ArrowRight", 9],
    [4, "ArrowLeft", 3],
    [0, "ArrowLeft", 0],
    [5, "ArrowDown", 9],
    [6, "ArrowDown", 6],
    [5, "ArrowUp", 1],
    [2, "ArrowUp", 2],
    [5, "Home", 0],
    [5, "End", 9],
  ])("%i %s → %i", (from, key, to) => {
    expect(gridMove(from, key, 10, 4)).toBe(to);
  });

  test("other keys and an empty grid don't move", () => {
    expect(gridMove(0, "Enter", 10, 4)).toBeNull();
    expect(gridMove(0, "ArrowDown", 0, 4)).toBeNull();
  });
});

describe("IconGlyph", () => {
  test("draws the svg in the icon's colour", () => {
    const out = renderToStaticMarkup(
      <IconGlyph look={{ icon: "lucide:house", svg: SVG, color: "blue" }} fallback="F" />,
    );
    expect(out).toContain("<svg");
    expect(out).toContain("var(--icon-blue)");
    expect(out).not.toContain("F");
  });

  test("an emoji as text", () => {
    const out = renderToStaticMarkup(<IconGlyph look={{ icon: "🏠" }} fallback="F" />);
    expect(out).toContain("🏠");
    expect(out).not.toContain("<svg");
  });

  test("a colour alone colours the fallback", () => {
    const out = renderToStaticMarkup(<IconGlyph look={{ color: "accent" }} fallback="F" />);
    expect(out).toContain("F");
    expect(out).toContain("var(--ink)");
  });
});

describe("⌘K", () => {
  test("Change folder icon… only where the client can", () => {
    const r = createRegistry();
    registerBuiltins(r);
    const cmd = r.command("folder.icon")!;
    expect(cmd).toBeDefined();
    const qc = {
      getQueryData: () => ({ inbox: "Inbox", folders: [], notes: [] }),
    };
    const route = { kind: "list", list: { type: "folder", path: "Work" } };
    const ctx = (client: Partial<NotesClient>) =>
      ({ client, queryClient: qc, route }) as unknown as Parameters<
        NonNullable<typeof cmd.when>
      >[0];
    expect(cmd.when!(ctx({}))).toBe(false);
    expect(cmd.when!(ctx({ setFolderIcon: async () => ({}), searchIcons: async () => [] }))).toBe(
      true,
    );
  });
});
