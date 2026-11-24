import { Compartment, EditorState, type Extension } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { tags as t } from "@lezer/highlight";
import { livePreview, type PreviewHandlers } from "./preview.ts";

// How a note is shown. Live hides markers away from the cursor (the
// default); Reading is the finished page, read-only with every marker
// hidden; Source is the raw markdown with colour only. The three share
// one document and one view: the mode lives in a Compartment, so
// switching it keeps the text, the selection, the undo history and the
// scroll position.

export type EditorMode = "live" | "reading" | "source";

export const EDITOR_MODES: readonly EditorMode[] = ["live", "reading", "source"];

export const isEditorMode = (v: unknown): v is EditorMode =>
  v === "live" || v === "reading" || v === "source";

const v = (name: string, fallback: string) => `var(--${name}, ${fallback})`;
const md = (name: string, token: string, fallback: string) =>
  `var(--md-${name}, ${v(token, fallback)})`;

/** Syntax colours for Source mode, from the app's CSS variables. */
export const sourceHighlight = HighlightStyle.define([
  { tag: t.heading, fontWeight: "650", color: md("heading", "foreground", "#1d1f24") },
  { tag: t.strong, fontWeight: "650", color: "var(--md-bold, inherit)" },
  { tag: t.emphasis, fontStyle: "italic", color: "var(--md-italic, inherit)" },
  { tag: t.strikethrough, textDecoration: "line-through" },
  { tag: [t.link, t.url], color: md("link", "ink", "#1e7363") },
  { tag: t.monospace, color: md("code", "ink-strong", "#2f40b3") },
  { tag: t.labelName, color: md("tag", "ink-strong", "#2f40b3") },
  { tag: t.quote, color: md("quote", "muted-foreground", "#6b6f78") },
  { tag: [t.processingInstruction, t.meta, t.contentSeparator], color: v("faint", "#9ca0a8") },
]);

/** The extensions that make up one mode. */
export function modeExtensions(mode: EditorMode, handlers: PreviewHandlers = {}): Extension {
  switch (mode) {
    case "live":
      return livePreview(handlers);
    case "reading":
      return [
        livePreview(handlers, { reading: true }),
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        EditorView.editorAttributes.of({ class: "cm-reading" }),
      ];
    case "source":
      return [
        syntaxHighlighting(sourceHighlight),
        EditorView.editorAttributes.of({ class: "cm-source" }),
      ];
  }
}

export const modeCompartment = new Compartment();

const handlersOf = new WeakMap<EditorView, PreviewHandlers>();

/** Remembers the click handlers of a view, for later mode changes. */
export function rememberHandlers(view: EditorView, handlers: PreviewHandlers) {
  handlersOf.set(view, handlers);
}

/** Switches a mounted editor to another mode without recreating it. */
export function setEditorMode(view: EditorView, mode: EditorMode) {
  view.dispatch({
    effects: modeCompartment.reconfigure(modeExtensions(mode, handlersOf.get(view))),
  });
}
