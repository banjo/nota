// Deterministic vaults for the harness: a small everyday vault, a 2000-note
// vault, and one ~5000-line note. Same seed, same notes, every run.

import type { ViewInfo } from "@nota/client";
import type { SeedNote } from "./memory-client.ts";

function rng(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (Math.imul(s, 1664525) + 1013904223) >>> 0;
    return s / 2 ** 32;
  };
}

const WORDS =
  "the plan note draft sync file folder index link tag status review idea meeting agenda list task week release patch editor tree inbox archive project area garden book read write ship test bench chart table code block quote callout mode theme".split(
    " ",
  );

function sentence(r: () => number, n = 10) {
  const w = Array.from({ length: n }, () => WORDS[Math.floor(r() * WORDS.length)]!);
  w[0] = w[0]![0]!.toUpperCase() + w[0]!.slice(1);
  return `${w.join(" ")}.`;
}

const FOLDERS = [
  "Inbox",
  "Projects",
  "Projects/Website",
  "Projects/Website/Launch",
  "Projects/App",
  "Projects/App/Sync",
  "Areas",
  "Areas/Health",
  "Areas/Home",
  "Reading",
  "Meetings",
  "Archive",
];

/** The body every seeded note shares a flavour of: links, tags, a callout, a table, code. */
function richBody(title: string, r: () => number, i: number, titles: string[]) {
  const other = titles[(i * 7 + 3) % titles.length]!;
  const lines = [
    `# ${title}`,
    "",
    `${sentence(r)} See [[${other}]] and #${WORDS[i % WORDS.length]}.`,
    "",
    "## Notes",
    "",
    `- ${sentence(r, 6)}`,
    `- [ ] ${sentence(r, 5)}`,
    `- [x] ${sentence(r, 5)}`,
    "",
    "> [!NOTE]",
    `> ${sentence(r, 8)}`,
    "",
    "| Key | Value |",
    "| --- | ---: |",
    `| size | ${i} |`,
    `| words | ${i * 3} |`,
    "",
    "```ts",
    `const n${i} = ${i};`,
    "```",
    "",
    sentence(r, 14),
  ];
  return lines.join("\n");
}

export function smallVault(): SeedNote[] {
  const r = rng(1);
  const titles = Array.from({ length: 40 }, (_, i) => `Note ${String(i + 1).padStart(2, "0")}`);
  const notes: SeedNote[] = titles.map((title, i) => {
    const folder = FOLDERS[i % FOLDERS.length]!;
    return {
      path: `${folder}/${title}.md`,
      body: richBody(title, r, i, titles),
      properties: {
        tags: [WORDS[(i * 3) % WORDS.length]!],
        ...(i % 3 === 0 ? { status: "draft" } : {}),
        created: `2026-10-${String((i % 28) + 1).padStart(2, "0")}T08:00:00Z`,
        updated: `2026-11-${String((i % 18) + 1).padStart(2, "0")}T08:00:00Z`,
      },
    };
  });
  // The note a cold start opens: the newest `updated`.
  notes.push({
    path: "Meetings/Standup.md",
    body: richBody("Standup", r, 99, titles),
    properties: {
      tags: ["meeting"],
      status: "in progress",
      created: "2026-11-01T08:00:00Z",
      updated: "2026-11-22T08:00:00Z",
    },
  });
  return notes;
}

/** ~2000 notes across 60 folders, three levels deep. */
export function largeVault(): SeedNote[] {
  const r = rng(2);
  const folders: string[] = ["Inbox"];
  for (let a = 0; a < 6; a++) {
    folders.push(`Area ${a}`);
    for (let b = 0; b < 3; b++) {
      folders.push(`Area ${a}/Topic ${b}`);
      for (let c = 0; c < 2; c++) folders.push(`Area ${a}/Topic ${b}/Part ${c}`);
    }
  }
  const titles = Array.from({ length: 2000 }, (_, i) => `Item ${String(i).padStart(4, "0")}`);
  const notes: SeedNote[] = titles.map((title, i) => ({
    path: `${folders[i % folders.length]}/${title}.md`,
    body: `# ${title}\n\n${sentence(r)} [[${titles[(i + 1) % titles.length]}]] #${WORDS[i % WORDS.length]}\n\n- ${sentence(r, 6)}\n`,
    properties: {
      tags: [WORDS[i % WORDS.length]!],
      updated: `2026-11-${String((i % 20) + 1).padStart(2, "0")}T08:00:00Z`,
    },
  }));
  notes.push({
    path: "Inbox/Standup.md",
    body: richBody("Standup", r, 1, titles),
    properties: { tags: ["meeting"], updated: "2026-11-22T08:00:00Z" },
  });
  return notes;
}

/** One ~5000-line note (prose, lists, headings, tables, code, callouts). */
export function bigNote(): SeedNote {
  const r = rng(3);
  const lines = ["# Big note", ""];
  let i = 0;
  while (lines.length < 5000) {
    i++;
    lines.push(`## Section ${i}`, "");
    for (let p = 0; p < 6; p++) lines.push(`${sentence(r, 16)} #${WORDS[p]} [[Note 0${p + 1}]]`);
    lines.push("");
    for (let p = 0; p < 8; p++) lines.push(`- ${p % 2 ? "[ ] " : ""}${sentence(r, 7)}`);
    lines.push("");
    if (i % 4 === 0)
      lines.push("| a | b | c |", "| --- | --- | --- |", "| 1 | 2 | 3 |", "| 4 | 5 | 6 |", "");
    if (i % 3 === 0) lines.push("```js", `function f${i}() {`, `  return ${i};`, "}", "```", "");
    if (i % 5 === 0) lines.push("> [!TIP] Callout", `> ${sentence(r, 9)}`, "");
  }
  return {
    path: "Projects/Big note.md",
    body: lines.join("\n"),
    properties: { tags: ["big"], updated: "2026-11-01T08:00:00Z" },
  };
}

export const VIEWS: ViewInfo[] = [
  {
    id: "meetings",
    name: "Meetings",
    icon: "clock",
    tag: "meeting",
    file: "meetings.yaml",
    count: 0,
  },
];
