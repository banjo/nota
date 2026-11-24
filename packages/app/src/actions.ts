import { type NotesClient, NotaError } from "@nota/client";
import { toast } from "@nota/ui/sonner";
import type { QueryClient } from "@tanstack/react-query";
import { rememberFolder } from "./lib/folders.ts";
import { folderLabel, folderOf, joinTitle, resolveLink, splitTitle } from "./lib/notes.ts";
import { followedPath, remapFolder } from "./lib/rename.ts";
import { keys, noteWritten, setNoteBase } from "./queries.ts";
import { type Route, useUI } from "./state.ts";

// What commands, menus and rows do, in one place.

const message = (e: unknown) =>
  e instanceof NotaError || e instanceof Error ? e.message : String(e);

export function fail(action: string, e: unknown) {
  toast.error(action, { description: message(e) });
}

/** Where a new note goes: the folder on screen, else the inbox. */
export function newNoteFolder(route: Route, inbox: string): string {
  if (route.kind === "note") {
    const f = folderOf(route.path);
    return f === "" ? "/" : f;
  }
  if (route.kind === "list" && route.list.type === "folder") return route.list.path || "/";
  return inbox;
}

export async function createNote(
  client: NotesClient,
  qc: QueryClient,
  folder?: string,
  title = "Untitled",
) {
  try {
    const note = await client.create({ title, folder });
    // A new note is for writing: leave reading mode.
    if (useUI.getState().noteMode === "reading") useUI.getState().setNoteMode("live");
    noteWritten(qc, note);
    void qc.invalidateQueries({ queryKey: keys.tree });
    void qc.invalidateQueries({ queryKey: keys.inbox });
    useUI.getState().navigate({ kind: "note", path: note.path });
    // The title field takes the focus (NotePage), selected, ready to type over.
    requestAnimationFrame(() =>
      document.dispatchEvent(new CustomEvent("nota:focus-title", { detail: { select: true } })),
    );
    return note;
  } catch (e) {
    fail("Couldn't create the note", e);
    return undefined;
  }
}

export async function moveNote(
  client: NotesClient,
  qc: QueryClient,
  path: string,
  folder: string,
  opts: { quiet?: boolean } = {},
) {
  try {
    const r = await client.move(path, folder);
    useUI.getState().moved(r.from, r.path);
    qc.removeQueries({ queryKey: keys.note(r.from) });
    void qc.invalidateQueries();
    if (!opts.quiet) toast.success(`Moved to ${folderLabel(folderOf(r.path))}`);
    return r;
  } catch (e) {
    fail("Couldn't move the note", e);
    return undefined;
  }
}

/**
 * Makes an empty folder (nested paths make the parents) and shows it in the
 * tree. ⌘K "Add folder…" and the sidebar's new-folder row both end here;
 * the row passes `inline` to show the outcome in place instead of a toast.
 */
export async function addFolder(
  client: NotesClient,
  qc: QueryClient,
  path: string,
  inline?: { onError: (message: string) => void },
) {
  try {
    const p = await client.createFolder(path);
    await qc.invalidateQueries({ queryKey: keys.tree });
    useUI.getState().reveal(`${p}/_`);
    if (!inline) toast.success(`Created ${folderLabel(p)}`);
    return p;
  } catch (e) {
    if (inline) inline.onError(message(e));
    else fail("Couldn't create the folder", e);
    return undefined;
  }
}

/**
 * Renames a folder (its notes move with it) and keeps the screen on the
 * same things: unfolded folders stay unfolded, and the open note or list
 * follows. `inline` shows the outcome in the rename row instead of a toast.
 */
export async function renameFolder(
  client: NotesClient,
  qc: QueryClient,
  path: string,
  to: string,
  inline?: { onError: (message: string) => void },
) {
  try {
    const r = await client.renameFolder(path, to);
    const ui = useUI.getState();
    const expanded: Record<string, boolean> = {};
    for (const [k, v] of Object.entries(ui.expanded))
      expanded[k.startsWith("folder:") ? `folder:${remapFolder(k.slice(7), path, r.path)}` : k] = v;
    const route: Route =
      ui.route.kind === "note"
        ? { kind: "note", path: remapFolder(ui.route.path, path, r.path) }
        : ui.route.kind === "list" && ui.route.list.type === "folder"
          ? {
              kind: "list",
              list: { type: "folder", path: remapFolder(ui.route.list.path, path, r.path) },
            }
          : ui.route;
    useUI.setState({ expanded, route });
    qc.removeQueries({ queryKey: ["note"] });
    void qc.invalidateQueries();
    return r.path;
  } catch (e) {
    if (inline) inline.onError(message(e));
    else fail("Couldn't rename the folder", e);
    return undefined;
  }
}

/**
 * Renames a note: its title (the first line) becomes `title`, and its
 * file follows when it was named after the old title, as in the page's
 * own title field.
 */
export async function renameNote(
  client: NotesClient,
  qc: QueryClient,
  path: string,
  title: string,
  inline?: { onError: (message: string) => void },
) {
  try {
    const note = await client.get(path);
    const parts = splitTitle(note.body);
    let cur = note;
    if (parts.title !== title) {
      const r = await client.save(path, joinTitle({ ...parts, title }), {
        version: note.version,
        hash: note.hash,
      });
      if (r.conflict) throw new Error("The note changed somewhere else. Try again.");
      cur = r.note;
      noteWritten(qc, cur);
    }
    const to = followedPath(path, parts.title, title);
    let at = path;
    if (to) {
      setNoteBase(cur);
      const m = await client.rename(path, to);
      useUI.getState().moved(m.from, m.path);
      qc.removeQueries({ queryKey: keys.note(m.from) });
      at = m.path;
    }
    void qc.invalidateQueries();
    return at;
  } catch (e) {
    if (inline) inline.onError(message(e));
    else fail("Couldn't rename the note", e);
    return undefined;
  }
}

/** Creates a folder, then moves a note into it. Nothing moves when the folder is refused. */
export async function createFolderAndMove(
  client: NotesClient,
  qc: QueryClient,
  path: string,
  folder: string,
) {
  let made: string;
  try {
    made = await client.createFolder(folder);
  } catch (e) {
    fail("Couldn't create the folder", e);
    return undefined;
  }
  rememberFolder(made);
  void qc.invalidateQueries({ queryKey: keys.tree });
  return moveNote(client, qc, path, made);
}

/** Creates a folder, then a new note in it. */
export async function addFolderThenNote(client: NotesClient, qc: QueryClient, folder: string) {
  const made = await addFolder(client, qc, folder);
  return made === undefined ? undefined : createNoteIn(client, qc, made);
}

/** Moves a note into a folder chosen in the palette and remembers the choice. */
export async function fileNote(client: NotesClient, qc: QueryClient, path: string, folder: string) {
  const r = await moveNote(client, qc, path, folder);
  if (r) rememberFolder(folder);
  return r;
}

/** Makes a note in a folder chosen in the palette and remembers the choice. */
export async function createNoteIn(client: NotesClient, qc: QueryClient, folder: string) {
  const n = await createNote(client, qc, folder);
  if (n) rememberFolder(folder);
  return n;
}

export async function deleteNote(client: NotesClient, qc: QueryClient, path: string) {
  try {
    await client.remove(path);
    const ui = useUI.getState();
    if (ui.route.kind === "note" && ui.route.path === path) ui.navigate({ kind: "home" });
    qc.removeQueries({ queryKey: keys.note(path) });
    void qc.invalidateQueries();
    toast(
      client.kind === "desktop" ? "Moved to the trash (.trash in the vault)" : "Moved to the trash",
    );
  } catch (e) {
    fail("Couldn't delete the note", e);
  }
}

/** Opens the note a [[wikilink]] names, creating it when there is none. */
export async function openWikilink(
  client: NotesClient,
  qc: QueryClient,
  target: string,
  from?: string,
) {
  const tree = await qc.ensureQueryData({ queryKey: keys.tree, queryFn: () => client.tree() });
  const hit = resolveLink(target, tree.notes, from);
  if (hit) {
    useUI.getState().navigate({ kind: "note", path: hit.path });
    return;
  }
  const folder = from ? folderOf(from) || "/" : undefined;
  await createNote(client, qc, folder, target);
}

export function openExternal(href: string) {
  const url = /^[a-z][a-z0-9+.-]*:/i.test(href) ? href : `https://${href}`;
  window.open(url, "_blank", "noopener");
}

/**
 * Opens the vault wizard (Switch vault…, New vault…). With $NOTA_VAULT
 * set the variable decides which vault opens, so a switch would not
 * stick: say so instead.
 */
export async function openVaultFlow(client: NotesClient, mode: "create" | "open") {
  try {
    if ((await client.shell?.setup?.vaultSource()) === "env") {
      toast("NOTA_VAULT is set, so it decides which vault opens", {
        description: "Unset it to switch vaults from the app.",
      });
      return;
    }
  } catch {
    // Not knowing is not a reason to refuse.
  }
  useUI.getState().openVaultFlow(mode);
}
