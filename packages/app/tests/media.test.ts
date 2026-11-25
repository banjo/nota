import type { Note, NotesClient, Tree } from "@nota/client";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, test, vi } from "vite-plus/test";
import { noteHandlers } from "../src/editorHandlers.ts";
import { attachmentName, relativeHref, vaultPath } from "../src/lib/notes.ts";

const toast = vi.hoisted(() => Object.assign(vi.fn(), { error: vi.fn(), warning: vi.fn() }));
vi.mock("@nota/ui/sonner", () => ({ toast }));

describe("vaultPath", () => {
  test("resolves a link against the note's folder, decoded, inside the vault", () => {
    const from = "Projekt/Nota/Plan.md";
    expect(vaultPath("bild.png", from)).toEqual({ path: "Projekt/Nota/bild.png" });
    expect(vaultPath("../../Attachments/Pasted%20image%201.png", from)).toEqual({
      path: "Attachments/Pasted image 1.png",
    });
    expect(vaultPath("/Attachments/a.png#x", from)).toEqual({
      path: "Attachments/a.png",
      hash: "x",
    });
    expect(vaultPath("<mapp/med mellanslag.png>", "A.md")).toEqual({
      path: "mapp/med mellanslag.png",
    });
    for (const href of [
      "https://x.se/a.png",
      "data:image/png;base64,AA",
      "//cdn/a.png",
      "#rubrik",
      "../../../ut.png",
      "mapp/",
    ])
      expect(vaultPath(href, from), href).toBeNull();
  });
});

describe("relativeHref", () => {
  test("links a file from a note with a relative, encoded path", () => {
    expect(relativeHref("Projekt/Nota/Plan.md", "Attachments/Pasted image 1.png")).toBe(
      "../../Attachments/Pasted%20image%201.png",
    );
    expect(relativeHref("Plan.md", "Attachments/a.png")).toBe("Attachments/a.png");
    expect(relativeHref("Attachments/Om.md", "Attachments/Möte.png")).toBe("M%C3%B6te.png");
    // Round trip: the link resolves back to the file.
    const from = "a/b/Note.md";
    expect(vaultPath(relativeHref(from, "a/c/x y.png"), from)).toEqual({ path: "a/c/x y.png" });
  });
});

describe("attachmentName", () => {
  const now = new Date(2026, 10, 25, 14, 30, 12);
  test("keeps a dropped file's name; a pasted one is named like Obsidian's", () => {
    expect(attachmentName({ name: "skiss.png", type: "image/png" }, now)).toBe("skiss.png");
    expect(attachmentName({ name: "image.png", type: "image/png" }, now)).toBe(
      "Pasted image 20261125143012.png",
    );
    expect(attachmentName({ name: "", type: "image/jpeg" }, now)).toBe(
      "Pasted image 20261125143012.jpg",
    );
  });
});

const note = (path: string, body: string): Note => ({
  path,
  folder: path.split("/").slice(0, -1).join("/"),
  title: body.split("\n")[0]!.replace(/^#\s*/, ""),
  tags: [],
  properties: {},
  modified: "",
  size: 0,
  version: 1,
  hash: "",
  body,
  content: body,
  backlinks: [],
  links: [],
});

function fakeClient(over: Partial<NotesClient> = {}) {
  const notes = [
    note("Projekt/Plan.md", "# Plan\n\nText"),
    note("Arkiv/Mål.md", "# Mål\n\nBli klara."),
  ];
  const tree: Tree = { inbox: "Inbox", folders: [], notes, cursor: 0 };
  return {
    kind: "desktop",
    tree: async () => tree,
    get: async (p: string) => notes.find((n) => n.path === p)!,
    fileUrl: (p: string, from?: string) => `notafile://vault/${p}${from ? `?from=${from}` : ""}`,
    attach: vi.fn(async (name: string) => `Attachments/${name}`),
    ...over,
  } as unknown as NotesClient;
}

describe("noteHandlers", () => {
  test("resolve images from the note: paths against its folder, ![[names]] by the backend", () => {
    const h = noteHandlers(fakeClient(), new QueryClient(), "Projekt/Plan.md");
    expect(h.resolveAsset!("../Attachments/a%20b.png", false)).toBe(
      "notafile://vault/Attachments/a b.png",
    );
    expect(h.resolveAsset!("a.png", true)).toBe("notafile://vault/a.png?from=Projekt/Plan.md");
    expect(h.resolveAsset!("https://x.se/a.png", false)).toBeNull();
  });

  test("leave images as links where the client can't show files", () => {
    const h = noteHandlers(
      fakeClient({ fileUrl: undefined, attach: undefined }),
      new QueryClient(),
      "A.md",
    );
    expect(h.resolveAsset).toBeUndefined();
    expect(h.onAttach).toBeUndefined();
  });

  test("save a pasted image to Attachments and link it relatively", async () => {
    const attach = vi.fn(async (name: string) => `Attachments/${name}`);
    const h = noteHandlers(fakeClient({ attach }), new QueryClient(), "Projekt/Plan.md");
    const file = new File(["PNG"], "image.png", { type: "image/png" });
    const md = await h.onAttach!(file);
    expect(md).toMatch(/^!\[\]\(\.\.\/Attachments\/Pasted%20image%20\d{14}\.png\)$/);
    expect(attach).toHaveBeenCalledWith(expect.stringMatching(/^Pasted image \d{14}\.png$/), file);
  });

  test("a failed upload inserts nothing and says why", async () => {
    const client = fakeClient({
      attach: vi.fn(async () => Promise.reject(new Error("too large"))),
    });
    const h = noteHandlers(client, new QueryClient(), "Plan.md");
    expect(await h.onAttach!(new File(["x"], "a.png", { type: "image/png" }))).toBeNull();
    expect(toast.error).toHaveBeenCalledWith("Couldn't save a.png", { description: "too large" });
  });

  test("embed a note without its title, a section with it, one level deep", async () => {
    const h = noteHandlers(fakeClient(), new QueryClient(), "Projekt/Plan.md");
    const whole = await h.embedNote!("Mål");
    expect(whole).toMatchObject({ path: "Arkiv/Mål.md", title: "Mål", body: "Bli klara." });
    expect(whole!.handlers.embedNote).toBeUndefined();
    expect(whole!.handlers.onAttach).toBeUndefined();
    expect(whole!.handlers.resolveAsset!("x.png", false)).toBe("notafile://vault/Arkiv/x.png");
    expect((await h.embedNote!("Mål", "Mål"))!.body).toBe("# Mål\n\nBli klara.");
    expect(await h.embedNote!("Saknas")).toBeNull();
    expect(await h.embedNote!("Plan")).toBeNull(); // itself
  });
});
