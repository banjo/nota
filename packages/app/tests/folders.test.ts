import type { NoteInfo, NotesClient, Tree } from "@nota/client";
import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, test, vi } from "vite-plus/test";
import {
  addFolder,
  addFolderThenNote,
  createFolderAndMove,
  createNoteIn,
  fileNote,
} from "../src/actions.ts";
import { registerBuiltins } from "../src/builtins/index.tsx";
import {
  baseFolder,
  choiceLabel,
  cleanFolderInput,
  folderChoices,
  readRecentFolders,
  RECENT_KEY,
  rememberFolder,
  resolveNewFolder,
} from "../src/lib/folders.ts";
import { createRegistry, type CommandContext } from "../src/registry.ts";
import { useUI } from "../src/state.ts";

vi.mock("@nota/ui/sonner", () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }),
}));

function memStore(initial: Record<string, string> = {}) {
  const m = new Map(Object.entries(initial));
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
  };
}

const tree: Pick<Tree, "inbox" | "folders"> = {
  inbox: "Inbox",
  folders: ["Archive", "Inbox", "Projects", "Projects/Ideas", "Projects/Nota", "Recipes"].map(
    (p) => ({ path: p, name: p.split("/").pop()!, notes: p === "Archive" ? 0 : 1 }),
  ),
};

describe("clean names", () => {
  test("cleanFolderInput", () => {
    expect(cleanFolderInput("  /Projects//Ideas\\2027/ ")).toBe("Projects/Ideas/2027");
    expect(cleanFolderInput("Mo\u0308te")).toBe("Möte");
    expect(cleanFolderInput(" / ")).toBe("");
  });
  test("resolveNewFolder is relative to the selected folder; a leading / starts at the root", () => {
    expect(resolveNewFolder("Ideas", "Projects")).toBe("Projects/Ideas");
    expect(resolveNewFolder("a/b", "")).toBe("a/b");
    expect(resolveNewFolder("/Ideas", "Projects")).toBe("Ideas");
    expect(resolveNewFolder("   ", "Projects")).toBe("");
  });
  test("baseFolder: where the note or list on screen is, never the inbox", () => {
    expect(baseFolder({ kind: "note", path: "Projects/Nota/Plan.md" }, "Inbox")).toBe(
      "Projects/Nota",
    );
    expect(baseFolder({ kind: "note", path: "Plan.md" }, "Inbox")).toBe("");
    expect(baseFolder({ kind: "list", list: { type: "folder", path: "Recipes" } }, "Inbox")).toBe(
      "Recipes",
    );
    expect(baseFolder({ kind: "note", path: "Inbox/Idea.md" }, "Inbox")).toBe("");
    expect(baseFolder({ kind: "inbox" }, "Inbox")).toBe("");
    expect(baseFolder({ kind: "list", list: { type: "tag" } }, "Inbox")).toBe("");
  });
});

describe("folder choices", () => {
  test("no query: suggested, then recent, then the rest; the note's own folder is left out", () => {
    const c = folderChoices({
      tree,
      query: "",
      from: "Inbox",
      recent: ["Recipes", "Gone", "Projects"],
      suggestion: { folder: "Projects/Nota", reason: "Tagged #nota" },
    });
    expect(c.suggested).toEqual({ folder: "Projects/Nota", reason: "Tagged #nota" });
    expect(c.recent).toEqual(["Recipes", "Projects"]); // a deleted folder is skipped
    expect(c.rest).toEqual(["/", "Archive", "Projects/Ideas"]);
    expect(c.create).toBeUndefined();
  });

  test("a note in the root can't be moved to the root", () => {
    const c = folderChoices({ tree, query: "", from: "" });
    expect(c.rest[0]).toBe("Archive");
    expect(c.rest).not.toContain("/");
  });

  test("a note being created may go to the root", () => {
    expect(folderChoices({ tree, query: "" }).rest[0]).toBe("/");
  });

  test("the query filters with accents and case folded; suggested only when it matches", () => {
    const c = folderChoices({
      tree,
      query: "IDE",
      from: "Inbox",
      suggestion: { folder: "Recipes", reason: "x" },
    });
    expect(c.suggested).toBeUndefined();
    expect(c.rest).toEqual(["Projects/Ideas"]);
    expect(folderChoices({ tree, query: "root", from: "Archive" }).rest).toEqual(["/"]);
  });

  test("an unknown name offers Create; an existing one (any case) does not", () => {
    expect(folderChoices({ tree, query: " Projects/New Ideas/ ", from: "Inbox" }).create).toBe(
      "Projects/New Ideas",
    );
    expect(folderChoices({ tree, query: "projects/IDEAS", from: "Inbox" }).create).toBeUndefined();
    expect(folderChoices({ tree, query: "Projects", from: "Inbox" }).create).toBeUndefined();
    expect(folderChoices({ tree, query: "", from: "Inbox" }).create).toBeUndefined();
  });

  test("labels", () => {
    expect(choiceLabel("/")).toBe("Vault root");
    expect(choiceLabel("Projects/Ideas")).toBe("Projects / Ideas");
  });
});

describe("recent folders", () => {
  test("newest first, deduplicated, at most five, root ignored", () => {
    const s = memStore();
    for (const f of ["a", "b", "c", "d", "e", "f", "c"]) rememberFolder(f, s);
    rememberFolder("/", s);
    expect(readRecentFolders(s)).toEqual(["c", "f", "e", "d", "b"]);
    expect(s.getItem(RECENT_KEY)).toBe('["c","f","e","d","b"]');
  });
  test("broken storage reads as empty", () => {
    expect(readRecentFolders(memStore({ [RECENT_KEY]: "{oops" }))).toEqual([]);
    expect(readRecentFolders(memStore({ [RECENT_KEY]: '{"a":1}' }))).toEqual([]);
    expect(readRecentFolders(undefined)).toEqual([]);
  });
});

// Palette flows: the actions each row runs, against a fake client.

function fakeClient(opts: { refuse?: string } = {}) {
  const calls: string[] = [];
  const client = {
    kind: "desktop",
    createFolder: vi.fn(async (p: string) => {
      calls.push(`createFolder ${p}`);
      if (opts.refuse) throw new Error(opts.refuse);
      return p;
    }),
    move: vi.fn(async (path: string, folder: string) => {
      calls.push(`move ${path} -> ${folder}`);
      return { from: path, path: `${folder}/${path.split("/").pop()}` };
    }),
    create: vi.fn(async (input: { folder?: string; title?: string }) => {
      calls.push(`create ${input.folder}`);
      return { path: `${input.folder}/Untitled.md`, folder: input.folder } as unknown as NoteInfo;
    }),
  } as unknown as NotesClient;
  return { client, calls };
}

describe("palette flows", () => {
  beforeEach(() => {
    const store = memStore();
    vi.stubGlobal("localStorage", store);
    vi.stubGlobal("requestAnimationFrame", () => 0);
    vi.stubGlobal("document", { dispatchEvent: () => true });
    useUI.setState({ route: { kind: "home" }, expanded: {} });
  });

  test("pick: moves the note and remembers the folder", async () => {
    const { client, calls } = fakeClient();
    const r = await fileNote(client, new QueryClient(), "Inbox/Idea.md", "Projects/Nota");
    expect(calls).toEqual(["move Inbox/Idea.md -> Projects/Nota"]);
    expect(r?.path).toBe("Projects/Nota/Idea.md");
    expect(readRecentFolders()).toEqual(["Projects/Nota"]);
  });

  test("create and move: makes the folder first, then moves, in one step", async () => {
    const { client, calls } = fakeClient();
    await createFolderAndMove(client, new QueryClient(), "Inbox/Idea.md", "Projects/Ideas/2027");
    expect(calls).toEqual([
      "createFolder Projects/Ideas/2027",
      "move Inbox/Idea.md -> Projects/Ideas/2027",
    ]);
    expect(readRecentFolders()).toEqual(["Projects/Ideas/2027"]);
  });

  test("create and move: a refused folder moves nothing", async () => {
    const { client, calls } = fakeClient({ refuse: "differs only in case" });
    const r = await createFolderAndMove(client, new QueryClient(), "Inbox/Idea.md", "projects");
    expect(r).toBeUndefined();
    expect(calls).toEqual(["createFolder projects"]);
    expect(readRecentFolders()).toEqual([]);
  });

  test("add folder: creates it and opens its parents in the tree", async () => {
    const { client, calls } = fakeClient();
    const p = await addFolder(client, new QueryClient(), "Projects/Ideas");
    expect(p).toBe("Projects/Ideas");
    expect(calls).toEqual(["createFolder Projects/Ideas"]);
    expect(useUI.getState().expanded).toMatchObject({
      "folder:Projects": true,
      "folder:Projects/Ideas": true,
    });
  });

  test("add folder: a refused name changes nothing", async () => {
    const { client } = fakeClient({ refuse: "already exists" });
    expect(await addFolder(client, new QueryClient(), "Projects")).toBeUndefined();
    expect(useUI.getState().expanded).toEqual({});
  });

  test("new note in folder: picks, creates there, remembers", async () => {
    const { client, calls } = fakeClient();
    await createNoteIn(client, new QueryClient(), "Recipes");
    expect(calls).toEqual(["create Recipes"]);
    expect(readRecentFolders()).toEqual(["Recipes"]);
    expect(useUI.getState().route).toEqual({ kind: "note", path: "Recipes/Untitled.md" });
  });

  test("new note in a new folder", async () => {
    const { client, calls } = fakeClient();
    await addFolderThenNote(client, new QueryClient(), "Journal/2027");
    expect(calls).toEqual(["createFolder Journal/2027", "create Journal/2027"]);
  });
});

describe("palette commands", () => {
  test("folder actions are commands, found by folder / add / move / mkdir", () => {
    const r = createRegistry();
    registerBuiltins(r);
    const find = (q: string) =>
      r
        .get("command")
        .filter((c) => c.title.toLowerCase().includes(q) || (c.keywords ?? []).includes(q))
        .map((c) => c.id);
    expect(find("folder")).toEqual(
      expect.arrayContaining(["note.addToFolder", "note.newInFolder", "folder.add"]),
    );
    expect(find("add")).toEqual(expect.arrayContaining(["note.addToFolder", "folder.add"]));
    expect(find("move")).toContain("note.addToFolder");
    expect(find("mkdir")).toEqual(["folder.add"]);
    expect(r.command("note.move")).toBeUndefined();
    expect(r.command("note.addToFolder")?.shortcut).toBe("mod+shift+m");
  });

  test("Add to folder needs a note; the others are always there", () => {
    const r = createRegistry();
    registerBuiltins(r);
    const ctx = (notePath?: string) => ({ notePath }) as CommandContext;
    expect(r.command("note.addToFolder")?.when?.(ctx())).toBe(false);
    expect(r.command("note.addToFolder")?.when?.(ctx("A.md"))).toBe(true);
    expect(r.command("folder.add")?.when).toBeUndefined();
    expect(r.command("note.newInFolder")?.when).toBeUndefined();
  });

  test("each command opens its palette mode", async () => {
    const r = createRegistry();
    registerBuiltins(r);
    const ui = useUI.getState();
    await r.command("note.addToFolder")!.run({ notePath: "Inbox/A.md" } as CommandContext);
    expect(useUI.getState().palette).toMatchObject({
      open: true,
      mode: "move",
      path: "Inbox/A.md",
    });
    ui.closePalette();
    await r.command("note.newInFolder")!.run({} as CommandContext);
    expect(useUI.getState().palette).toMatchObject({ open: true, mode: "noteIn" });
    const tree = { inbox: "Inbox", folders: [], notes: [], cursor: 0 };
    await r.command("folder.add")!.run({
      route: { kind: "note", path: "Projects/Plan.md" },
      queryClient: new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } }),
      client: { tree: async () => tree },
    } as unknown as CommandContext);
    expect(useUI.getState().palette).toMatchObject({
      open: true,
      mode: "newFolder",
      path: "Projects",
    });
  });
});
