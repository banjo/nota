import type { NoteInfo, Tree } from "@nota/client";
import type { Suggestion } from "../registry.ts";

// Where an inbox note probably belongs, from simple rules (no AI):
//   1. a tag that names a folder ("#fotboll" → Fotboll, "#nota" → Projekt/Nota)
//   2. the folder where most other notes with the same tag live
//   3. the folder of the most recent note with a similar title
// The highest score per folder wins; ties go to the deeper folder.

const STOP = new Set(
  "och att det som en ett med för på av till om är var har inte men den de så kan ska vid från eller när där här efter mot under the and for with from that this into about over".split(
    " ",
  ),
);

const norm = (s: string) =>
  s
    .normalize("NFC")
    .toLowerCase()
    .replace(/[\s_-]+/g, " ")
    .trim();

/** The meaningful words of a title. */
export function words(title: string): string[] {
  return title
    .normalize("NFC")
    .toLowerCase()
    .split(/[^\p{L}\p{N}]+/u)
    .filter((w) => w.length >= 3 && !STOP.has(w) && !/^\d+$/.test(w));
}

export function suggestFolders(note: NoteInfo, tree: Tree): Suggestion[] {
  const folders = tree.folders
    .map((f) => f.path)
    .filter((f) => f !== tree.inbox && !f.startsWith(`${tree.inbox}/`));
  const best = new Map<string, Suggestion>();
  const offer = (folder: string, score: number, reason: string) => {
    const cur = best.get(folder);
    if (!cur || score > cur.score) best.set(folder, { folder, score, reason });
  };

  // 1. Tags naming a folder: the whole path ("#projekt/nota") or its last part.
  for (const tag of note.tags) {
    const t = norm(tag);
    for (const f of folders) {
      if (norm(f) === t) offer(f, 3.2, `Tagged #${tag}`);
      else if (norm(f.split("/").pop()!) === t)
        offer(f, 3 + f.split("/").length * 0.01, `Tagged #${tag}`);
    }
  }

  const others = tree.notes.filter(
    (n) =>
      n.path !== note.path &&
      n.folder !== tree.inbox &&
      n.folder !== "" &&
      folders.includes(n.folder),
  );

  // 2. Where the other notes with this tag live.
  for (const tag of note.tags) {
    const counts = new Map<string, number>();
    for (const n of others)
      if (n.tags.includes(tag)) counts.set(n.folder, (counts.get(n.folder) ?? 0) + 1);
    for (const [f, c] of counts)
      offer(f, 1.5 + Math.min(c, 5) * 0.1, `Notes tagged #${tag} are here`);
  }

  // 3. Similar titles: shared words with notes elsewhere, newest wins,
  //    and words that name a folder.
  const mine = new Set(words(note.title));
  if (mine.size) {
    for (const f of folders) {
      const leaf = norm(f.split("/").pop()!);
      if (mine.has(leaf)) offer(f, 2.5, `“${f.split("/").pop()}” is in the title`);
    }
    let top: { n: NoteInfo; shared: number } | null = null;
    for (const n of others) {
      const shared = words(n.title).filter((w) => mine.has(w)).length;
      if (!shared) continue;
      if (!top || shared > top.shared || (shared === top.shared && n.modified > top.n.modified))
        top = { n, shared };
    }
    if (top) offer(top.n.folder, 1 + top.shared * 0.5, `Like “${top.n.title}”`);
  }

  return [...best.values()].sort(
    (a, b) =>
      b.score - a.score ||
      b.folder.split("/").length - a.folder.split("/").length ||
      a.folder.localeCompare(b.folder),
  );
}
