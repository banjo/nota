import { type NotesClient, NotaError } from "@nota/client";
import { toast } from "@nota/ui/sonner";
import type { QueryClient } from "@tanstack/react-query";
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
