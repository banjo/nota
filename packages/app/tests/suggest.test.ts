import type { NoteInfo, Tree } from "@nota/client";
import { describe, expect, test } from "vite-plus/test";
import { suggestFolders, words } from "../src/lib/suggest.ts";

const n = (
  path: string,
  title: string,
  tags: string[] = [],
  modified = "2026-11-20T10:00:00Z",
): NoteInfo => ({
  path,
  folder: path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "",
  title,
  tags,
  properties: {},
  modified,
  size: 1,
});

// The demo vault (scripts/demo-vault.sh).
const notes = [
  n("Projekt/Nota/Sync-protokoll.md", "Sync-protokoll", ["nota", "sync"]),
  n("Projekt/Nota/CLI-kommandon.md", "CLI-kommandon", ["nota", "cli"]),
  n("Projekt/Schackklubb/Bänkar i Göteborg.md", "Bänkar i Göteborg", ["schack"]),
  n("Fotboll/Junior träning tisdag.md", "Junior: träning tisdag", ["fotboll", "junior"]),
  n("Fotboll/Futsal december.md", "Futsal: december", ["fotboll", "futsal"]),
  n("Hem/Julklappar 2026.md", "Julklappar 2026", ["jul"]),
  n("Recept/Pasta alla vodka.md", "Pasta alla vodka", ["recept", "pasta"]),
  n("Recept/Kimchi-stekt ris.md", "Kimchi-stekt ris", ["recept", "snabbt"]),
];
const inbox = [
  n("Inbox/Dela en anteckning via länk.md", "Dela en anteckning via länk", ["idé", "nota"]),
  n("Inbox/Ringa elbolaget om fakturan.md", "Ringa elbolaget om fakturan", ["hem"]),
  n("Inbox/Futsal testa 2-2 i powerplay.md", "Futsal: testa 2-2 i powerplay", ["taktik"]),
  n("Inbox/Smash burgers med kimchi-mayo.md", "Smash burgers med kimchi-mayo"),
  n("Inbox/Köpa mjölk.md", "Köpa mjölk"),
];
const tree: Tree = {
  inbox: "Inbox",
  cursor: 0,
  folders: [
    "Fotboll",
    "Hem",
    "Inbox",
    "Projekt",
    "Projekt/Schackklubb",
    "Projekt/Nota",
    "Recept",
  ].map((p) => ({ path: p, name: p.split("/").pop()!, notes: 1 })),
  notes: [...notes, ...inbox],
};
const top = (i: number) => suggestFolders(inbox[i]!, tree)[0];

describe("inbox suggestions", () => {
  test("a tag naming a folder wins, nested folders too", () => {
    expect(top(0)).toMatchObject({ folder: "Projekt/Nota", reason: "Tagged #nota" });
    expect(top(1)).toMatchObject({ folder: "Hem", reason: "Tagged #hem" });
  });
  test("a similar title points at its folder", () => {
    expect(top(2)).toMatchObject({ folder: "Fotboll", reason: "Like “Futsal: december”" });
    expect(top(3)).toMatchObject({ folder: "Recept", reason: "Like “Kimchi-stekt ris”" });
  });
  test("nothing to go on: no suggestion", () => {
    expect(suggestFolders(inbox[4]!, tree)).toEqual([]);
  });
  test("other notes with the same tag", () => {
    const note = n("Inbox/Bänk i Slottsskogen.md", "Bänk vid dammen", ["schack-bänk", "fika"]);
    const tagged = n("Hem/Fika.md", "Fika", ["fika"]);
    expect(suggestFolders(note, { ...tree, notes: [...tree.notes, tagged] })[0]).toMatchObject({
      folder: "Hem",
    });
  });
  test("words drop short and common ones", () => {
    expect(words("Futsal: testa 2-2 i powerplay")).toEqual(["futsal", "testa", "powerplay"]);
    expect(words("Smash burgers med kimchi-mayo")).toEqual(["smash", "burgers", "kimchi", "mayo"]);
  });
});
