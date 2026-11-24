// Whether a note's properties (its frontmatter) show above the body: one
// preference per device in localStorage, not per note, hidden until asked
// for. Windows and tabs follow each other through the storage event, as
// the note mode and the theme do.

export type PropertiesPref = "show" | "hide";

export const PROPERTIES_KEY = "nota:properties";
export const DEFAULT_PROPERTIES: PropertiesPref = "hide";

type Storage = Pick<globalThis.Storage, "getItem" | "setItem" | "removeItem">;

/** The stored choice; anything unknown is the default (hidden). */
export const readPropertiesPref = (v: string | null | undefined): PropertiesPref =>
  v === "show" || v === "hide" ? v : DEFAULT_PROPERTIES;

export function loadPropertiesPref(storage: Storage | null | undefined): PropertiesPref {
  try {
    return readPropertiesPref(storage?.getItem(PROPERTIES_KEY));
  } catch {
    return DEFAULT_PROPERTIES; // storage disabled
  }
}

export function savePropertiesPref(storage: Storage | null | undefined, pref: PropertiesPref) {
  try {
    if (pref === DEFAULT_PROPERTIES) storage?.removeItem(PROPERTIES_KEY);
    else storage?.setItem(PROPERTIES_KEY, pref);
  } catch {
    // Private browsing: the choice lasts this page.
  }
}

export const toggledPropertiesPref = (p: PropertiesPref): PropertiesPref =>
  p === "show" ? "hide" : "show";

/** How many properties the chip counts: every key in the note's frontmatter. */
export const propertyCount = (props: Record<string, unknown> | undefined) =>
  Object.keys(props ?? {}).length;

interface StorageEventLike {
  key: string | null;
}
interface WindowLike {
  addEventListener(type: "storage", fn: (e: StorageEventLike) => void): void;
  removeEventListener(type: "storage", fn: (e: StorageEventLike) => void): void;
}

/** Calls onChange with the new choice when another window changes it. */
export function watchPropertiesPref(
  win: WindowLike | null | undefined,
  storage: Storage | null | undefined,
  onChange: (pref: PropertiesPref) => void,
): () => void {
  if (!win) return () => {};
  const fn = (e: StorageEventLike) => {
    // key is null when another window cleared the whole storage.
    if (e.key === null || e.key === PROPERTIES_KEY) onChange(loadPropertiesPref(storage));
  };
  win.addEventListener("storage", fn);
  return () => win.removeEventListener("storage", fn);
}
