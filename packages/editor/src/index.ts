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
import { livePreview, type PreviewHandlers } from "./preview.ts";
import { notaSyntax } from "./syntax.ts";
import { notaTheme } from "./theme.ts";

export {
  activeLines,
  previewSpecs,
  toggleTaskAt,
  type DecoSpec,
  type PreviewOptions,
} from "./decorations.ts";
export { livePreview, toDecorations, type PreviewHandlers } from "./preview.ts";
export { findTags, notaSyntax, parseWikilink, tagLength, type Wikilink } from "./syntax.ts";
export { notaTheme } from "./theme.ts";
export type { EditorView } from "@codemirror/view";

/** Markdown with GFM and Nota's wikilinks and tags. */
export const notaMarkdown = () =>
  markdown({ base: markdownLanguage, extensions: [notaSyntax], addKeymap: false });

/** Extensions shared by every editor instance (no DOM handlers). */
export const extensions: Extension[] = [notaMarkdown(), EditorView.lineWrapping];

/** Marks a transaction that came from outside (a reload), not typing. */
export const external = Annotation.define<boolean>();

export interface EditorOptions extends PreviewHandlers {
  doc: string;
  /** Called with the whole text after every edit made in the editor. */
  onChange?: (doc: string) => void;
  placeholder?: string;
  /** More extensions, e.g. from plugins. */
  extensions?: Extension[];
}

/** Mounts Nota's live-preview markdown editor in parent. */
export function createEditor(parent: HTMLElement, opts: EditorOptions): EditorView {
  const { doc, onChange, placeholder, extensions: extra = [], ...handlers } = opts;
  return new EditorView({
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
        livePreview(handlers),
        notaTheme,
        EditorView.updateListener.of((u) => {
          if (!u.docChanged || !onChange) return;
          if (u.transactions.every((tr) => tr.annotation(external))) return;
          onChange(u.state.doc.toString());
        }),
        ...extra,
      ],
    }),
  });
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
