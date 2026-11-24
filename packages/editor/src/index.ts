import { closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { markdown, markdownKeymap, markdownLanguage } from "@codemirror/lang-markdown";
import { Annotation, EditorState, type Extension, Transaction } from "@codemirror/state";
import {
  drawSelection,
  dropCursor,
  EditorView,
  keymap,
  placeholder as placeholderExt,
} from "@codemirror/view";
import { modeCompartment, modeExtensions, rememberHandlers, type EditorMode } from "./mode.ts";
import { type PreviewHandlers } from "./preview.ts";
import { notaSyntax } from "./syntax.ts";
import { notaTheme } from "./theme.ts";

export {
  activeLines,
  previewSpecs,
  tableSpecs,
  toggleTaskAt,
  type DecoSpec,
  type PreviewOptions,
  type TableSpec,
} from "./decorations.ts";
export {
  EDITOR_MODES,
  isEditorMode,
  modeExtensions,
  setEditorMode,
  sourceHighlight,
  type EditorMode,
} from "./mode.ts";
export { livePreview, toDecorations, type PreviewHandlers } from "./preview.ts";
export { findTags, notaSyntax, parseWikilink, tagLength, type Wikilink } from "./syntax.ts";
export { notaTheme } from "./theme.ts";
export type { EditorView } from "@codemirror/view";

/** Markdown with GFM and Nota's wikilinks and tags. */
export const notaMarkdown = () =>
  markdown({ base: markdownLanguage, extensions: [notaSyntax], addKeymap: false });

/** Extensions shared by every editor instance (no DOM handlers). */
export const extensions: Extension[] = [notaMarkdown(), EditorView.lineWrapping];

/**
 * Under a finger, scrolling to the caret leaves room for the page's
 * sticky top bar above and a little air above the keyboard below;
 * nothing changes with a mouse.
 */
export const touchScrollMargins = EditorView.scrollMargins.of(() =>
  typeof matchMedia !== "undefined" && matchMedia("(pointer: coarse)").matches
    ? { top: 64, bottom: 32 }
    : null,
);

/** Marks a transaction that came from outside (a reload), not typing. */
export const external = Annotation.define<boolean>();

export interface EditorOptions extends PreviewHandlers {
  doc: string;
  /** Called with the whole text after every edit made in the editor. */
  onChange?: (doc: string) => void;
  placeholder?: string;
  /** How the note is shown; "live" when absent. Change it with setEditorMode. */
  mode?: EditorMode;
  /** More extensions, e.g. from plugins. */
  extensions?: Extension[];
}

/** Mounts Nota's live-preview markdown editor in parent. */
export function createEditor(parent: HTMLElement, opts: EditorOptions): EditorView {
  const { doc, onChange, placeholder, mode = "live", extensions: extra = [], ...handlers } = opts;
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc,
      extensions: [
        history(),
        drawSelection(),
        dropCursor(),
        closeBrackets(),
        keymap.of([
          ...markdownKeymap,
          ...closeBracketsKeymap,
          ...defaultKeymap,
          ...historyKeymap,
          indentWithTab,
        ]),
        ...extensions,
        EditorView.contentAttributes.of({
          spellcheck: "true",
          autocorrect: "on",
          autocapitalize: "sentences",
        }),
        placeholder ? placeholderExt(placeholder) : [],
        modeCompartment.of(modeExtensions(mode, handlers)),
        notaTheme,
        touchScrollMargins,
        EditorView.updateListener.of((u) => {
          if (!u.docChanged || !onChange) return;
          if (u.transactions.every((tr) => tr.annotation(external))) return;
          onChange(u.state.doc.toString());
        }),
        ...extra,
      ],
    }),
  });
  rememberHandlers(view, handlers);
  return view;
}

/**
 * Replaces the editor's text with text from outside (the file changed
 * on disk), keeping the cursor where it can. It does not count as an
 * edit: no onChange, no undo step.
 */
export function replaceDoc(view: EditorView, text: string) {
  const cur = view.state.doc.toString();
  if (cur === text) return;
  // Only replace the part that differs, so the cursor and scroll stay.
  let start = 0;
  while (start < cur.length && start < text.length && cur[start] === text[start]) start++;
  let endCur = cur.length;
  let endNew = text.length;
  while (endCur > start && endNew > start && cur[endCur - 1] === text[endNew - 1]) {
    endCur--;
    endNew--;
  }
  view.dispatch({
    changes: { from: start, to: endCur, insert: text.slice(start, endNew) },
    annotations: [external.of(true), Transaction.addToHistory.of(false)],
  });
}
