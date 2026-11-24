import type { Tree } from "@nota/client";
import { checkName } from "../onboarding/setup.ts";
import { filename, folderOf, stem } from "./notes.ts";

// Renaming in place, in the sidebar tree: a folder or a note gets a new
// name, checked with the same rules as the vault and the setup wizard
// (internal/vault/testdata/names.json). Pure functions, so the flow is
// testable without a DOM.

export type RenameTarget = { kind: "folder" | "note"; path: string };

const caseless = (p: string) => p.normalize("NFC").toLowerCase();

/** The text a rename field starts with: the folder's name, the note's title. */
export const nameOf = (t: RenameTarget, title?: string) =>
  t.kind === "note" ? (title ?? stem(t.path)) : (t.path.split("/").pop() ?? t.path);

/**
 * Checks a typed name for the folder at `path` (its new path is in the
 * same parent) with the shared rules, plus "is there one already".
 * An empty or unchanged name is no error: Enter just cancels, as in
 * Finder. `to` is "" unless the name is fine and different.
 */
export function checkFolderRename(
  raw: string,
  path: string,
  tree: Pick<Tree, "inbox" | "folders">,
): { to: string; error: string } {
  if (raw.trim() === "") return { to: "", error: "" };
  const { name, error } = checkName(raw, "inbox");
  if (error)
    return {
      to: "",
      error: error.includes("slashes") ? "Use a single folder name, without slashes." : error,
    };
  const parent = folderOf(path);
  const to = parent ? `${parent}/${name}` : name;
  if (to === path) return { to: "", error: "" };
  if (caseless(to) === caseless(path))
    return { to: "", error: "A name can’t differ from the current one only in letter case." };
  const taken = new Set([tree.inbox, ...tree.folders.map((f) => f.path)].map(caseless));
  if (taken.has(caseless(to)))
    return { to: "", error: `There is already a folder called “${name}” here.` };
  return { to, error: "" };
}

/**
 * Checks a typed title for the note at `path`: it becomes the note's
 * first line, and its file name when that followed the old title. The
 * rules are the folder ones, so a name is a name anywhere in the vault.
 */
export function checkNoteRename(
  raw: string,
  path: string,
  siblings: string[],
): { title: string; error: string } {
  if (raw.trim() === "") return { title: "", error: "" };
  const { name, error } = checkName(raw, "inbox");
  if (error)
    return {
      title: "",
      error: error.includes("slashes") ? "A name can’t contain slashes." : error,
    };
  const taken = new Set(siblings.filter((p) => p !== path).map((p) => caseless(stem(p))));
  if (taken.has(caseless(filename(name))))
    return { title: "", error: `There is already a note called “${name}” here.` };
  return { title: name, error: "" };
}

/**
 * The path a note's file moves to when its title changes, or null when
 * the file keeps its name: only a file named after the old title (or a
 * fresh "Untitled") follows, as in the page's own title field.
 */
export function followedPath(path: string, oldTitle: string, newTitle: string): string | null {
  const want = filename(newTitle);
  const cur = stem(path);
  if (want === cur) return null;
  if (cur !== filename(oldTitle) && !/^Untitled( \d+)?$/.test(cur)) return null;
  const f = folderOf(path);
  return `${f ? `${f}/` : ""}${want}.md`;
}

/** `p` with the folder `from` replaced by `to` at its start, or `p` itself. */
export const remapFolder = (p: string, from: string, to: string) =>
  p === from ? to : p.startsWith(`${from}/`) ? to + p.slice(from.length) : p;
