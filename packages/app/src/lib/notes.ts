import type { NoteInfo, Tree } from "@nota/client";

// Pure helpers about notes, titles and the folder tree.

/** A note body split into its title line and the rest. */
export interface TitleParts {
  /** The heading marker of the first line ("# ") or "". */
  prefix: string;
  title: string;
  /** Blank lines between the title and the rest. */
  sep: string;
  rest: string;
  /** Whether the body had a newline after the title. */
  newline: boolean;
}

/** Splits a body into title (first line) and rest, losslessly. */
export function splitTitle(body: string): TitleParts {
  const nl = body.indexOf("\n");
  const first = nl < 0 ? body : body.slice(0, nl);
  const prefix = /^#{1,6}[ \t]+/.exec(first)?.[0] ?? "";
  const title = first.slice(prefix.length);
  if (nl < 0) return { prefix, title, sep: "", rest: "", newline: false };
  const after = body.slice(nl + 1);
  const sep = /^\n*/.exec(after)![0];
  return { prefix, title, sep, rest: after.slice(sep.length), newline: true };
}

/** The inverse of splitTitle; a first rest under a bare title gets a blank line. */
export function joinTitle(p: TitleParts): string {
  const head = p.prefix + p.title;
  if (!p.newline && p.rest === "") return head;
  const sep = !p.newline && p.sep === "" ? "\n" : p.sep;
  return `${head}\n${sep}${p.rest}`;
}

/** The file name Nota gives a title (as vault.Filename does). */
export function filename(title: string): string {
  let out = "";
  let space = false;
  for (const ch of title.normalize("NFC")) {
    if ('/\\:*?"<>|#^[]'.includes(ch) || /\s|\p{Cc}/u.test(ch)) {
      space = out.length > 0;
      continue;
    }
    if (space) {
      out += " ";
      space = false;
    }
    out += ch;
  }
  out = Array.from(out).slice(0, 120).join("");
  out = out.replace(/^[. ]+|[. ]+$/g, "");
  return out || "Untitled";
}

export const stem = (path: string) => (path.split("/").pop() ?? path).replace(/\.md$/i, "");
export const folderOf = (path: string) =>
  path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
export const baseName = (path: string) => path.split("/").pop() ?? path;

/** "Projekt / Nota" for display. */
export const folderLabel = (folder: string) => (folder ? folder.split("/").join(" / ") : "Vault");

export interface FolderNode {
  path: string;
  name: string;
  folders: FolderNode[];
  notes: NoteInfo[];
  /** Notes inside, subfolders included. */
  count: number;
}

const collator = new Intl.Collator("sv", { sensitivity: "base", numeric: true });
export const compareNames = (a: string, b: string) => collator.compare(a, b);

/** Nests the flat tree; the root has path "". */
export function buildTree(tree: Tree): FolderNode {
  const root: FolderNode = { path: "", name: "", folders: [], notes: [], count: tree.notes.length };
  const nodes = new Map<string, FolderNode>([["", root]]);
  const ensure = (path: string): FolderNode => {
    const have = nodes.get(path);
    if (have) return have;
    const parent = ensure(folderOf(path));
    const node: FolderNode = { path, name: baseName(path), folders: [], notes: [], count: 0 };
    parent.folders.push(node);
    nodes.set(path, node);
    return node;
  };
  for (const f of tree.folders) ensure(f.path).count = f.notes;
  for (const n of tree.notes) ensure(n.folder).notes.push(n);
  for (const node of nodes.values()) {
    node.folders.sort((a, b) => compareNames(a.name, b.name));
    node.notes.sort((a, b) => compareNames(a.title, b.title));
  }
  return root;
}

export type TreeRow =
  | { kind: "folder"; node: FolderNode; depth: number; open: boolean }
  | { kind: "note"; note: NoteInfo; depth: number };

/** The visible rows of the tree, depth first; `skip` leaves a top folder out (the inbox). */
export function flattenTree(
  root: FolderNode,
  isOpen: (path: string) => boolean,
  skip?: string,
): TreeRow[] {
  const rows: TreeRow[] = [];
  const walk = (node: FolderNode, depth: number) => {
    for (const f of node.folders) {
      if (depth === 0 && f.path === skip) continue;
      const open = isOpen(f.path);
      rows.push({ kind: "folder", node: f, depth, open });
      if (open) walk(f, depth + 1);
    }
    for (const n of node.notes) rows.push({ kind: "note", note: n, depth });
  };
  walk(root, 0);
  return rows;
}

/** Finds the note a [[wikilink]] target names: by path, file name or title. */
export function resolveLink(
  target: string,
  notes: NoteInfo[],
  from?: string,
): NoteInfo | undefined {
  const t = target.normalize("NFC").replace(/\.md$/i, "").toLowerCase();
  const byPath = notes.find((n) => n.path.replace(/\.md$/i, "").toLowerCase() === t);
  if (byPath) return byPath;
  const matches = notes.filter(
    (n) => stem(n.path).toLowerCase() === t || n.title.toLowerCase() === t,
  );
  if (matches.length <= 1 || !from) return matches[0];
  // Several: the one closest to the linking note, as Obsidian does.
  const dir = folderOf(from);
  return matches.find((n) => n.folder === dir) ?? matches[0];
}
