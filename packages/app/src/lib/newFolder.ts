import type { Tree } from "@nota/client";
import { checkName } from "../onboarding/setup.ts";
import type { TreeRow } from "./notes.ts";

// "New folder" straight in the sidebar tree, Finder style: a row with a
// name field appears where the folder will go; Enter creates it, Esc
// cancels. Pure functions here, so the flow is testable without a DOM.

/** Where an inline new-folder row sits: "" is the vault root. */
export interface FolderDraft {
  parent: string;
}

export type DraftRow = { kind: "draft"; parent: string; depth: number };

/**
 * The tree's rows with the draft row in place: first inside its parent
 * (right under the parent's row, one level deeper), or at the top for the
 * vault root. A parent that is not on screen leaves the rows as they are.
 */
export function withDraft(rows: TreeRow[], draft: FolderDraft | null): (TreeRow | DraftRow)[] {
  if (!draft) return rows;
  if (draft.parent === "") return [{ kind: "draft", parent: "", depth: 0 }, ...rows];
  const i = rows.findIndex((r) => r.kind === "folder" && r.node.path === draft.parent);
  if (i < 0) return rows;
  const out: (TreeRow | DraftRow)[] = [...rows];
  out.splice(i + 1, 0, { kind: "draft", parent: draft.parent, depth: rows[i]!.depth + 1 });
  return out;
}

const caseless = (p: string) => p.normalize("NFC").toLowerCase();

/**
 * Checks a typed name for a new folder in `parent` with the same rules as
 * the vault and the setup wizard (internal/vault/testdata/names.json),
 * plus "is there one already". An empty name is no error: Enter on an
 * empty row just cancels, as in Finder. `path` is "" unless the name is fine.
 */
export function checkNewFolder(
  raw: string,
  parent: string,
  tree: Pick<Tree, "inbox" | "folders">,
): { path: string; error: string } {
  if (raw.trim() === "") return { path: "", error: "" };
  const { name, error } = checkName(raw, "inbox");
  if (error) return { path: "", error };
  const path = parent ? `${parent}/${name}` : name;
  const taken = new Set([tree.inbox, ...tree.folders.map((f) => f.path)].map(caseless));
  if (taken.has(caseless(path)))
    return { path: "", error: `There is already a folder called “${name}” here.` };
  return { path, error: "" };
}
