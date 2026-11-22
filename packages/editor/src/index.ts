import { markdown } from "@codemirror/lang-markdown";
import { EditorView, basicSetup } from "codemirror";

/** Extensions shared by every editor instance. */
export const extensions = [basicSetup, markdown(), EditorView.lineWrapping];

/** Mounts a markdown editor in parent and calls onChange with the new text. */
export function createEditor(parent: HTMLElement, doc: string, onChange?: (doc: string) => void) {
  return new EditorView({
    parent,
    doc,
    extensions: [
      ...extensions,
      EditorView.updateListener.of((u) => {
        if (u.docChanged) onChange?.(u.state.doc.toString());
      }),
    ],
  });
}
