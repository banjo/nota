import { type EditorMode, isEditorMode } from "@nota/editor";

// How notes are shown (Live, Reading or Source): one preference per
// device in localStorage, not per note. Windows and tabs follow each
// other through the storage event, as the theme does.

export type NoteMode = EditorMode;

export const NOTE_MODE_KEY = "nota:note-mode";
export const DEFAULT_NOTE_MODE: NoteMode = "live";

type Storage = Pick<globalThis.Storage, "getItem" | "setItem" | "removeItem">;

/** The stored mode; anything unknown is Live. */
export function readNoteMode(v: string | null | undefined): NoteMode {
  return isEditorMode(v) ? v : DEFAULT_NOTE_MODE;
}

export function loadNoteMode(storage: Storage | null | undefined): NoteMode {
  try {
    return readNoteMode(storage?.getItem(NOTE_MODE_KEY));
  } catch {
    return DEFAULT_NOTE_MODE; // storage disabled
  }
}

export function saveNoteMode(storage: Storage | null | undefined, mode: NoteMode) {
  try {
    if (mode === DEFAULT_NOTE_MODE) storage?.removeItem(NOTE_MODE_KEY);
    else storage?.setItem(NOTE_MODE_KEY, mode);
  } catch {
    // Private browsing: the choice lasts this page.
  }
}

/** The mode ⌘E switches to: Live and Reading swap, Source goes back to Live. */
export const toggledNoteMode = (mode: NoteMode): NoteMode => (mode === "live" ? "reading" : "live");

interface StorageEventLike {
  key: string | null;
}
interface WindowLike {
  addEventListener(type: "storage", fn: (e: StorageEventLike) => void): void;
  removeEventListener(type: "storage", fn: (e: StorageEventLike) => void): void;
}

/** Calls onChange with the new mode when another window changes it. */
export function watchNoteMode(
  win: WindowLike | null | undefined,
  storage: Storage | null | undefined,
  onChange: (mode: NoteMode) => void,
): () => void {
  if (!win) return () => {};
  const fn = (e: StorageEventLike) => {
    // key is null when another window cleared the whole storage.
    if (e.key === null || e.key === NOTE_MODE_KEY) onChange(loadNoteMode(storage));
  };
  win.addEventListener("storage", fn);
  return () => win.removeEventListener("storage", fn);
}
