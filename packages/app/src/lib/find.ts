import { type EditorView, openFind } from "@nota/editor";

// ⌘F from outside the editor (the title, the page, ⌘K, the phone's
// sheet): the page on screen opens the find bar in its editor.

const FIND_EVENT = "nota:find";

/** Opens the find bar (or find and replace) in the editor on screen. */
export function requestFind(replace = false): void {
  document.dispatchEvent(new CustomEvent(FIND_EVENT, { detail: { replace } }));
}

/** Lets requestFind reach this editor; returns the disposer. */
export function listenFind(view: EditorView): () => void {
  const on = (e: Event) =>
    void openFind(view, !!(e as CustomEvent<{ replace?: boolean }>).detail?.replace);
  document.addEventListener(FIND_EVENT, on);
  return () => document.removeEventListener(FIND_EVENT, on);
}
