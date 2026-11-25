// A show/hide choice for a section of a note (its properties, its
// backlinks): one preference per device in localStorage, not per note,
// hidden until asked for. Windows and tabs follow each other through the
// storage event, as the note mode and the theme do.

export type ShowPref = "show" | "hide";

export const DEFAULT_SHOW_PREF: ShowPref = "hide";

type Storage = Pick<globalThis.Storage, "getItem" | "setItem" | "removeItem">;

interface StorageEventLike {
  key: string | null;
}
interface WindowLike {
  addEventListener(type: "storage", fn: (e: StorageEventLike) => void): void;
  removeEventListener(type: "storage", fn: (e: StorageEventLike) => void): void;
}

/** The stored choice; anything unknown is the default (hidden). */
export const readShowPref = (v: string | null | undefined): ShowPref =>
  v === "show" || v === "hide" ? v : DEFAULT_SHOW_PREF;

export const toggledShowPref = (p: ShowPref): ShowPref => (p === "show" ? "hide" : "show");

/** Load, save and watch the choice stored under `key`. */
export function showPref(key: string) {
  function load(storage: Storage | null | undefined): ShowPref {
    try {
      return readShowPref(storage?.getItem(key));
    } catch {
      return DEFAULT_SHOW_PREF; // storage disabled
    }
  }

  function save(storage: Storage | null | undefined, pref: ShowPref) {
    try {
      if (pref === DEFAULT_SHOW_PREF) storage?.removeItem(key);
      else storage?.setItem(key, pref);
    } catch {
      // Private browsing: the choice lasts this page.
    }
  }

  /** Calls onChange with the new choice when another window changes it. */
  function watch(
    win: WindowLike | null | undefined,
    storage: Storage | null | undefined,
    onChange: (pref: ShowPref) => void,
  ): () => void {
    if (!win) return () => {};
    const fn = (e: StorageEventLike) => {
      // key is null when another window cleared the whole storage.
      if (e.key === null || e.key === key) onChange(load(storage));
    };
    win.addEventListener("storage", fn);
    return () => win.removeEventListener("storage", fn);
  }

  return { key, load, save, watch };
}
