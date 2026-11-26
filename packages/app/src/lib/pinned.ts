import type { NoteInfo } from "@nota/client";

// Pinned notes are frontmatter: `pinned: <number>` is the note's place in
// the list (lower first), `pinned: true` puts it after the numbered ones.
// New pins go ten apart, so a move usually picks a number between the
// two neighbours and rewrites one file.

export const PINNED_KEY = "pinned";
const STEP = 10;

/** The note's place in the pinned list, or undefined when it is not pinned. */
export function pinOrder(note: Pick<NoteInfo, "properties">): number | undefined {
  const v = note.properties[PINNED_KEY];
  if (v === true) return Infinity;
  return typeof v === "number" && Number.isFinite(v) ? v : undefined;
}

export const isPinned = (note: Pick<NoteInfo, "properties">) => pinOrder(note) !== undefined;

/** The pinned notes in their order (ties by path). */
export function pinnedNotes(notes: readonly NoteInfo[]): NoteInfo[] {
  return notes
    .filter(isPinned)
    .sort((a, b) => pinOrder(a)! - pinOrder(b)! || (a.path < b.path ? -1 : 1));
}

/** The number that pins a note last, before the `pinned: true` ones. */
export function nextPin(notes: readonly NoteInfo[]): number {
  let hi = 0;
  for (const n of notes) {
    const o = pinOrder(n);
    if (o !== undefined && o !== Infinity && o > hi) hi = o;
  }
  return Math.floor(hi) + STEP;
}

export interface PinChange {
  path: string;
  pinned: number;
}

/**
 * The writes that move `path` to `index` in the pinned list `list`: one
 * note given a number between its new neighbours when there is room,
 * otherwise the whole list renumbered 10, 20, 30 (only changed notes).
 */
export function movePin(list: readonly NoteInfo[], path: string, index: number): PinChange[] {
  const moving = list.find((n) => n.path === path);
  if (!moving) return [];
  const rest = list.filter((n) => n.path !== path);
  const at = Math.max(0, Math.min(index, rest.length));
  const order = [...rest.slice(0, at), moving, ...rest.slice(at)];
  if (order.every((n, i) => n === list[i])) return [];
  const lo = at > 0 ? pinOrder(rest[at - 1]!)! : 0;
  const after = at < rest.length ? pinOrder(rest[at]!)! : Infinity;
  const hi = after === Infinity ? lo + 2 * STEP : after;
  const mid = Math.floor((lo + hi) / 2);
  if (Number.isFinite(lo) && mid > lo && mid < hi) return [{ path, pinned: mid }];
  return order.flatMap((n, i) =>
    pinOrder(n) === (i + 1) * STEP ? [] : [{ path: n.path, pinned: (i + 1) * STEP }],
  );
}
