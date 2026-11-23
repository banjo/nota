import type { NotesClient } from "@nota/client";
import { TooltipProvider } from "@nota/ui/tooltip";
import { QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { readFileSync } from "node:fs";
import { addFolder } from "../src/actions.ts";
import { registerBuiltins } from "../src/builtins/index.tsx";
import {
  cancelNewFolder,
  FolderNameField,
  FolderRowActions,
  startNewFolder,
  useFolderDraft,
} from "../src/components/NewFolder.tsx";
import { checkNewFolder, withDraft } from "../src/lib/newFolder.ts";
import { buildTree, flattenTree } from "../src/lib/notes.ts";
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
  notes: [
    {
      path: "Projects/Plan.md",
      title: "Plan",
      folder: "Projects",
      tags: [],
      created: "",
      updated: "",
    },
  ],
  cursor: 0,
} as unknown as Parameters<typeof buildTree>[0];

describe("checkNewFolder", () => {
  test("uses the vault's name rules (the shared fixture)", () => {
    for (const c of cases) {
      if (c.in.trim() === "") continue;
      // A folder row takes one name: the slash message is the folder one.
      const error = c.error.includes("slashes")
        ? "Use a single folder name, without slashes."
        : c.error;
      expect(checkNewFolder(c.in, "", tree), JSON.stringify(c.in)).toEqual({
        path: c.name,
        error,
      });
    }
  });

  test("an empty name is no error: Enter cancels", () => {
    expect(checkNewFolder("", "Projects", tree)).toEqual({ path: "", error: "" });
    expect(checkNewFolder("   ", "", tree)).toEqual({ path: "", error: "" });
  });

  test("makes the path inside the parent, NFC and trimmed", () => {
    expect(checkNewFolder("  Drafts ", "Projects", tree).path).toBe("Projects/Drafts");
    expect(checkNewFolder("Café", "", tree).path).toBe("Café");
  });

  test("refuses a folder that is there already, in any case", () => {
    expect(checkNewFolder("ideas", "Projects", tree).error).toBe(
      "There is already a folder called “ideas” here.",
    );
    expect(checkNewFolder("work", "", tree).error).toMatch(/already/);
    expect(checkNewFolder("Inbox", "", tree).error).toMatch(/already/);
    // The same name elsewhere is fine.
    expect(checkNewFolder("Ideas", "Work", tree).path).toBe("Work/Ideas");
  });
});

describe("withDraft", () => {
  const rows = flattenTree(buildTree(tree), (p) => p === "Projects", "Inbox");

  test("no draft, no change", () => {
    expect(withDraft(rows, null)).toBe(rows);
  });

  test("the vault root's row comes first", () => {
    const out = withDraft(rows, { parent: "" });
    expect(out[0]).toEqual({ kind: "draft", parent: "", depth: 0 });
    expect(out.length).toBe(rows.length + 1);
  });

  test("a folder's row sits right under it, one level in", () => {
    const out = withDraft(rows, { parent: "Projects" });
    const i = out.findIndex((r) => r.kind === "folder" && r.node.path === "Projects");
    expect(out[i + 1]).toEqual({ kind: "draft", parent: "Projects", depth: 1 });
    // Before the folder's own subfolders and notes.
    expect(out[i + 2]).toMatchObject({ kind: "folder", node: { path: "Projects/Ideas" } });
  });

  test("a parent that is not on screen adds nothing", () => {
    expect(withDraft(rows, { parent: "Projects/Ideas/Deep" })).toEqual(rows);
  });
});

describe("starting and cancelling", () => {
  beforeEach(() => {
    cancelNewFolder();
    useUI.setState({ expanded: { "section:folders": false } });
  });

  test("opens the Folders section and the way down to the parent", () => {
    startNewFolder("Projects/Ideas");
    expect(useFolderDraft.getState().draft).toEqual({ parent: "Projects/Ideas" });
    const e = useUI.getState().expanded;
    expect(e["section:folders"]).toBe(true);
    expect(e["folder:Projects"]).toBe(true);
    expect(e["folder:Projects/Ideas"]).toBe(true);
    cancelNewFolder();
    expect(useFolderDraft.getState().draft).toBeNull();
  });
});

describe("the row and ⌘K create through the same action", () => {
  beforeEach(() => {
    toast.mockClear();
    toast.error.mockClear();
    toast.success.mockClear();
  });

  test("⌘K still offers Add folder…", () => {
    const r = createRegistry();
    registerBuiltins(r);
    expect(r.command("folder.add")?.title).toBe("Add folder…");
    expect(r.command("note.newInFolder")?.title).toBe("New note in folder…");
  });

  test("inline: an error goes to the row, not a toast", async () => {
    const client = {
      createFolder: vi.fn().mockRejectedValue(new Error("Work already exists")),
    } as unknown as NotesClient;
    const onError = vi.fn();
    const made = await addFolder(client, new QueryClient(), "Work", { onError });
    expect(made).toBeUndefined();
    expect(onError).toHaveBeenCalledWith("Work already exists");
    expect(toast.error).not.toHaveBeenCalled();
  });

  test("inline: success shows the folder, without a toast", async () => {
    const createFolder = vi.fn().mockResolvedValue("Projects/Drafts");
    const client = { createFolder } as unknown as NotesClient;
    const made = await addFolder(client, new QueryClient(), "Projects/Drafts", {
      onError: vi.fn(),
    });
    expect(made).toBe("Projects/Drafts");
    expect(createFolder).toHaveBeenCalledWith("Projects/Drafts");
    expect(toast.success).not.toHaveBeenCalled();
    expect(useUI.getState().expanded["folder:Projects/Drafts"]).toBe(true);
  });

  test("palette: the same action toasts", async () => {
    const client = { createFolder: vi.fn().mockResolvedValue("Later") } as unknown as NotesClient;
    await addFolder(client, new QueryClient(), "Later");
    expect(toast.success).toHaveBeenCalledWith("Created Later");
  });
});

describe("accessible names", () => {
  const html = (node: ReactNode) => renderToStaticMarkup(<TooltipProvider>{node}</TooltipProvider>);

  test("row buttons say what they do and where", () => {
    const out = html(
      <FolderRowActions name="Projects" onNewNote={() => {}} onNewFolder={() => {}} />,
    );
    expect(out).toContain('aria-label="New note in Projects"');
    expect(out).toContain('aria-label="New folder in Projects"');
    expect(out.match(/<button type="button"/g)?.length).toBe(2);
  });

  test("the name field is labelled, and its error is announced and linked", () => {
    const ok = html(<FolderNameField depth={0} value="" error="" onChange={() => {}} />);
    expect(ok).toContain('aria-label="New folder name"');
    expect(ok).not.toContain("aria-invalid");
    expect(ok).not.toContain('role="alert"');

    const bad = html(
      <FolderNameField
        depth={1}
        value="a/b"
        error="Use a single folder name, without slashes."
        onChange={() => {}}
      />,
    );
    expect(bad).toContain('aria-invalid="true"');
    expect(bad).toContain('aria-describedby="new-folder-error"');
    expect(bad).toMatch(/<p id="new-folder-error" role="alert"[^>]*>Use a single folder name/);
  });
});
