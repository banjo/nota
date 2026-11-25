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
  type CalloutKind,
  type DecoSpec,
  footnotes,
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
    // A mark with data-text stands in for its whole range (a footnote's number).
    const stand = marks.find((m) => m.attrs?.["data-text"] && m.from <= from && m.to >= to);
    if (stand && stand.from !== from) continue;
    let node: Node = document.createTextNode(
      stand ? stand.attrs!["data-text"]! : text.slice(from, to),
    );
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

class CalloutWidget extends WidgetType {
  constructor(
    readonly kind: CalloutKind,
    readonly label: string | undefined,
  ) {
    super();
  }
  eq(other: CalloutWidget) {
    return other.kind === this.kind && other.label === this.label;
  }
  toDOM() {
    const head = document.createElement("span");
    head.className = "cm-callout-head";
    const icon = document.createElement("span");
    icon.className = "cm-callout-icon";
    icon.dataset.callout = this.kind;
    icon.setAttribute("aria-hidden", "true");
    head.append(icon);
    if (this.label) {
      const label = document.createElement("span");
      label.className = "cm-callout-label";
      label.textContent = this.label;
      head.append(label);
    }
    return head;
  }
}

/** A footnote reference as its number, or a definition's number. */
class FootnoteWidget extends WidgetType {
  constructor(
    readonly id: string,
    readonly label: string,
    readonly definition: boolean,
  ) {
    super();
  }
  eq(other: FootnoteWidget) {
    return (
      other.id === this.id && other.label === this.label && other.definition === this.definition
    );
  }
  toDOM() {
    if (this.definition) {
      const el = document.createElement("span");
      el.className = "cm-footnote-label";
      el.dataset.footnoteDef = this.id;
      el.textContent = `${this.label}.`;
      return el;
    }
    const el = document.createElement("sup");
    el.className = "cm-footnote-ref";
    el.dataset.footnote = this.id;
    el.textContent = this.label;
    return el;
  }
  ignoreEvent() {
    return false;
  }
}

const flashLine = StateEffect.define<number | null>();

/** A line that briefly lights up after a jump to it. */
const flash = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    for (const e of tr.effects)
      if (e.is(flashLine))
        return e.value === null
          ? Decoration.none
          : Decoration.set(
              Decoration.line({ class: "cm-footnote-flash" }).range(
                tr.state.doc.lineAt(e.value).from,
              ),
            );
    return tr.docChanged ? deco.map(tr.changes) : deco;
  },
  provide: (f) => EditorView.decorations.from(f),
});

/** Scrolls to pos and lights its line up, without moving the cursor. */
function jumpTo(view: EditorView, pos: number) {
  view.dispatch({ effects: [EditorView.scrollIntoView(pos, { y: "center" }), flashLine.of(pos)] });
  setTimeout(() => {
    if (view.dom.isConnected) view.dispatch({ effects: flashLine.of(null) });
  }, 1200);
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
        if (s.widget === "footnote" || s.widget === "footnote-label") {
          const widget = new FootnoteWidget(
            s.footnote ?? "",
            s.label ?? "",
            s.widget === "footnote-label",
          );
          ranges.push(Decoration.replace({ widget }).range(s.from, s.to));
        } else if (s.widget === "callout") {
          const widget = new CalloutWidget(s.callout ?? "note", s.label);
          ranges.push(Decoration.replace({ widget }).range(s.from, s.to));
        } else {
          ranges.push((s.widget === "bullet" ? bullet : checkbox(!!s.checked)).range(s.from, s.to));
        }
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
      // A footnote jumps to its definition, and a definition's number
      // back to the first reference.
      const note = target.closest<HTMLElement>("[data-footnote], [data-footnote-def]");
      if (note && event.button === 0) {
        const fn = footnotes(view.state);
        const pos = note.dataset.footnote
          ? fn.defs.get(note.dataset.footnote)
          : fn.refs.get(note.dataset.footnoteDef!);
        if (pos !== undefined) {
          jumpTo(view, pos);
          event.preventDefault();
          return true;
        }
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
    flash,
    EditorView.focusChangeEffect.of((_, focusing) => setFocused.of(focusing)),
    tablesField(reading),
    plugin,
    clicks,
    tableClicks,
  ];
}
