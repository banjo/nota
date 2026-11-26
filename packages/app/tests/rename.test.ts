import type { NotesClient } from "@nota/client";
import { QueryClient } from "@tanstack/react-query";
import { readFileSync } from "node:fs";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { linksMessage, renameFolder } from "../src/actions.ts";
import { registerBuiltins } from "../src/builtins/index.tsx";
import { menuTarget } from "../src/components/RowMenu.tsx";
import {
  checkFolderRename,
  checkNoteRename,
  followedPath,
  nameOf,
  remapFolder,
} from "../src/lib/rename.ts";
import { createRegistry } from "../src/registry.ts";
import { useUI } from "../src/state.ts";

const toast = vi.hoisted(() => Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }));
vi.mock("@nota/ui/sonner", () => ({ toast }));

const cases = JSON.parse(
  readFileSync(new URL("../../../internal/vault/testdata/names.json", import.meta.url), "utf8"),
) as { in: string; kind: "vault" | "inbox"; name: string; error: string }[];

const tree = {
  inbox: "Inbox",
  folders: [
    { path: "Inbox", name: "Inbox", notes: 0 },
    { path: "Projects", name: "Projects", notes: 1 },
    { path: "Projects/Ideas", name: "Ideas", notes: 0 },
    { path: "Work", name: "Work", notes: 0 },
  ],
};

describe("checkFolderRename", () => {
  test("uses the shared name rules (names.json, folder kind)", () => {
    for (const c of cases.filter((c) => c.kind === "inbox" && c.in.trim() !== "")) {
      const r = checkFolderRename(c.in, "Work", tree);
      const error = c.error.includes("slashes")
        ? "Use a single folder name, without slashes."
        : c.error;
      expect(r.error, JSON.stringify(c.in)).toBe(error);
      // A fine name that is not the current one is the new path in the same parent.
      if (!c.error && c.name !== "Work") expect(r.to).toBe(c.name);
    }
  });

  test("empty or unchanged cancels without an error", () => {
    expect(checkFolderRename("  ", "Work", tree)).toEqual({ to: "", error: "" });
    expect(checkFolderRename("Work", "Work", tree)).toEqual({ to: "", error: "" });
  });

  test("stays in the parent folder", () => {
    expect(checkFolderRename("Notions", "Projects/Ideas", tree).to).toBe("Projects/Notions");
  });

  test("refuses a taken name in any case, and a case-only change", () => {
    expect(checkFolderRename("projects", "Work", tree).error).toMatch(/already a folder/);
    expect(checkFolderRename("work", "Work", tree).error).toMatch(/letter case/);
  });
});

describe("checkNoteRename", () => {
  const sibs = ["Projects/Plan.md", "Projects/Notes.md"];
  test("refuses what the vault refuses", () => {
    expect(checkNoteRename("a/b", "Projects/Plan.md", sibs).error).toBe(
      "A name can’t contain slashes.",
    );
    expect(checkNoteRename("a:b", "Projects/Plan.md", sibs).error).toMatch(/can’t contain/);
    expect(checkNoteRename("  ", "Projects/Plan.md", sibs)).toEqual({ title: "", error: "" });
  });
  test("a name that is taken in the folder is refused, its own is not", () => {
    expect(checkNoteRename("notes", "Projects/Plan.md", sibs).error).toMatch(/already a note/);
    expect(checkNoteRename("Plan", "Projects/Plan.md", sibs)).toEqual({ title: "Plan", error: "" });
  });
});

describe("followedPath and remapFolder", () => {
  test("the file follows a title it was named after, or Untitled", () => {
    expect(followedPath("A/Plan.md", "Plan", "Roadmap")).toBe("A/Roadmap.md");
    expect(followedPath("A/Hello.md", "Hello", "Hello")).toBeNull();
    expect(followedPath("Untitled 2.md", "Hello", "Hi")).toBe("Hi.md");
    expect(followedPath("A/Custom.md", "Plan", "Roadmap")).toBeNull();
  });
  test("remaps a folder and what is under it, nothing else", () => {
    expect(remapFolder("Work", "Work", "Job")).toBe("Job");
    expect(remapFolder("Work/a/b.md", "Work", "Job")).toBe("Job/a/b.md");
    expect(remapFolder("Workshop/a.md", "Work", "Job")).toBe("Workshop/a.md");
  });
  test("nameOf", () => {
    expect(nameOf({ kind: "folder", path: "A/B" })).toBe("B");
    expect(nameOf({ kind: "note", path: "A/B.md" })).toBe("B");
    expect(nameOf({ kind: "note", path: "A/B.md" }, "Title")).toBe("Title");
  });
});

describe("menuTarget", () => {
  const at = (dataset: Record<string, string> | null) =>
    ({ closest: () => (dataset ? { dataset } : null) }) as unknown as EventTarget;

  test("a note row, a folder row, anything else is the empty area", () => {
    expect(menuTarget(at({ ctxKind: "note", ctxPath: "A/B.md", ctxTitle: "Bee" }))).toEqual({
      kind: "note",
      path: "A/B.md",
      title: "Bee",
    });
    expect(menuTarget(at({ ctxKind: "folder", ctxPath: "A/Sub" }))).toEqual({
      kind: "folder",
      path: "A/Sub",
      name: "Sub",
    });
    expect(menuTarget(at(null))).toEqual({ kind: "empty" });
    expect(menuTarget(null)).toEqual({ kind: "empty" });
    expect(menuTarget({} as EventTarget)).toEqual({ kind: "empty" });
  });
});

describe("renameFolder keeps the screen on the same things", () => {
  const qc = new QueryClient();
  const client = {
    renameFolder: vi.fn(async (_p: string, to: string) => ({ from: "Work", path: to })),
  } as unknown as NotesClient;
  beforeEach(() => {
    useUI.setState({
      route: { kind: "note", path: "Work/Deep/a.md" },
      expanded: {
        "section:folders": true,
        "folder:Work": true,
        "folder:Work/Deep": true,
        "folder:Other": true,
      },
    });
  });

  test("moves unfolded state and the open note", async () => {
    expect(await renameFolder(client, qc, "Work", "Job")).toBe("Job");
    const s = useUI.getState();
    expect(s.route).toEqual({ kind: "note", path: "Job/Deep/a.md" });
    expect(s.expanded).toMatchObject({
      "folder:Job": true,
      "folder:Job/Deep": true,
      "folder:Other": true,
    });
    expect(s.expanded["folder:Work"]).toBeUndefined();
  });

  test("the open folder list follows", async () => {
    useUI.setState({ route: { kind: "list", list: { type: "folder", path: "Work" } } });
    await renameFolder(client, qc, "Work", "Job");
    expect(useUI.getState().route).toEqual({ kind: "list", list: { type: "folder", path: "Job" } });
  });

  test("a refusal shows in the row, not as a toast", async () => {
    const bad = {
      renameFolder: vi.fn().mockRejectedValue(new Error("already exists")),
    } as unknown as NotesClient;
    const onError = vi.fn();
    expect(await renameFolder(bad, qc, "Work", "Job", { onError })).toBeUndefined();
    expect(onError).toHaveBeenCalledWith("already exists");
    expect(toast.error).not.toHaveBeenCalled();
  });
});

describe("⌘K has the rename commands", () => {
  test("note.rename and folder.rename", () => {
    const r = createRegistry();
    registerBuiltins(r);
    expect(r.command("note.rename")?.title).toBe("Rename note");
    expect(r.command("folder.rename")?.title).toBe("Rename folder");
  });
});

describe("linksMessage", () => {
  test("says how many links a move rewrote, and nothing when none", () => {
    expect(linksMessage(undefined)).toBeUndefined();
    expect(linksMessage({ count: 0, notes: [] })).toBeUndefined();
    expect(linksMessage({ count: 1, notes: [{ path: "a.md", links: 1 }] })).toBe(
      "Updated 1 link in 1 note",
    );
    expect(
      linksMessage({
        count: 3,
        notes: [
          { path: "a.md", links: 2 },
          { path: "b.md", links: 1 },
        ],
      }),
    ).toBe("Updated 3 links in 2 notes");
  });

  test("a renamed folder reports the links it rewrote", async () => {
    const qc = new QueryClient();
    const client = {
      renameFolder: vi.fn(async () => ({
        from: "Work",
        path: "Job",
        links: { count: 2, notes: [{ path: "a.md", links: 2 }] },
      })),
    } as unknown as NotesClient;
    toast.mockClear();
    await renameFolder(client, qc, "Work", "Job");
    expect(toast).toHaveBeenCalledWith("Updated 2 links in 1 note");
  });
});
