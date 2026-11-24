import type { ExternalDoc, NotesClient, Opened, OpenerApi } from "@nota/client";
import { TooltipProvider } from "@nota/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import { ClientContext } from "../src/context.ts";
import { addPreview, displayPath, linkKind, nextPreview } from "../src/lib/opened.ts";
import { persistedRoute, useUI } from "../src/state.ts";

const toast = vi.hoisted(() =>
  Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn(), warning: vi.fn() }),
);
vi.mock("@nota/ui/sonner", () => ({ toast }));

const ext = await import("../src/external.ts");
const { showOpened } = await import("../src/components/OpenedFiles.tsx");
const { PreviewBanner } = await import("../src/components/ExternalPage.tsx");

const preview = (id: string, name = `${id}.md`): Opened => ({
  id,
  kind: "external",
  name,
  dir: "~/Downloads",
});

const doc = (over: Partial<ExternalDoc> = {}): ExternalDoc => ({
  id: "x1",
  name: "README.md",
  dir: "~/Downloads",
  title: "Read me",
  content: "# Read me\n",
  body: "# Read me\n",
  properties: {},
  modified: "2026-11-24T10:00:00Z",
  size: 10,
  hash: "h1",
  utf8: true,
  ...over,
});

function fakeOpener(over: Partial<OpenerApi> = {}) {
  return {
    take: vi.fn(async () => []),
    previews: vi.fn(async () => []),
    read: vi.fn(async () => doc()),
    save: vi.fn(async () => ({ conflict: false, path: "Inbox/README.md" })),
    overwriteOriginal: vi.fn(async () => ({ conflict: false })),
    reveal: vi.fn(async () => {}),
    dismiss: vi.fn(async () => {}),
    follow: vi.fn(async () => preview("x2")),
    pick: vi.fn(async () => {}),
    onOpened: () => () => {},
    onChanged: () => () => {},
    ...over,
  } satisfies OpenerApi;
}

function setup(over: Partial<OpenerApi> = {}) {
  const opener = fakeOpener(over);
  const client = { kind: "desktop", opener } as unknown as NotesClient;
  const qc = new QueryClient();
  qc.setQueryData(ext.externalKey("x1"), doc());
  return { client, qc, opener };
}

beforeEach(() => {
  vi.clearAllMocks();
  ext.useOpened.setState({ previews: [], drafts: {}, confirm: null });
  useUI.setState({ route: { kind: "home" }, noteMode: "live" });
});

describe("the preview list", () => {
  test("moves a reopened file to the end and drops the oldest past the limit", () => {
    let list: Opened[] = [];
    for (const id of ["a", "b", "c"]) list = addPreview(list, preview(id), () => false, 3).list;
    expect(addPreview(list, preview("a"), () => false, 3).list.map((x) => x.id)).toEqual([
      "b",
      "c",
      "a",
    ]);
    const r = addPreview(list, preview("d"), () => false, 3);
    expect(r.list.map((x) => x.id)).toEqual(["b", "c", "d"]);
    expect(r.dropped).toEqual(["a"]);
  });

  test("never drops a preview with a draft", () => {
    let list: Opened[] = [];
    for (const id of ["a", "b"]) list = addPreview(list, preview(id), () => false, 2).list;
    const r = addPreview(list, preview("c"), (id) => id === "a", 2);
    expect(r.list.map((x) => x.id)).toEqual(["a", "c"]);
    expect(r.dropped).toEqual(["b"]);
  });

  test("closing goes to the one opened before, else the next", () => {
    const list = ["a", "b", "c"].map((id) => preview(id));
    expect(nextPreview(list, "b")).toBe("a");
    expect(nextPreview(list, "a")).toBe("b");
    expect(nextPreview([preview("a")], "a")).toBeUndefined();
  });

  test("display paths and links", () => {
    expect(displayPath({ dir: "~/Downloads", name: "x.md" })).toBe("~/Downloads/x.md");
    expect(displayPath({ dir: "C:\\Users\\me", name: "x.md" })).toBe("C:\\Users\\me\\x.md");
    expect(linkKind("https://example.com")).toBe("web");
    expect(linkKind("mailto:a@b.c")).toBe("web");
    expect(linkKind("next.md#part")).toBe("file");
    expect(linkKind("../docs/Other.markdown")).toBe("file");
    for (const h of [
      "javascript:alert(1)",
      "file:///etc/x.md",
      "/etc/x.md",
      "x.png",
      "//evil/x.md",
    ])
      expect(linkKind(h)).toBe("blocked");
  });

  test("a preview's route is not kept between runs", () => {
    expect(persistedRoute({ kind: "external", id: "x1" })).toEqual({ kind: "home" });
  });
});

describe("opening outside files", () => {
  test("every one goes to the sidebar, the last one gets the page", () => {
    const { client } = setup();
    showOpened([preview("a"), preview("b")], client);
    expect(ext.useOpened.getState().previews.map((p) => p.id)).toEqual(["a", "b"]);
    expect(useUI.getState().route).toEqual({ kind: "external", id: "b" });
  });
});

describe("draft and save", () => {
  test("Edit makes a draft from the file; nothing is written", () => {
    const { qc, opener } = setup();
    ext.editPreview(qc, "x1");
    expect(ext.draftOf("x1")).toBe("# Read me\n");
    expect(ext.isDirty(qc, "x1")).toBe(false);
    ext.setDraft("x1", "# Read me\n\nmore");
    expect(ext.isDirty(qc, "x1")).toBe(true);
    ext.revertDraft(qc, "x1");
    expect(ext.draftOf("x1")).toBe("# Read me\n");
    expect(opener.save).not.toHaveBeenCalled();
    expect(opener.overwriteOriginal).not.toHaveBeenCalled();
  });

  test("a file that isn't UTF-8 can't be edited", () => {
    const { qc } = setup();
    qc.setQueryData(ext.externalKey("x1"), doc({ utf8: false }));
    ext.editPreview(qc, "x1");
    expect(ext.draftOf("x1")).toBeUndefined();
  });

  test("Save to vault sends the draft and opens the new note", async () => {
    const { client, qc, opener } = setup();
    showOpened([preview("x1", "README.md")], client);
    ext.setDraft("x1", "# Edited\n");
    await ext.saveToVault(client, qc, "x1", "Projects");
    expect(opener.save).toHaveBeenCalledWith("x1", {
      folder: "Projects",
      hash: "h1",
      body: "# Edited\n",
      move: false,
    });
    expect(useUI.getState().route).toEqual({ kind: "note", path: "Inbox/README.md" });
    expect(ext.useOpened.getState().previews).toEqual([]);
    expect(ext.draftOf("x1")).toBeUndefined();
    expect(toast.success.mock.calls[0]![1]).toMatchObject({
      description: "The original file is unchanged.",
    });
  });

  test("a preview without a draft saves the file as is", async () => {
    const { client, qc, opener } = setup();
    await ext.saveToVault(client, qc, "x1", "");
    expect(opener.save).toHaveBeenCalledWith("x1", {
      folder: "",
      hash: "h1",
      body: undefined,
      move: false,
    });
  });

  test("a conflict keeps the preview and the draft", async () => {
    const { client, qc } = setup({ save: vi.fn(async () => ({ conflict: true })) });
    showOpened([preview("x1")], client);
    ext.setDraft("x1", "mine");
    await ext.saveToVault(client, qc, "x1", "", true);
    expect(toast.warning).toHaveBeenCalled();
    expect(ext.draftOf("x1")).toBe("mine");
    expect(useUI.getState().route).toEqual({ kind: "external", id: "x1" });
  });

  test("Move asks first, then saves with move", async () => {
    const { client, qc, opener } = setup();
    showOpened([preview("x1", "README.md")], client);
    ext.confirmMove(client, qc, "x1", "Projects");
    const c = ext.useOpened.getState().confirm!;
    expect(c.body).toContain("~/Downloads/README.md");
    expect(c.body).toContain("Trash");
    expect(opener.save).not.toHaveBeenCalled();
    await c.run();
    expect(opener.save).toHaveBeenCalledWith("x1", expect.objectContaining({ move: true }));
  });

  test("Overwrite original asks, and is only offered with a draft", async () => {
    const { client, qc, opener } = setup();
    ext.confirmOverwrite(client, qc, "x1");
    expect(ext.useOpened.getState().confirm).toBeNull();
    ext.setDraft("x1", "new body");
    ext.confirmOverwrite(client, qc, "x1");
    const c = ext.useOpened.getState().confirm!;
    expect(c.destructive).toBe(true);
    await c.run();
    expect(opener.overwriteOriginal).toHaveBeenCalledWith("x1", "new body", "h1");
    expect(ext.draftOf("x1")).toBeUndefined();
  });

  test("closing a changed draft asks; an unchanged one just closes", () => {
    const { client, qc, opener } = setup();
    showOpened([preview("x1")], client);
    ext.setDraft("x1", "changed");
    ext.closePreview(client, qc, "x1");
    expect(ext.useOpened.getState().confirm?.action).toBe("Discard and close");
    expect(opener.dismiss).not.toHaveBeenCalled();
    ext.setDraft("x1", "# Read me\n");
    ext.closePreview(client, qc, "x1");
    expect(opener.dismiss).toHaveBeenCalledWith("x1");
    expect(useUI.getState().route).toEqual({ kind: "home" });
  });
});

describe("a previewed file that changes on disk", () => {
  test("the preview reads it again", async () => {
    const fresh = doc({ body: "new\n", hash: "h2" });
    const { client, qc } = setup({ read: vi.fn(async () => fresh) });
    await ext.fileChanged(client, qc, "x1");
    expect(qc.getQueryData(ext.externalKey("x1"))).toEqual(fresh);
    expect(toast).not.toHaveBeenCalled();
  });

  test("a draft is kept and says so", async () => {
    const { client, qc } = setup({ read: vi.fn(async () => doc({ body: "new\n", hash: "h2" })) });
    ext.setDraft("x1", "mine");
    await ext.fileChanged(client, qc, "x1");
    expect(ext.draftOf("x1")).toBe("mine");
    expect(toast).toHaveBeenCalledWith(
      "The original file changed on disk",
      expect.objectContaining({ description: expect.stringContaining("Revert") }),
    );
    ext.revertDraft(qc, "x1");
    expect(ext.draftOf("x1")).toBe("new\n");
  });

  test("a gone file keeps its text and the draft", async () => {
    const { client, qc } = setup({
      read: vi.fn(async (): Promise<ExternalDoc> => {
        throw new Error("gone");
      }),
    });
    ext.setDraft("x1", "mine");
    await ext.fileChanged(client, qc, "x1");
    expect(qc.getQueryData(ext.externalKey("x1"))).toEqual(doc());
    expect(ext.draftOf("x1")).toBe("mine");
    expect(toast.warning).toHaveBeenCalledWith("The original file is gone", expect.anything());
  });
});

describe("the banner", () => {
  function render(node: ReactNode) {
    const { client, qc } = setup();
    return renderToStaticMarkup(
      <QueryClientProvider client={qc}>
        <ClientContext.Provider value={{ ...client, shell: { platform: "darwin" } } as NotesClient}>
          <TooltipProvider>{node}</TooltipProvider>
        </ClientContext.Provider>
      </QueryClientProvider>,
    );
  }

  test("preview: where the file is, Edit, Save, Reveal in Finder, Close", () => {
    const html = render(<PreviewBanner doc={doc()} editing={false} dirty={false} />);
    expect(html).toContain("Preview of ~/Downloads/README.md");
    for (const s of ["Edit", "Save to vault…", "Reveal in Finder", "Close"])
      expect(html).toContain(s);
  });

  test("draft: unsaved changes, Revert and Discard", () => {
    const html = render(<PreviewBanner doc={doc()} editing dirty />);
    expect(html).toContain("Draft · unsaved changes");
    for (const s of ["Save to vault…", "Revert", "Discard"]) expect(html).toContain(s);
    expect(render(<PreviewBanner doc={doc()} editing dirty={false} />)).toContain(
      "Draft · no changes yet",
    );
  });

  test("gone: a draft can still be saved; a preview can only close", () => {
    const draft = render(<PreviewBanner doc={doc()} editing dirty gone />);
    expect(draft).toContain("~/Downloads/README.md is gone.");
    for (const s of ["Save to vault…", "Discard"]) expect(draft).toContain(s);
    const plain = render(<PreviewBanner doc={doc()} editing={false} dirty={false} gone />);
    expect(plain).toContain("is gone.");
    expect(plain).toContain("Close");
    expect(plain).not.toContain("Save to vault");
  });

  test("not UTF-8: no Edit, no Save", () => {
    const html = render(<PreviewBanner doc={doc({ utf8: false })} editing={false} dirty={false} />);
    expect(html).toContain("UTF-8 text");
    expect(html).not.toContain("Save to vault");
    expect(html).not.toContain(">Edit<");
  });
});
