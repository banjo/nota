import {
  type LinksUpdated,
  type NotesClient,
  NotaError,
  type PropertyPatch,
  type Tree,
} from "@nota/client";
import { toast } from "@nota/ui/sonner";
import type { QueryClient } from "@tanstack/react-query";
import { rememberFolder } from "./lib/folders.ts";
import {
  folderLabel,
  folderOf,
  joinTitle,
  noteLink,
  resolveLink,
  splitTitle,
} from "./lib/notes.ts";
import { isPinned, movePin, nextPin, PINNED_KEY, pinnedNotes } from "./lib/pinned.ts";
import { followedPath, remapFolder } from "./lib/rename.ts";
import { keys, noteBase, noteWritten, setNoteBase } from "./queries.ts";
import { type Route, useUI } from "./state.ts";

// What commands, menus and rows do, in one place.

const message = (e: unknown) =>
  e instanceof NotaError || e instanceof Error ? e.message : String(e);

export function fail(action: string, e: unknown) {
  toast.error(action, { description: message(e) });
}

const count = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** "Updated 3 links in 2 notes", or undefined when a move changed no links. */
export function linksMessage(links: LinksUpdated | undefined): string | undefined {
  if (!links?.count) return undefined;
  return `Updated ${count(links.count, "link")} in ${count(links.notes.length, "note")}`;
}

/** Tells the user which links a rename or move rewrote, if any. */
export function reportLinks(links: LinksUpdated | undefined) {
  const m = linksMessage(links);
  if (m) toast(m);
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
    if (!opts.quiet)
      toast.success(`Moved to ${folderLabel(folderOf(r.path))}`, {
        description: linksMessage(r.links),
      });
    else reportLinks(r.links);
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
    reportLinks(r.links);
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
      reportLinks(m.links);
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
    const id = await client.remove(path);
    const ui = useUI.getState();
    const open = ui.route.kind === "note" && ui.route.path === path;
    if (open) ui.navigate({ kind: "home" });
    qc.removeQueries({ queryKey: keys.note(path) });
    void qc.invalidateQueries();
    toast("Moved to the trash", {
      action: {
        label: "Undo",
        onClick: () => void restoreFromTrash(client, qc, id, { reopen: open, quiet: true }),
      },
    });
  } catch (e) {
    fail("Couldn't delete the note", e);
  }
}

const noteName = (path: string) => path.split("/").pop()!.replace(/\.md$/i, "");

/**
 * Puts a deleted note back where it was. `reopen` shows it again (the
 * undo of deleting the open note); `quiet` skips the "Restored" toast.
 */
export async function restoreFromTrash(
  client: NotesClient,
  qc: QueryClient,
  id: string,
  opts: { reopen?: boolean; quiet?: boolean } = {},
): Promise<string | undefined> {
  try {
    const path = await client.restoreTrash(id);
    void qc.invalidateQueries();
    const open = () => useUI.getState().navigate({ kind: "note", path });
    if (opts.reopen) open();
    if (!opts.quiet)
      toast(`Restored “${noteName(path)}”`, {
        description: path.includes("/") ? `In ${path.slice(0, path.lastIndexOf("/"))}` : undefined,
        action: { label: "Open", onClick: open },
      });
    return path;
  } catch (e) {
    const taken = e instanceof NotaError && (e.code === "exists" || e.code === "case_collision");
    fail(
      "Couldn't restore the note",
      taken ? "A note has taken its place since. Rename or move that note, then try again." : e,
    );
  }
}

/** Deletes a note in the trash for good. */
export async function purgeFromTrash(client: NotesClient, qc: QueryClient, id: string) {
  if (!client.purgeTrash) return;
  try {
    await client.purgeTrash(id);
    void qc.invalidateQueries({ queryKey: keys.trash });
  } catch (e) {
    fail("Couldn't delete the note", e);
  }
}

/** Deletes everything in the trash for good. */
export async function emptyTrash(client: NotesClient, qc: QueryClient) {
  if (!client.emptyTrash) return;
  try {
    await client.emptyTrash();
    void qc.invalidateQueries({ queryKey: keys.trash });
    toast("Emptied the trash");
  } catch (e) {
    fail("Couldn't empty the trash", e);
  }
}

/** Opens the note a [[wikilink]] names, creating it when there is none. */
export async function openWikilink(
  client: NotesClient,
  qc: QueryClient,
  target: string,
  from?: string,
  heading?: string,
) {
  const tree = await qc.ensureQueryData({ queryKey: keys.tree, queryFn: () => client.tree() });
  const hit = resolveLink(target, tree.notes, from);
  if (hit) {
    useUI
      .getState()
      .navigate({ kind: "note", path: hit.path, ...(heading ? { anchor: heading } : {}) });
    return;
  }
  const folder = from ? folderOf(from) || "/" : undefined;
  await createNote(client, qc, folder, target);
}

/**
 * A [text](dest) link in a note: another note ("Plan.md", "../a/b.md#Mål")
 * opens in Nota, a web or mail link in the browser. Other files can't be
 * opened yet.
 */
export async function openLink(client: NotesClient, qc: QueryClient, href: string, from: string) {
  const link = noteLink(href, from);
  if (!link) {
    if (/^[a-z][a-z0-9+.-]*:/i.test(href.trim()) || /^www\./i.test(href.trim()))
      return openExternal(href);
    toast("Only links to notes and web pages open from a note.");
    return;
  }
  const tree = await qc.ensureQueryData({ queryKey: keys.tree, queryFn: () => client.tree() });
  const hit = resolveLink(link.target, tree.notes, from);
  if (!hit) {
    toast(`No note at “${link.target}”`);
    return;
  }
  useUI
    .getState()
    .navigate({ kind: "note", path: hit.path, ...(link.heading ? { anchor: link.heading } : {}) });
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

/** Shows new `pinned` values in the cached tree before the writes land. */
function showPins(qc: QueryClient, pins: Map<string, number | null>) {
  qc.setQueryData<Tree>(
    keys.tree,
    (t) =>
      t && {
        ...t,
        notes: t.notes.map((n) => {
          if (!pins.has(n.path)) return n;
          const { [PINNED_KEY]: _, ...rest } = n.properties;
          const v = pins.get(n.path);
          return { ...n, properties: v === null ? rest : { ...rest, [PINNED_KEY]: v } };
        }),
      },
  );
}

/** Writes only `pinned` (null unpins), whatever else changed in the note meanwhile. */
async function writePins(client: NotesClient, qc: QueryClient, pins: Map<string, number | null>) {
  showPins(qc, pins);
  try {
    for (const [path, v] of pins) {
      const patch: PropertyPatch =
        v === null ? { unset: [PINNED_KEY] } : { set: { [PINNED_KEY]: v } };
      let r = await client.patch(path, patch, noteBase(path));
      if (r.conflict) r = await client.patch(path, patch);
      if (!r.conflict) noteWritten(qc, r.note);
    }
  } finally {
    void qc.invalidateQueries({ queryKey: keys.tree });
  }
}

const loadTree = (client: NotesClient, qc: QueryClient) =>
  qc.ensureQueryData({ queryKey: keys.tree, queryFn: () => client.tree() });

/** Pins a note last in the pinned list, or unpins it. */
export async function setPinned(client: NotesClient, qc: QueryClient, path: string, pin: boolean) {
  try {
    const tree = await loadTree(client, qc);
    const note = tree.notes.find((n) => n.path === path);
    if (note && isPinned(note) === pin) return;
    await writePins(client, qc, new Map([[path, pin ? nextPin(tree.notes) : null]]));
  } catch (e) {
    fail(pin ? "Couldn't pin the note" : "Couldn't unpin the note", e);
  }
}

export async function togglePinned(client: NotesClient, qc: QueryClient, path: string) {
  const tree = await loadTree(client, qc);
  const note = tree.notes.find((n) => n.path === path);
  await setPinned(client, qc, path, !(note && isPinned(note)));
}

/** Moves a pinned note to `index` in the pinned list. */
export async function movePinned(
  client: NotesClient,
  qc: QueryClient,
  path: string,
  index: number,
) {
  try {
    const tree = await loadTree(client, qc);
    const changes = movePin(pinnedNotes(tree.notes), path, index);
    if (changes.length)
      await writePins(client, qc, new Map(changes.map((c) => [c.path, c.pinned])));
  } catch (e) {
    fail("Couldn't move the pinned note", e);
  }
}
