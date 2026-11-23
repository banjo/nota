import { type NotesClient, NotaError } from "@nota/client";
import { toast } from "@nota/ui/sonner";
import type { QueryClient } from "@tanstack/react-query";
import { rememberFolder } from "./lib/folders.ts";
import { folderLabel, folderOf, resolveLink } from "./lib/notes.ts";
import { keys, noteWritten } from "./queries.ts";
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

/** Makes an empty folder (nested paths make the parents) and shows it in the tree. */
export async function addFolder(client: NotesClient, qc: QueryClient, path: string) {
  try {
    const p = await client.createFolder(path);
    void qc.invalidateQueries({ queryKey: keys.tree });
    useUI.getState().reveal(`${p}/_`);
    toast.success(`Created ${folderLabel(p)}`);
    return p;
  } catch (e) {
    fail("Couldn't create the folder", e);
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
