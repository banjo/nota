import type { ExternalDoc, NotesClient, Opened } from "@nota/client";
import { toast } from "@nota/ui/sonner";
import type { QueryClient } from "@tanstack/react-query";
import { create } from "zustand";
import { fail } from "./actions.ts";
import { rememberFolder } from "./lib/folders.ts";
import { addPreview, displayPath, nextPreview } from "./lib/opened.ts";
import { folderLabel, folderOf } from "./lib/notes.ts";
import { keys } from "./queries.ts";
import { useUI } from "./state.ts";

// Markdown files from outside the vault (desktop): a read-only preview
// first; Edit makes an in-memory draft. Nothing is written to the
// original or the vault until Save to vault, Move into vault, or the
// explicit Overwrite original. Previews and drafts live as long as the
// window; they are never stored.

export interface Confirm {
  title: string;
  body: string;
  action: string;
  destructive?: boolean;
  run: () => void | Promise<void>;
}

interface OpenedState {
  /** The previews in the sidebar, oldest first. */
  previews: Opened[];
  /** Drafts by preview id: present while editing. */
  drafts: Record<string, string>;
  /** The question a preview action asks first, or null. */
  confirm: Confirm | null;
}

export const useOpened = create<OpenedState>()(() => ({ previews: [], drafts: {}, confirm: null }));

export const externalKey = (id: string) => ["external", id] as const;

/** The draft of a preview, if it is being edited. */
export const draftOf = (id: string) => useOpened.getState().drafts[id];

export function setDraft(id: string, body: string | undefined) {
  useOpened.setState((s) => {
    const drafts = { ...s.drafts };
    if (body === undefined) delete drafts[id];
    else drafts[id] = body;
    return { drafts };
  });
}

/** A draft differs from the file as last read. */
export function isDirty(qc: QueryClient, id: string): boolean {
  const d = draftOf(id);
  const doc = qc.getQueryData<ExternalDoc>(externalKey(id));
  return d !== undefined && d !== doc?.body;
}

/** Adds previews the page did not know (after a reload, they come from Go). */
export function knowPreviews(client: NotesClient, list: Opened[]) {
  for (const o of list) addKnown(client, o);
}

function addKnown(client: NotesClient, o: Opened) {
  const { list, dropped } = addPreview(
    useOpened.getState().previews,
    o,
    (id) => draftOf(id) !== undefined,
  );
  useOpened.setState({ previews: list });
  for (const id of dropped) void client.opener?.dismiss(id);
}

/** Opens a preview: in the sidebar, and on the page. */
export function showPreview(client: NotesClient, o: Opened) {
  addKnown(client, o);
  useUI.getState().navigate({ kind: "external", id: o.id });
}

function forget(id: string) {
  const s = useOpened.getState();
  const after = nextPreview(s.previews, id);
  setDraft(id, undefined);
  useOpened.setState({ previews: s.previews.filter((x) => x.id !== id) });
  const ui = useUI.getState();
  if (ui.route.kind === "external" && ui.route.id === id)
    ui.navigate(after ? { kind: "external", id: after } : { kind: "home" });
}

/** Closes a preview; a draft with changes asks first. */
export function closePreview(client: NotesClient, qc: QueryClient, id: string) {
  const close = () => {
    forget(id);
    qc.removeQueries({ queryKey: externalKey(id) });
    void client.opener?.dismiss(id);
  };
  if (!isDirty(qc, id)) return close();
  useOpened.setState({
    confirm: {
      title: "Discard your changes?",
      body: "The draft was never saved: closing it loses your edits. The original file stays as it is.",
      action: "Discard and close",
      destructive: true,
      run: close,
    },
  });
}

/** Turns the preview into a draft in the editor. Nothing is written. */
export function editPreview(qc: QueryClient, id: string) {
  const doc = qc.getQueryData<ExternalDoc>(externalKey(id));
  if (!doc || draftOf(id) !== undefined) return;
  if (!doc.utf8) {
    toast("This file isn't UTF-8 text, so it can't be edited here.");
    return;
  }
  setDraft(id, doc.body);
}

/** Back to the file's text, still editing. */
export function revertDraft(qc: QueryClient, id: string) {
  const doc = qc.getQueryData<ExternalDoc>(externalKey(id));
  if (doc && draftOf(id) !== undefined) setDraft(id, doc.body);
}

/** Drops the draft: back to the read-only preview. */
export function discardDraft(qc: QueryClient, id: string) {
  if (!isDirty(qc, id)) return setDraft(id, undefined);
  useOpened.setState({
    confirm: {
      title: "Discard the draft?",
      body: "Your edits go away and the preview shows the file again. Nothing was written anywhere.",
      action: "Discard",
      destructive: true,
      run: () => setDraft(id, undefined),
    },
  });
}

async function currentDoc(client: NotesClient, qc: QueryClient, id: string): Promise<ExternalDoc> {
  return (
    qc.getQueryData<ExternalDoc>(externalKey(id)) ??
    (await qc.fetchQuery<ExternalDoc>({
      queryKey: externalKey(id),
      queryFn: () => client.opener!.read(id),
    }))
  );
}

/**
 * Saves the preview (or its draft) into a vault folder as a new note;
 * with move, the original then goes to the Trash. Opens the new note.
 */
export async function saveToVault(
  client: NotesClient,
  qc: QueryClient,
  id: string,
  folder: string,
  move = false,
) {
  const o = client.opener;
  if (!o) return;
  try {
    const doc = await currentDoc(client, qc, id);
    const draft = draftOf(id);
    const r = await o.save(id, { folder, hash: doc.hash, body: draft, move });
    if (r.conflict) {
      void qc.invalidateQueries({ queryKey: externalKey(id) });
      toast.warning("The file changed on disk", {
        description:
          draft === undefined
            ? "The preview now shows the new version. Save again to keep it."
            : "Moving it now would lose those changes. Save to vault keeps your draft and leaves the file alone.",
      });
      return;
    }
    forget(id);
    qc.removeQueries({ queryKey: externalKey(id) });
    if (folder) rememberFolder(folder);
    void qc.invalidateQueries({ queryKey: keys.tree });
    void qc.invalidateQueries({ queryKey: keys.inbox });
    if (r.path) useUI.getState().navigate({ kind: "note", path: r.path });
    const where = r.path ? folderLabel(folderOf(r.path)) : "the vault";
    toast.success(`Saved to ${where}`, {
      description: move
        ? r.trashError
          ? `Couldn't move the original to the Trash: ${r.trashError}`
          : `The original (${displayPath(doc)}) is in the Trash.`
        : `The original file is unchanged.`,
    });
  } catch (e) {
    fail("Couldn't save the file to the vault", e);
  }
}

/** Asks, then saves and moves the original to the Trash. */
export function confirmMove(client: NotesClient, qc: QueryClient, id: string, folder: string) {
  const p = useOpened.getState().previews.find((x) => x.id === id);
  useOpened.setState({
    confirm: {
      title: `Move “${p?.name ?? "the file"}” into ${folder === "/" ? "the vault root" : folder ? folderLabel(folder) : "the inbox"}?`,
      body: `A note is made in the vault${draftOf(id) !== undefined ? " from your draft" : ""}, then the original${p ? ` at ${displayPath(p)}` : ""} goes to the Trash.`,
      action: "Move into vault",
      run: () => saveToVault(client, qc, id, folder, true),
    },
  });
}

/** Asks, then writes the draft over the original file. */
export function confirmOverwrite(client: NotesClient, qc: QueryClient, id: string) {
  const o = client.opener;
  const draft = draftOf(id);
  const p = useOpened.getState().previews.find((x) => x.id === id);
  if (!o || draft === undefined) return;
  useOpened.setState({
    confirm: {
      title: "Overwrite the original file?",
      body: `Your draft replaces the text of ${p ? displayPath(p) : "the file"}. Nota can't undo this; nothing goes into the vault.`,
      action: "Overwrite original",
      destructive: true,
      run: async () => {
        try {
          const doc = await currentDoc(client, qc, id);
          const r = await o.overwriteOriginal(id, draftOf(id) ?? draft, doc.hash);
          if (r.conflict) {
            toast.warning("The file changed on disk", {
              description: "Nothing was written. Your draft is still here.",
            });
            return;
          }
          setDraft(id, undefined);
          await qc.invalidateQueries({ queryKey: externalKey(id) });
          toast.success("The original file was overwritten");
        } catch (e) {
          fail("Couldn't write the original file", e);
        }
      },
    },
  });
}

/** The preview on screen, for commands. */
export const currentPreview = () => {
  const r = useUI.getState().route;
  return r.kind === "external" ? r.id : undefined;
};
