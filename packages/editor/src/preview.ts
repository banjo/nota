import { syntaxTree } from "@codemirror/language";
import {
  type EditorState,
  type Extension,
  type Range,
  StateEffect,
  StateField,
} from "@codemirror/state";
import {
  Decoration,
  type DecorationSet,
  EditorView,
  ViewPlugin,
  type ViewUpdate,
  WidgetType,
} from "@codemirror/view";
import {
  activeLines,
  type DecoSpec,
  type InlineSpec,
  previewSpecs,
  type TableCell,
  type TableSpec,
  tableSpecs,
  toggleTaskAt,
} from "./decorations.ts";

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

/** Text with its inline specs as spans: hidden ranges dropped, marks nested. */
function renderInline(parent: HTMLElement, text: string, specs: InlineSpec[]) {
  const cuts = new Set([0, text.length]);
  for (const s of specs) {
    cuts.add(s.from);
    cuts.add(s.to);
  }
  const points = [...cuts].sort((a, b) => a - b);
  const marks = specs
    .filter((s): s is Extract<InlineSpec, { type: "mark" }> => s.type === "mark")
    .sort((a, b) => a.from - b.from || b.to - a.to);
  for (let i = 0; i < points.length - 1; i++) {
    const from = points[i]!;
    const to = points[i + 1]!;
    if (specs.some((s) => s.type === "hide" && s.from <= from && s.to >= to)) continue;
    let node: Node = document.createTextNode(text.slice(from, to));
    for (const m of marks.filter((m) => m.from <= from && m.to >= to).reverse()) {
      const span = document.createElement("span");
      span.className = m.class;
      for (const [k, v] of Object.entries(m.attrs ?? {})) span.setAttribute(k, v);
      span.append(node);
      node = span;
    }
    parent.append(node);
  }
}

class TableWidget extends WidgetType {
  constructor(readonly table: TableSpec) {
    super();
  }
  eq(other: TableWidget) {
    return other.table.source === this.table.source;
  }
  toDOM() {
    const wrap = document.createElement("div");
    wrap.className = "cm-table-widget";
    const table = document.createElement("table");
    const row = (cells: TableCell[], tag: "th" | "td") => {
      const tr = document.createElement("tr");
      cells.forEach((cell, i) => {
        const el = document.createElement(tag);
        const align = this.table.align[i];
        if (align) el.style.textAlign = align;
        el.dataset.offset = String(cell.offset + cell.text.length);
        renderInline(el, cell.text, cell.specs);
        tr.append(el);
      });
      return tr;
    };
    const head = document.createElement("thead");
    head.append(row(this.table.header, "th"));
    const body = document.createElement("tbody");
    for (const r of this.table.rows) body.append(row(r, "td"));
    table.append(head);
    if (this.table.rows.length) table.append(body);
    wrap.append(table);
    return wrap;
  }
  ignoreEvent() {
    return false;
  }
}

const setFocused = StateEffect.define<boolean>();

/** Whether the editor has focus, as state, for the table field. */
const focused = StateField.define<boolean>({
  create: () => false,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setFocused)) return e.value;
    return value;
  },
});

const tableDecorations = (state: EditorState, reading: boolean): DecorationSet =>
  Decoration.set(
    tableSpecs(state, {
      activeLines: reading ? new Set<number>() : activeLines(state, state.field(focused)),
      reading,
    }).map((t) =>
      Decoration.replace({ widget: new TableWidget(t), block: true }).range(t.from, t.to),
    ),
  );

/**
 * Rendered tables replace whole lines, which only a state field may do
 * (block decorations can't come from a view plugin).
 */
const tablesField = (reading: boolean) =>
  StateField.define<DecorationSet>({
    create: (state) => tableDecorations(state, reading),
    update(deco, tr) {
      const changed =
        tr.docChanged ||
        tr.selection ||
        tr.effects.some((e) => e.is(setFocused)) ||
        syntaxTree(tr.state) !== syntaxTree(tr.startState);
      return changed ? tableDecorations(tr.state, reading) : deco;
    },
    provide: (f) => EditorView.decorations.from(f),
  });

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
export function livePreview(
  handlers: PreviewHandlers = {},
  opts: { reading?: boolean } = {},
): Extension {
  const reading = !!opts.reading;
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
            // Reading mode has no cursor line: every marker is hidden.
            activeLines: reading ? new Set<number>() : activeLines(view.state, view.hasFocus),
            reading,
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
      const onSourceLine = !reading && activeLines(view.state, view.hasFocus).has(lineNo);
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

  // A click on a rendered table puts the cursor at the end of that cell,
  // which shows the table's source (Live only).
  const tableClicks = EditorView.domEventHandlers({
    mousedown(event, view) {
      if (reading || event.button !== 0) return false;
      const target = event.target as HTMLElement;
      const wrap = target.closest<HTMLElement>(".cm-table-widget");
      if (!wrap) return false;
      const cell = target.closest<HTMLElement>("[data-offset]");
      const start = view.posAtDOM(wrap);
      const anchor = cell ? start + Number(cell.dataset.offset) : view.state.doc.lineAt(start).to;
      view.dispatch({ selection: { anchor }, scrollIntoView: true, userEvent: "select.pointer" });
      view.focus();
      event.preventDefault();
      return true;
    },
  });

  return [
    focused,
    EditorView.focusChangeEffect.of((_, focusing) => setFocused.of(focusing)),
    tablesField(reading),
    plugin,
    clicks,
    tableClicks,
  ];
}
