import type { NoteInfo, Tree } from "@nota/client";
import { describe, expect, test } from "vite-plus/test";
import {
  attachmentsRow,
  buildTree,
  filename,
  flattenTree,
  joinTitle,
  noteLink,
  resolveLink,
  splitTitle,
} from "../src/lib/notes.ts";
import { emptyValue, parseValue, propType, validKey } from "../src/lib/properties.ts";
import { label, matches } from "../src/lib/shortcuts.ts";
import { relativeTime } from "../src/lib/format.ts";

const info = (path: string, title = path.split("/").pop()!.replace(/\.md$/, "")): NoteInfo => ({
  path,
  folder: path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "",
  title,
  tags: [],
  properties: {},
  modified: "2026-11-22T10:00:00Z",
  size: 1,
});

describe("title split", () => {
  test.each([
    "# Sync-protokoll\n\nFilnivå-sync med 3-way merge.\n",
    "Ringa elbolaget om fakturan",
    "Ringa elbolaget om fakturan\n",
    "## Rubrik två\nDirekt text",
    "",
    "\n\nText utan titel",
    "# Titel\n\n\n\nLångt ner\n",
    "\n# Hejsan gänget\n\nText",
    "  \n\t\nTitel efter blanka rader",
    "\n\n",
  ])("round-trips %j", (body) => {
    expect(joinTitle(splitTitle(body))).toBe(body);
  });
  test("separates the heading marker", () => {
    expect(splitTitle("# Sync-protokoll\n\nText")).toEqual({
      lead: "",
      prefix: "# ",
      title: "Sync-protokoll",
      sep: "\n",
      rest: "Text",
      newline: true,
    });
  });
  test("skips blank lines before the title, as vault.Title does", () => {
    expect(splitTitle("\nHejsan gänget,\n\nNu är bokningen gjord!")).toEqual({
      lead: "\n",
      prefix: "",
      title: "Hejsan gänget,",
      sep: "\n",
      rest: "Nu är bokningen gjord!",
      newline: true,
    });
  });
  test("a renamed title keeps the blank lines before it", () => {
    expect(joinTitle({ ...splitTitle("\n# Gammal\n\nText"), title: "Ny" })).toBe("\n# Ny\n\nText");
  });
  test("a first line of text under a bare title gets a blank line", () => {
    expect(joinTitle({ ...splitTitle("Ringa elbolaget"), rest: "349 kr" })).toBe(
      "Ringa elbolaget\n\n349 kr",
    );
  });
  test("file names follow vault.Filename", () => {
    expect(filename("Futsal: testa 2-2 i powerplay")).toBe("Futsal testa 2-2 i powerplay");
    expect(filename("  ..Möte / plan?  ")).toBe("Möte plan");
    expect(filename("###")).toBe("Untitled");
  });
});

describe("tree", () => {
  const tree: Tree = {
    inbox: "Inbox",
    cursor: 0,
    folders: [
      { path: "Inbox", name: "Inbox", notes: 1 },
      { path: "Projekt", name: "Projekt", notes: 2 },
      { path: "Projekt/Nota", name: "Nota", notes: 2 },
      { path: "Fotboll", name: "Fotboll", notes: 1 },
    ],
    notes: [
      info("Inbox/Idé.md"),
      info("Projekt/Nota/Sync-protokoll.md"),
      info("Projekt/Nota/CLI-kommandon.md"),
      info("Fotboll/Junior.md"),
      info("Välkommen.md"),
    ],
  };
  test("nests folders and sorts them by name", () => {
    const root = buildTree(tree);
    expect(root.folders.map((f) => f.name)).toEqual(["Fotboll", "Inbox", "Projekt"]);
    expect(root.folders[2]!.folders[0]!.notes.map((n) => n.title)).toEqual([
      "CLI-kommandon",
      "Sync-protokoll",
    ]);
    expect(root.notes.map((n) => n.title)).toEqual(["Välkommen"]);
  });
  test("gives the attachments folder its own row unless it holds notes", () => {
    const t = (notes: number): Tree => ({
      inbox: "Inbox",
      attachments: "Attachments",
      folders: [{ path: "Attachments", name: "Attachments", notes }],
      notes: [],
      cursor: 0,
    });
    expect(attachmentsRow(t(0))).toBe("Attachments");
    expect(attachmentsRow(t(1))).toBeUndefined();
    expect(attachmentsRow({ ...t(0), attachments: "" })).toBeUndefined();
    expect(attachmentsRow({ ...t(0), attachments: undefined })).toBeUndefined();
    const rows = flattenTree(buildTree(t(0)), () => false, "Inbox", "Attachments");
    expect(rows).toEqual([]);
  });
  test("flattens the open folders, leaving the inbox out", () => {
    const open = new Set(["Projekt", "Projekt/Nota"]);
    const rows = flattenTree(buildTree(tree), (p) => open.has(p), "Inbox");
    expect(
      rows.map((r) =>
        r.kind === "folder" ? `${r.depth}:${r.node.name}/` : `${r.depth}:${r.note.title}`,
      ),
    ).toEqual([
      "0:Fotboll/",
      "0:Projekt/",
      "1:Nota/",
      "2:CLI-kommandon",
      "2:Sync-protokoll",
      "0:Välkommen",
    ]);
  });
  test("resolves wikilinks by path, name and title, closest first", () => {
    const notes = [
      info("Projekt/Plan.md"),
      info("Hem/Plan.md"),
      info("Recept/Pasta alla vodka.md", "Pasta alla vodka"),
    ];
    expect(resolveLink("Hem/Plan", notes)?.path).toBe("Hem/Plan.md");
    expect(resolveLink("plan", notes, "Hem/Julklappar.md")?.path).toBe("Hem/Plan.md");
    expect(resolveLink("Pasta alla vodka", notes)?.path).toBe("Recept/Pasta alla vodka.md");
    expect(resolveLink("Saknas", notes)).toBeUndefined();
  });
});

describe("properties", () => {
  test("infer editor types", () => {
    expect(
      [true, 3, ["a"], "2026-11-18", "2026-11-18T10:12:00+01:00", "utkast"].map(propType),
    ).toEqual(["bool", "number", "list", "date", "date", "text"]);
    expect(parseValue("number", "2,5")).toBe(2.5);
    expect(parseValue("number", "x")).toBeUndefined();
    expect(parseValue("list", "a, b,,c")).toEqual(["a", "b", "c"]);
    expect(emptyValue("bool")).toBe(false);
    expect(validKey("prio")).toBe(true);
    expect(validKey("bad: key")).toBe(false);
  });
});

describe("shortcuts", () => {
  const key = (k: string, code: string, mods: Partial<KeyboardEvent> = {}) =>
    ({
      key: k,
      code,
      metaKey: false,
      ctrlKey: false,
      shiftKey: false,
      altKey: false,
      ...mods,
    }) as KeyboardEvent;
  test("mod is ⌘ on macOS and Ctrl elsewhere", () => {
    expect(matches(key("k", "KeyK", { metaKey: true }), "mod+k", true)).toBe(true);
    expect(matches(key("k", "KeyK", { ctrlKey: true }), "mod+k", true)).toBe(false);
    expect(matches(key("k", "KeyK", { ctrlKey: true }), "mod+k", false)).toBe(true);
    expect(matches(key("I", "KeyI", { ctrlKey: true, shiftKey: true }), "mod+shift+i", false)).toBe(
      true,
    );
    expect(matches(key("\\", "Backslash", { ctrlKey: true }), "mod+\\", false)).toBe(true);
    expect(matches(key("n", "KeyN", { ctrlKey: true, shiftKey: true }), "mod+n", false)).toBe(
      false,
    );
  });
  test("labels", () => {
    expect(label("mod+shift+i", true)).toBe("⌘⇧I");
    expect(label("mod+k", false)).toBe("Ctrl+K");
  });
});

describe("relative time", () => {
  const now = new Date(2026, 10, 22, 12, 0);
  test("today, yesterday, this year, earlier", () => {
    expect(relativeTime(new Date(2026, 10, 22, 10, 42).toISOString(), now)).toBe("Today 10:42");
    expect(relativeTime(new Date(2026, 10, 21, 9, 5).toISOString(), now)).toBe("Yesterday 09:05");
    expect(relativeTime(new Date(2026, 10, 18).toISOString(), now)).toBe("18 Nov");
    expect(relativeTime(new Date(2025, 0, 2).toISOString(), now)).toBe("2 Jan 2025");
  });
});

describe("noteLink", () => {
  test("resolves like the index: against the note's folder, decoded, inside the vault", () => {
    const from = "Projekt/Nota/Plan.md";
    expect(noteLink("Annan.md", from)).toEqual({ target: "Projekt/Nota/Annan.md" });
    expect(noteLink("./sub/M%C3%B6te%20i%20dag.md#M%C3%A5l", from)).toEqual({
      target: "Projekt/Nota/sub/Möte i dag.md",
      heading: "M%C3%A5l",
    });
    expect(noteLink("../../Inbox.md#^abc", from)).toEqual({ target: "Inbox.md", heading: "^abc" });
    expect(noteLink("/Arkiv/Gammal.MD", from)).toEqual({ target: "Arkiv/Gammal.MD" });
    expect(noteLink("<Med mellanslag.md>", "A.md")).toEqual({ target: "Med mellanslag.md" });
  });

  test("is null for web links, anchors, other files and paths out of the vault", () => {
    for (const href of [
      "https://x.se/a.md",
      "mailto:a@b.se",
      "#rubrik",
      "//cdn/x.md",
      "bild.png",
      "mapp/",
      "../../../ut.md",
    ])
      expect(noteLink(href, "Projekt/Nota/Plan.md"), href).toBeNull();
  });
});
