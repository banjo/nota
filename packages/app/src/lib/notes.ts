import type { NoteInfo, Tree } from "@nota/client";

// Pure helpers about notes, titles and the folder tree.

/** A note body split into its title line and the rest. */
export interface TitleParts {
  /** Blank lines before the title, which the title skips as vault.Title does. */
  lead: string;
  /** The heading marker of the first line ("# ") or "". */
  prefix: string;
  title: string;
  /** Blank lines between the title and the rest. */
  sep: string;
  rest: string;
  /** Whether the body had a newline after the title. */
  newline: boolean;
}

/** Splits a body into title (first non-blank line) and rest, losslessly. */
export function splitTitle(body: string): TitleParts {
  const lead = /^(?:[ \t]*\n)*/.exec(body)![0];
  const text = body.slice(lead.length);
  const nl = text.indexOf("\n");
  const first = nl < 0 ? text : text.slice(0, nl);
  const prefix = /^#{1,6}[ \t]+/.exec(first)?.[0] ?? "";
  const title = first.slice(prefix.length);
  if (nl < 0) return { lead, prefix, title, sep: "", rest: "", newline: false };
  const after = text.slice(nl + 1);
  const sep = /^\n*/.exec(after)![0];
  return { lead, prefix, title, sep, rest: after.slice(sep.length), newline: true };
}

/** The inverse of splitTitle; a first rest under a bare title gets a blank line. */
export function joinTitle(p: TitleParts): string {
  const head = p.lead + p.prefix + p.title;
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

/**
 * The attachments folder when it has a row of its own in the sidebar
 * instead of in the tree: when it holds no notes (a note there keeps it
 * in the tree, or the note would have nowhere to show).
 */
export function attachmentsRow(tree: Tree | undefined): string | undefined {
  const dir = tree?.attachments;
  if (!dir) return undefined;
  return tree.folders.some((f) => f.path === dir && f.notes > 0) ? undefined : dir;
}

export type TreeRow =
  | { kind: "folder"; node: FolderNode; depth: number; open: boolean }
  | { kind: "note"; note: NoteInfo; depth: number };

/**
 * The visible rows of the tree, depth first; `skip` leaves top folders out
 * (the inbox and the attachments, which have rows of their own).
 */
export function flattenTree(
  root: FolderNode,
  isOpen: (path: string) => boolean,
  ...skip: (string | undefined)[]
): TreeRow[] {
  const rows: TreeRow[] = [];
  const walk = (node: FolderNode, depth: number) => {
    for (const f of node.folders) {
      if (depth === 0 && skip.includes(f.path)) continue;
      const open = isOpen(f.path);
      rows.push({ kind: "folder", node: f, depth, open });
      if (open) walk(f, depth + 1);
    }
    for (const n of node.notes) rows.push({ kind: "note", note: n, depth });
  };
  walk(root, 0);
  return rows;
}

/**
 * A link's destination as a vault path, resolved against the linking
 * note's folder as the index does (internal/markdown.parseMarkdownLink):
 * URL-decoded, "/" from the vault root. Null for web links, anchors and
 * paths outside the vault.
 */
export function vaultPath(href: string, from: string): { path: string; hash?: string } | null {
  let dest = href.trim().replace(/^<(.*)>$/, "$1");
  if (/^[a-z][a-z0-9+.-]*:/i.test(dest) || dest.startsWith("#") || dest.startsWith("//"))
    return null;
  let hash: string | undefined;
  const at = dest.indexOf("#");
  if (at >= 0) {
    hash = dest.slice(at + 1) || undefined;
    dest = dest.slice(0, at);
  }
  try {
    dest = decodeURIComponent(dest);
  } catch {}
  const parts = dest.startsWith("/") ? [] : folderOf(from).split("/").filter(Boolean);
  for (const p of dest.split("/")) {
    if (p === "" || p === ".") continue;
    if (p === "..") {
      if (!parts.length) return null;
      parts.pop();
    } else parts.push(p);
  }
  if (!parts.length || dest.endsWith("/")) return null;
  return { path: parts.join("/").normalize("NFC"), ...(hash ? { hash } : {}) };
}

/**
 * A [text](dest) link to a note: its vault path and heading (see
 * vaultPath). Null for anything but a .md file in the vault.
 */
export function noteLink(href: string, from: string): { target: string; heading?: string } | null {
  const p = vaultPath(href, from);
  if (!p || !/\.md$/i.test(p.path)) return null;
  return { target: p.path, ...(p.hash ? { heading: p.hash } : {}) };
}

/**
 * The relative href a note at `from` links the file at `to` with, each
 * segment URL-encoded ("../Attachments/Pasted%20image.png"), so it works
 * on GitHub and in any Markdown reader too.
 */
export function relativeHref(from: string, to: string): string {
  const a = folderOf(from).split("/").filter(Boolean);
  const b = to.split("/");
  let i = 0;
  while (i < a.length && i < b.length - 1 && a[i] === b[i]) i++;
  return [...a.slice(i).map(() => ".."), ...b.slice(i)]
    .map((s) => (s === ".." ? s : encodeURIComponent(s)))
    .join("/");
}

/**
 * The name a pasted image is saved under: its own, unless the browser
 * made one up ("image.png"), then "Pasted image 20261125143012.png" as in
 * Obsidian.
 */
export function attachmentName(file: { name: string; type: string }, now = new Date()): string {
  const ext =
    file.name.match(/\.[a-z0-9]+$/i)?.[0].toLowerCase() ??
    `.${file.type.split("/")[1]?.replace("jpeg", "jpg").replace("svg+xml", "svg") ?? "png"}`;
  if (file.name && !/^image\.[a-z0-9]+$/i.test(file.name)) return file.name;
  const p = (n: number) => String(n).padStart(2, "0");
  const stamp = `${now.getFullYear()}${p(now.getMonth() + 1)}${p(now.getDate())}${p(now.getHours())}${p(now.getMinutes())}${p(now.getSeconds())}`;
  return `Pasted image ${stamp}${ext}`;
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
