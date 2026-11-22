import type { Extension, Range } from "@codemirror/state";
import {
  Decoration,
  type DecorationSet,
  EditorView,
  ViewPlugin,
  type ViewUpdate,
  WidgetType,
} from "@codemirror/view";
import { activeLines, type DecoSpec, previewSpecs, toggleTaskAt } from "./decorations.ts";

class CheckboxWidget extends WidgetType {
  constructor(readonly checked: boolean) {
    super();
  }
  eq(other: CheckboxWidget) {
    return other.checked === this.checked;
  }
  toDOM() {
    const box = document.createElement("span");
    box.className = "cm-task-checkbox";
    box.setAttribute("role", "checkbox");
    box.setAttribute("aria-checked", String(this.checked));
    box.dataset.checked = String(this.checked);
    return box;
  }
  ignoreEvent() {
    return false;
  }
}

class BulletWidget extends WidgetType {
  eq() {
    return true;
  }
  toDOM() {
    const b = document.createElement("span");
    b.className = "cm-bullet";
    b.textContent = "•";
    return b;
  }
}

const bullet = Decoration.replace({ widget: new BulletWidget() });
const hidden = Decoration.replace({});
const checkbox = (checked: boolean) => Decoration.replace({ widget: new CheckboxWidget(checked) });

/** Turns specs into a sorted decoration set. */
export function toDecorations(specs: DecoSpec[]): DecorationSet {
  const ranges: Range<Decoration>[] = [];
  for (const s of specs) {
    switch (s.type) {
      case "hide":
        ranges.push(hidden.range(s.from, s.to));
        break;
      case "mark":
        ranges.push(Decoration.mark({ class: s.class, attributes: s.attrs }).range(s.from, s.to));
        break;
      case "line":
        ranges.push(
          Decoration.line({
            class: s.class,
            attributes: s.style ? { style: s.style } : undefined,
          }).range(s.from),
        );
        break;
      case "widget":
        ranges.push((s.widget === "bullet" ? bullet : checkbox(!!s.checked)).range(s.from, s.to));
        break;
    }
  }
  return Decoration.set(ranges, true);
}

export interface PreviewHandlers {
  /** A click on a rendered [[wikilink]]. */
  onWikilink?: (target: string, heading?: string) => void;
  /** A click on a rendered #tag. */
  onTag?: (tag: string) => void;
  /** A click on a rendered link or URL. */
  onLink?: (href: string) => void;
}

/** The live-preview extension: decorations plus clickable widgets. */
export function livePreview(handlers: PreviewHandlers = {}): Extension {
  const plugin = ViewPlugin.fromClass(
    class {
      decorations: DecorationSet;
      constructor(view: EditorView) {
        this.decorations = this.build(view);
      }
      update(u: ViewUpdate) {
        if (u.docChanged || u.selectionSet || u.viewportChanged || u.focusChanged)
          this.decorations = this.build(u.view);
      }
      build(view: EditorView) {
        return toDecorations(
          previewSpecs(view.state, {
            activeLines: activeLines(view.state, view.hasFocus),
            ranges: view.visibleRanges,
          }),
        );
      }
    },
    { decorations: (v) => v.decorations },
  );

  const clicks = EditorView.domEventHandlers({
    mousedown(event, view) {
      const target = event.target as HTMLElement;
      const box = target.closest(".cm-task-checkbox");
      if (box) {
        const pos = view.posAtDOM(box);
        const change = toggleTaskAt(view.state, pos);
        if (change) view.dispatch({ changes: change, userEvent: "input.toggle" });
        event.preventDefault();
        return true;
      }
      // Rendered links open on click; on the line being edited (where
      // the source shows) they need ⌘/Ctrl, so clicking still places
      // the cursor.
      const clickable = target.closest<HTMLElement>("[data-wikilink], [data-tag], [data-href]");
      if (!clickable) return false;
      const lineNo = view.state.doc.lineAt(view.posAtDOM(clickable)).number;
      const onSourceLine = activeLines(view.state, view.hasFocus).has(lineNo);
      if (onSourceLine && !(event.metaKey || event.ctrlKey)) return false;
      const wl = target.closest<HTMLElement>("[data-wikilink]");
      if (wl && handlers.onWikilink) {
        handlers.onWikilink(wl.dataset.wikilink!, wl.dataset.heading);
        event.preventDefault();
        return true;
      }
      const tag = target.closest<HTMLElement>("[data-tag]");
      if (tag && handlers.onTag) {
        handlers.onTag(tag.dataset.tag!);
        event.preventDefault();
        return true;
      }
      const link = target.closest<HTMLElement>("[data-href]");
      if (link && handlers.onLink) {
        handlers.onLink(link.dataset.href!);
        event.preventDefault();
        return true;
      }
      return false;
    },
  });

  return [plugin, clicks];
}
