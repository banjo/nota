import type { Tree } from "@nota/client";
import type { Suggestion } from "../registry.ts";
import { compareNames, folderLabel } from "./notes.ts";

// The folder picker of ⌘K ("Add to folder…", "New note in folder…") as pure
// functions, so the flows are testable without a DOM.

export const fold = (s: string) => s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();

/** "/a//b\" → "a/b": what a typed folder name means, before the vault checks it. */
export function cleanFolderInput(input: string): string {
  return input
    .normalize("NFC")
    .replace(/\\/g, "/")
    .split("/")
    .map((s) => s.trim())
    .filter(Boolean)
    .join("/");
}

/**
 * The path "Add folder…" creates. A name starting with "/" is from the
 * vault root; any other is relative to `base`, the selected folder ("" for
 * the root).
 */
export function resolveNewFolder(input: string, base: string): string {
  const name = cleanFolderInput(input);
  if (!name) return "";
  return input.trim().startsWith("/") || !base ? name : `${base}/${name}`;
}

/** The folder "Add folder…" starts from: where the note or list on screen is. */
export function baseFolder(
  route: { kind: string; path?: string; list?: { type: string; path?: string } },
  inbox: string,
): string {
  let f = "";
  if (route.kind === "note" && route.path?.includes("/"))
    f = route.path.slice(0, route.path.lastIndexOf("/"));
  else if (route.kind === "list" && route.list?.type === "folder") f = route.list.path ?? "";
  return f === inbox || f.startsWith(`${inbox}/`) ? "" : f;
}

export interface FolderChoices {
  /** The inbox suggestion, first. */
  suggested?: { folder: string; reason: string };
  /** Recently chosen folders, newest first. */
  recent: string[];
  /** The vault root ("/") and the rest, sorted; nothing from the groups above. */
  rest: string[];
  /** A folder to create and use, when the typed name is not one. */
  create?: string;
}

export interface ChoiceInput {
  tree: Pick<Tree, "inbox" | "folders">;
  /** What the user typed. */
  query: string;
  /** The folder the note is in already, "" for the root: not offered. */
  from?: string;
  recent?: string[];
  suggestion?: Pick<Suggestion, "folder" | "reason">;
  /** Whether the vault root is a choice (it is for notes, "" or "/" both mean it). */
  root?: boolean;
}

const rootLabel = "vault root";

/** What the folder picker lists for a query, in order. */
export function folderChoices(i: ChoiceInput): FolderChoices {
  const q = fold(i.query.trim());
  const from = i.from === undefined ? undefined : i.from || "/";
  const all = new Set(i.tree.folders.map((f) => f.path));
  all.add(i.tree.inbox);
  const folders = [...all].sort(compareNames);
  const matches = (f: string) => f !== from && (!q || fold(f === "/" ? rootLabel : f).includes(q));
  const taken = new Set<string>();
  const take = (f: string) => {
    taken.add(f);
    return f;
  };

  const out: FolderChoices = { recent: [], rest: [] };
  const sg = i.suggestion;
  if (sg && all.has(sg.folder) && matches(sg.folder)) {
    out.suggested = { folder: take(sg.folder), reason: sg.reason };
  }
  for (const f of i.recent ?? []) {
    if (all.has(f) && matches(f) && !taken.has(f)) out.recent.push(take(f));
  }
  const candidates = [...(i.root === false ? [] : ["/"]), ...folders];
  out.rest = candidates.filter((f) => matches(f) && !taken.has(f));

  const typed = cleanFolderInput(i.query);
  if (typed && !all.has(typed) && ![...all].some((f) => fold(f) === fold(typed)))
    out.create = typed;
  return out;
}

/** "Projects / Ideas", or "Vault root" for "/". */
export const choiceLabel = (f: string) => (f === "/" ? "Vault root" : folderLabel(f));

// Recent folders, newest first, remembered on this device.

export const RECENT_KEY = "nota:recent-folders";
const RECENT_MAX = 5;

type Store = Pick<Storage, "getItem" | "setItem">;

function store(): Store | undefined {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined; // storage blocked
  }
}

export function readRecentFolders(s: Store | undefined = store()): string[] {
  try {
    const v: unknown = JSON.parse(s?.getItem(RECENT_KEY) ?? "[]");
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}

/** Remembers a chosen folder (not the vault root) at the front. Returns the new list. */
export function rememberFolder(folder: string, s: Store | undefined = store()): string[] {
  const f = cleanFolderInput(folder);
  if (!f) return readRecentFolders(s);
  const next = [f, ...readRecentFolders(s).filter((x) => x !== f)].slice(0, RECENT_MAX);
  try {
    s?.setItem(RECENT_KEY, JSON.stringify(next));
  } catch {
    // a full or blocked store only loses the shortcut
  }
  return next;
}
