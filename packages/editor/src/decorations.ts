import { syntaxTree } from "@codemirror/language";
import type { EditorState } from "@codemirror/state";
import type { SyntaxNode, Tree } from "@lezer/common";
import { parseWikilink } from "./syntax.ts";

// Live preview: markdown stays plain text in the document, and
// decorations render it. Markers (#, **, `, [[ ]] …) are hidden except
// on the lines the cursor is on, as in Obsidian and Bear. The specs are
// computed from the editor state alone, so they can be tested without
// a DOM; livePreview() turns them into CodeMirror decorations.

export type WidgetKind =
  | "checkbox"
  | "bullet"
  | "callout"
  | "footnote"
  | "footnote-label"
  /** Text in place of the source: label. */
  | "text"
  /** A line break (<br>). */
  | "break";

/** GitHub's five alert kinds; Obsidian's callout types map onto them. */
export type CalloutKind = "note" | "tip" | "important" | "warning" | "caution";

export type DecoSpec =
  /** Hide a marker (replace it with nothing). */
  | { type: "hide"; from: number; to: number }
  /** Style a range: class plus data attributes. */
  | { type: "mark"; from: number; to: number; class: string; attrs?: Record<string, string> }
  /** Style a whole line (at its start). */
  | { type: "line"; from: number; class: string; style?: string }
  /** Replace a range with a widget. */
  | {
      type: "widget";
      from: number;
      to: number;
      widget: WidgetKind;
      checked?: boolean;
      /** A task's status besides done and open ("/", "-", …). */
      status?: string;
      callout?: CalloutKind;
      /** A callout's title when it has none of its own; a footnote's number; a text widget's text. */
      label?: string;
      /** A footnote's id, lower-cased ("^<pos>" for an inline footnote). */
      footnote?: string;
      /** An inline footnote's text. */
      note?: string;
    };

export type InlineSpec = Extract<DecoSpec, { type: "hide" | "mark" }>;

export type TableAlign = "left" | "center" | "right" | undefined;

export type TableCell = {
  /** Where the cell's text starts, from the start of the table. */
  offset: number;
  text: string;
  /** Inline formatting, with positions in the cell's text. */
  specs: InlineSpec[];
};

/** A table rendered in place of its source lines. */
export type TableSpec = {
  type: "table";
  /** Start of the table's first line. */
  from: number;
  /** End of the table's last line. */
  to: number;
  source: string;
  align: TableAlign[];
  header: TableCell[];
  rows: TableCell[][];
};

export interface PreviewOptions {
  /** Lines (1-based) where markers show; usually where the cursor is. */
  activeLines: ReadonlySet<number>;
  /** Reading mode: no cursor, so no marker is ever shown as source. */
  reading?: boolean;
  /** Only these ranges are decorated (the viewport); whole doc when absent. */
  ranges?: readonly { from: number; to: number }[];
}

/** The lines the selection touches, when the editor has focus. */
export function activeLines(state: EditorState, focused: boolean): Set<number> {
  const out = new Set<number>();
  if (!focused) return out;
  for (const r of state.selection.ranges) {
    const a = state.doc.lineAt(r.from).number;
    const b = state.doc.lineAt(r.to).number;
    for (let l = a; l <= b; l++) out.add(l);
  }
  return out;
}

const INLINE: Record<string, { cls: string; mark: string }> = {
  Emphasis: { cls: "cm-em", mark: "EmphasisMark" },
  StrongEmphasis: { cls: "cm-strong", mark: "EmphasisMark" },
  Strikethrough: { cls: "cm-strike", mark: "StrikethroughMark" },
  Highlight: { cls: "cm-highlight", mark: "HighlightMark" },
  InlineCode: { cls: "cm-inline-code", mark: "CodeMark" },
};

function children(node: SyntaxNode, name: string): SyntaxNode[] {
  const out: SyntaxNode[] = [];
  for (let c = node.firstChild; c; c = c.nextSibling) if (c.name === name) out.push(c);
  return out;
}

/**
 * A table renders when it is top-level (a table in a quote or list keeps
 * its source, since its lines carry other markers) and no line of it is
 * active. While the cursor is in it, the whole table shows as source.
 */
function tableRendered(state: EditorState, node: SyntaxNode, opts: PreviewOptions): boolean {
  if (node.parent?.name !== "Document") return false;
  const first = state.doc.lineAt(node.from).number;
  const last = state.doc.lineAt(node.to).number;
  for (let l = first; l <= last; l++) if (opts.activeLines.has(l)) return false;
  return true;
}

function tableAlign(row: string): TableAlign[] {
  return row
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((c) => {
      const s = c.trim();
      const left = s.startsWith(":");
      const right = s.endsWith(":");
      if (left && right) return "center";
      if (right) return "right";
      if (left) return "left";
      return undefined;
    });
}

function tableSpec(state: EditorState, node: SyntaxNode): TableSpec {
  const doc = state.doc;
  const from = doc.lineAt(node.from).from;
  const to = doc.lineAt(node.to).to;
  const delimiter = node.getChild("TableDelimiter");
  const header = node.getChild("TableHeader");
  const align = delimiter ? tableAlign(doc.sliceString(delimiter.from, delimiter.to)) : [];
  const columns = align.length || (header ? children(header, "TableCell").length : 0);

  // Empty cells have no TableCell node, so cells are placed by the pipes.
  const cellsOf = (row: SyntaxNode): TableCell[] => {
    const cells: TableCell[] = [];
    let col = 0;
    let lastPipe = row.from;
    const empty = (at: number): TableCell => ({ offset: at - from, text: "", specs: [] });
    for (let c = row.firstChild; c; c = c.nextSibling) {
      if (c.name === "TableDelimiter") {
        if (c.from > row.from) {
          cells[col] ??= empty(lastPipe);
          col++;
        }
        lastPipe = c.to;
        continue;
      }
      if (c.name !== "TableCell" || col >= columns) continue;
      const specs = collect(
        state,
        { activeLines: new Set(), reading: true, ranges: [{ from: c.from, to: c.to }] },
        true,
      )
        .filter((s): s is InlineSpec => s.type === "hide" || s.type === "mark")
        .filter((s) => s.from >= c.from && s.to <= c.to)
        .map((s) => ({ ...s, from: s.from - c.from, to: s.to - c.from }));
      cells[col] = { offset: c.from - from, text: doc.sliceString(c.from, c.to), specs };
    }
    const out: TableCell[] = [];
    for (let i = 0; i < columns; i++) out.push(cells[i] ?? empty(lastPipe));
    return out;
  };

  return {
    type: "table",
    from,
    to,
    source: doc.sliceString(from, to),
    align: Array.from({ length: columns }, (_, i) => align[i]),
    header: header ? cellsOf(header) : [],
    rows: children(node, "TableRow").map(cellsOf),
  };
}

/** The tables that render in place of their source, in the whole document. */
export function tableSpecs(state: EditorState, opts: PreviewOptions): TableSpec[] {
  const out: TableSpec[] = [];
  for (let n = syntaxTree(state).topNode.firstChild; n; n = n.nextSibling)
    if (n.name === "Table" && tableRendered(state, n, opts)) out.push(tableSpec(state, n));
  return out;
}

export interface Footnotes {
  /**
   * id → number, in the order of the first reference (as on GitHub);
   * inline footnotes (^[text]) are numbered along, as "^<pos>".
   */
  numbers: Map<string, number>;
  /** id → where its first reference starts. */
  refs: Map<string, number>;
  /** id → where its definition starts. */
  defs: Map<string, number>;
}

/** "[^Id]" or "[^Id]:" → "id". */
export const footnoteId = (source: string) =>
  source.replace(/^\[\^/, "").replace(/\]:?$/, "").toLowerCase();

const footnoteCache = new WeakMap<Tree, Footnotes>();

/**
 * The document's footnotes. A reference only counts, and gets a number,
 * when its footnote is defined somewhere.
 */
export function footnotes(state: EditorState): Footnotes {
  const tree = syntaxTree(state);
  const cached = footnoteCache.get(tree);
  if (cached) return cached;
  const refs = new Map<string, number>();
  const defs = new Map<string, number>();
  const order: string[] = [];
  tree.iterate({
    enter: (n) => {
      if (n.name === "InlineFootnote") {
        order.push(`^${n.from}`);
        defs.set(`^${n.from}`, n.from);
      } else if (n.name === "FootnoteRef") {
        const id = footnoteId(state.doc.sliceString(n.from, n.to));
        if (!refs.has(id)) {
          refs.set(id, n.from);
          order.push(id);
        }
      } else if (n.name === "FootnoteLabel") {
        const id = footnoteId(state.doc.sliceString(n.from, n.to));
        if (!defs.has(id)) defs.set(id, n.from);
      }
    },
  });
  const numbers = new Map<string, number>();
  for (const id of order) if (defs.has(id)) numbers.set(id, numbers.size + 1);
  for (const id of refs.keys()) if (!defs.has(id)) refs.delete(id);
  for (const id of order) if (id.startsWith("^")) defs.delete(id);
  const out = { numbers, refs, defs };
  footnoteCache.set(tree, out);
  return out;
}

// The named entities people type; others stay as written.
const ENTITIES: Record<string, string> = {
  nbsp: "\u00a0",
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
  copy: "©",
  reg: "®",
  trade: "™",
  hellip: "…",
  mdash: "—",
  ndash: "–",
  laquo: "«",
  raquo: "»",
  lsquo: "‘",
  rsquo: "’",
  ldquo: "“",
  rdquo: "”",
  larr: "←",
  rarr: "→",
  uarr: "↑",
  darr: "↓",
  harr: "↔",
  lArr: "⇐",
  rArr: "⇒",
  hArr: "⇔",
  times: "×",
  divide: "÷",
  plusmn: "±",
  minus: "−",
  ne: "≠",
  le: "≤",
  ge: "≥",
  asymp: "≈",
  infin: "∞",
  deg: "°",
  micro: "µ",
  middot: "·",
  bull: "•",
  sect: "§",
  para: "¶",
  euro: "€",
  pound: "£",
  yen: "¥",
  cent: "¢",
  half: "½",
  frac12: "½",
  frac14: "¼",
  frac34: "¾",
  sup2: "²",
  sup3: "³",
  check: "✓",
  shy: "\u00ad",
  ensp: "\u2002",
  emsp: "\u2003",
  thinsp: "\u2009",
  zwj: "\u200d",
  zwnj: "\u200c",
};

/** The character of "&copy;" or "&#8594;" or "&#x2192;", or undefined. */
export function decodeEntity(source: string): string | undefined {
  const m = /^&(?:#(\d{1,7})|#[xX]([\da-fA-F]{1,6})|(\w+));$/.exec(source);
  if (!m) return undefined;
  if (m[3]) return ENTITIES[m[3]];
  const code = m[1] ? Number(m[1]) : Number.parseInt(m[2]!, 16);
  if (code === 0 || code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff)) return "\ufffd";
  return String.fromCodePoint(code);
}

// Inline HTML that only formats: its tags hide and its text gets the
// class. No HTML is ever rendered, and attributes are ignored.
const HTML_CLASSES: Record<string, string> = {
  kbd: "cm-kbd",
  sup: "cm-sup",
  sub: "cm-sub",
  mark: "cm-highlight",
  u: "cm-underline",
  ins: "cm-underline",
  s: "cm-strike",
  del: "cm-strike",
  strike: "cm-strike",
  b: "cm-strong",
  strong: "cm-strong",
  i: "cm-em",
  em: "cm-em",
  small: "cm-small",
};

/** "<kbd>" → open kbd, "</kbd>" → close kbd, "<br/>" → br; anything else null. */
function htmlTag(source: string): { name: string; close: boolean } | null {
  const m = /^<(\/?)([a-zA-Z]+)(?:\s[^<>]*)?\/?>$/.exec(source);
  if (!m) return null;
  const name = m[2]!.toLowerCase();
  if (name === "br") return m[1] ? null : { name, close: false };
  return HTML_CLASSES[name] ? { name, close: !!m[1] } : null;
}

/**
 * Obsidian's task statuses besides [ ] and [x]: in progress, cancelled,
 * forwarded, scheduled, important, question, star.
 */
export const TASK_STATUSES = ["/", "-", ">", "<", "!", "?", "*"];
/** "[/] " after a list marker: a task with another status. */
const STATUS_TASK = /^\[([^\]\n])\][ \t]/;

/** "[Some  Label]" → "some label", as CommonMark matches reference labels. */
const refLabel = (source: string) =>
  source
    .replace(/^\[|\]$/g, "")
    .trim()
    .replace(/\s+/g, " ")
    .toLowerCase();

/** A link destination without its optional <angle brackets>. */
const destination = (source: string) => source.replace(/^<(.*)>$/, "$1");

const linkRefCache = new WeakMap<Tree, Map<string, string>>();

/** The document's link reference definitions: label → destination (first wins). */
export function linkRefs(state: EditorState): Map<string, string> {
  const tree = syntaxTree(state);
  const cached = linkRefCache.get(tree);
  if (cached) return cached;
  const out = new Map<string, string>();
  tree.iterate({
    enter: (n) => {
      if (n.name !== "LinkReference") return;
      const label = n.node.getChild("LinkLabel");
      const url = n.node.getChild("URL");
      if (!label || !url) return false;
      const key = refLabel(state.doc.sliceString(label.from, label.to));
      if (!out.has(key)) out.set(key, destination(state.doc.sliceString(url.from, url.to)));
      return false;
    },
  });
  linkRefCache.set(tree, out);
  return out;
}

const CALLOUT_KINDS: Record<string, CalloutKind> = {
  note: "note",
  info: "note",
  todo: "note",
  abstract: "note",
  summary: "note",
  tldr: "note",
  quote: "note",
  cite: "note",
  tip: "tip",
  hint: "tip",
  success: "tip",
  check: "tip",
  done: "tip",
  important: "important",
  question: "important",
  help: "important",
  faq: "important",
  example: "important",
  warning: "warning",
  attention: "warning",
  caution: "caution",
  failure: "caution",
  fail: "caution",
  missing: "caution",
  danger: "caution",
  error: "caution",
  bug: "caution",
};

/** The kind of a callout type ("info" → note); unknown types are notes. */
export const calloutKind = (type: string): CalloutKind =>
  CALLOUT_KINDS[type.toLowerCase()] ?? "note";

/** "> [!type]± title" on a quote's first line, from the quote's ">". */
const CALLOUT = /^(>[ \t]?)\[!([\w-]+)\]([+-]?)([ \t]*)(.*)$/;

/** Computes the live-preview decorations of a state. */
export function previewSpecs(state: EditorState, opts: PreviewOptions): DecoSpec[] {
  return collect(state, opts, false);
}

/** inTable: decorating the inside of a table's cells, for its widget. */
function collect(state: EditorState, opts: PreviewOptions, inTable: boolean): DecoSpec[] {
  const doc = state.doc;
  const specs: DecoSpec[] = [];
  const lineOf = (pos: number) => doc.lineAt(pos).number;
  const active = (pos: number) => opts.activeLines.has(lineOf(pos));
  // The selection overlaps [from, to]: show that marker's source.
  const touched = (from: number, to: number) =>
    !opts.reading && state.selection.ranges.some((r) => r.from <= to && r.to >= from);
  const lineDone = new Set<string>();
  // Closing HTML tags already handled with their opening tag.
  const closed = new Set<number>();
  const line = (pos: number, cls: string, style?: string) => {
    const l = doc.lineAt(pos);
    const key = `${l.from}:${cls}`;
    if (lineDone.has(key)) return;
    lineDone.add(key);
    specs.push(
      style
        ? { type: "line", from: l.from, class: cls, style }
        : { type: "line", from: l.from, class: cls },
    );
  };
  const hide = (from: number, to: number) => {
    if (to > from) specs.push({ type: "hide", from, to });
  };
  const mark = (from: number, to: number, cls: string, attrs?: Record<string, string>) => {
    if (to > from)
      specs.push(
        attrs
          ? { type: "mark", from, to, class: cls, attrs }
          : { type: "mark", from, to, class: cls },
      );
  };
  // Swallow one space after a block marker ("# ", "> ").
  const withSpace = (to: number) => (doc.sliceString(to, to + 1) === " " ? to + 1 : to);

  const ranges = opts.ranges ?? [{ from: 0, to: doc.length }];
  const tree = syntaxTree(state);
  for (const { from, to } of ranges) {
    tree.iterate({
      from,
      to,
      enter: (ref) => {
        const node = ref.node;
        const name = ref.name;
        const heading = /^ATXHeading(\d)$/.exec(name);
        if (heading) {
          line(ref.from, `cm-h cm-h${heading[1]}`);
          if (!active(ref.from))
            for (const m of children(node, "HeaderMark")) hide(m.from, withSpace(m.to));
          return;
        }
        const setext = /^SetextHeading(\d)$/.exec(name);
        if (setext) {
          // The text's lines are the heading; the ===/--- line under it
          // shrinks to a gap once its marker is hidden.
          const underline = children(node, "HeaderMark").at(-1);
          const shown = [lineOf(ref.from), lineOf(ref.to)].some((l) => opts.activeLines.has(l));
          for (let l = lineOf(ref.from); l <= lineOf(ref.to); l++) {
            const at = doc.line(l).from;
            if (underline && at === doc.lineAt(underline.from).from) {
              if (!shown) line(at, "cm-setext-underline");
            } else line(at, `cm-h cm-h${setext[1]}`);
          }
          if (underline) {
            if (shown) mark(underline.from, underline.to, "cm-marker");
            else hide(underline.from, underline.to);
          }
          return;
        }
        const inline = INLINE[name];
        if (inline) {
          const marks = children(node, inline.mark);
          const first = marks[0];
          const last = marks[marks.length - 1];
          const inner =
            first && last && marks.length > 1
              ? { from: first.to, to: last.from }
              : { from: ref.from, to: ref.to };
          mark(inner.from, inner.to, inline.cls);
          if (!active(ref.from)) for (const m of marks) hide(m.from, m.to);
          else for (const m of marks) mark(m.from, m.to, "cm-marker");
          return name === "InlineCode" ? false : undefined;
        }
        switch (name) {
          case "WikiLink": {
            const marks = children(node, "WikiLinkMark");
            const open = marks[0];
            const close = marks[1];
            if (!open || !close) return false;
            const innerText = doc.sliceString(open.to, close.from);
            const wl = parseWikilink(innerText, doc.sliceString(ref.from, ref.from + 1) === "!");
            const bar = innerText.indexOf("|");
            const shown = !active(ref.from) && bar >= 0 ? open.to + bar + 1 : open.to;
            mark(shown, close.from, "cm-wikilink", {
              "data-wikilink": wl.target,
              ...(wl.heading ? { "data-heading": wl.heading } : {}),
            });
            if (!active(ref.from)) {
              hide(open.from, shown);
              hide(close.from, close.to);
            } else {
              mark(open.from, open.to, "cm-marker");
              mark(close.from, close.to, "cm-marker");
            }
            return false;
          }
          case "Comment":
          case "CommentBlock": {
            // Hidden when reading; faint while writing, so it isn't lost.
            if (opts.reading) {
              if (name === "Comment") hide(ref.from, ref.to);
              else
                for (let l = lineOf(ref.from); l <= lineOf(ref.to); l++) {
                  const ln = doc.line(l);
                  line(ln.from, "cm-comment-hidden");
                  hide(ln.from, ln.to);
                }
              return false;
            }
            mark(ref.from, ref.to, "cm-comment");
            for (const m of children(node, "CommentMark")) mark(m.from, m.to, "cm-marker");
            return false;
          }
          case "Entity": {
            const ch = decodeEntity(doc.sliceString(ref.from, ref.to));
            if (ch === undefined || active(ref.from)) return false;
            if (inTable) mark(ref.from, ref.to, "cm-entity", { "data-text": ch });
            else
              specs.push({ type: "widget", from: ref.from, to: ref.to, widget: "text", label: ch });
            return false;
          }
          case "HTMLTag": {
            const tag = htmlTag(doc.sliceString(ref.from, ref.to));
            if (!tag || closed.has(ref.from)) return false;
            if (tag.name === "br") {
              if (active(ref.from) || inTable) mark(ref.from, ref.to, "cm-marker");
              else specs.push({ type: "widget", from: ref.from, to: ref.to, widget: "break" });
              return false;
            }
            if (tag.close) return false; // without its opening tag: source
            // Its closing tag: the next one of the same name at this level.
            let depth = 0;
            let close: SyntaxNode | null = null;
            for (let c = node.nextSibling; c && !close; c = c.nextSibling) {
              if (c.name !== "HTMLTag") continue;
              const t = htmlTag(doc.sliceString(c.from, c.to));
              if (t?.name !== tag.name) continue;
              if (!t.close) depth++;
              else if (depth-- === 0) close = c;
            }
            if (!close) return false;
            closed.add(close.from);
            mark(ref.to, close.from, HTML_CLASSES[tag.name]!);
            for (const t of [ref, close]) {
              if (active(t.from)) mark(t.from, t.to, "cm-marker");
              else hide(t.from, t.to);
            }
            return false;
          }
          case "Escape":
            // "\*" shows as "*": the backslash is a marker.
            if (active(ref.from)) mark(ref.from, ref.from + 1, "cm-marker");
            else hide(ref.from, ref.from + 1);
            return false;
          case "Tag":
            mark(ref.from, ref.to, "cm-tag", { "data-tag": doc.sliceString(ref.from + 1, ref.to) });
            return false;
          case "FootnoteRef": {
            const id = footnoteId(doc.sliceString(ref.from, ref.to));
            const n = footnotes(state).numbers.get(id);
            if (n === undefined) return false; // never defined: plain text
            if (active(ref.from)) {
              mark(ref.from, ref.to, "cm-footnote-source");
            } else if (inTable) {
              // A cell is rendered from inline specs only: a mark that the
              // table widget draws as its data-text.
              mark(ref.from, ref.to, "cm-footnote-ref", {
                "data-footnote": id,
                "data-text": String(n),
              });
            } else {
              specs.push({
                type: "widget",
                from: ref.from,
                to: ref.to,
                widget: "footnote",
                footnote: id,
                label: String(n),
              });
            }
            return false;
          }
          case "InlineFootnote": {
            const id = `^${ref.from}`;
            const n = String(footnotes(state).numbers.get(id) ?? "");
            if (active(ref.from)) {
              for (const m of children(node, "InlineFootnoteMark")) mark(m.from, m.to, "cm-marker");
              mark(ref.from, ref.to, "cm-footnote-inline");
              return;
            }
            if (inTable) {
              mark(ref.from, ref.to, "cm-footnote-ref", { "data-text": n });
            } else {
              specs.push({
                type: "widget",
                from: ref.from,
                to: ref.to,
                widget: "footnote",
                footnote: id,
                label: n,
                note: doc.sliceString(ref.from + 2, ref.to - 1),
              });
            }
            return false;
          }
          case "FootnoteDefinition":
            for (let l = lineOf(ref.from); l <= lineOf(ref.to); l++)
              line(doc.line(l).from, "cm-footnote-def");
            return;
          case "FootnoteLabel": {
            const id = footnoteId(doc.sliceString(ref.from, ref.to));
            if (active(ref.from)) {
              mark(ref.from, ref.to, "cm-marker");
              return false;
            }
            const n = footnotes(state).numbers.get(id);
            specs.push({
              type: "widget",
              from: ref.from,
              to: withSpace(ref.to),
              widget: "footnote-label",
              footnote: id,
              label: n === undefined ? id : String(n),
            });
            return false;
          }
          case "Link": {
            const linkMarks = children(node, "LinkMark");
            const url = children(node, "URL")[0];
            const openMark = linkMarks[0];
            const textEnd = linkMarks[1];
            if (!openMark || !textEnd) return false;
            let href: string | undefined;
            if (url) href = destination(doc.sliceString(url.from, url.to));
            else {
              // [text][label], [text][] or [text]: a reference, when defined.
              const label = node.getChild("LinkLabel");
              const key = refLabel(
                label && label.to - label.from > 2
                  ? doc.sliceString(label.from, label.to)
                  : doc.sliceString(openMark.to, textEnd.from),
              );
              href = linkRefs(state).get(key);
            }
            if (href === undefined) return; // [text] without a target: plain text
            mark(openMark.to, textEnd.from, "cm-link", { "data-href": href });
            if (!active(ref.from)) {
              hide(openMark.from, openMark.to);
              hide(textEnd.from, ref.to);
            } else {
              mark(textEnd.from, ref.to, "cm-marker");
            }
            return false;
          }
          case "URL":
            // A bare or <autolink> URL, or a reference's destination (link
            // targets returned above).
            mark(ref.from, ref.to, "cm-link", {
              "data-href": destination(doc.sliceString(ref.from, ref.to)),
            });
            return false;
          case "LinkReference":
            line(ref.from, "cm-linkref");
            return;
          case "Blockquote": {
            const first = doc.lineAt(ref.from);
            const m = CALLOUT.exec(doc.sliceString(ref.from, first.to));
            const last = lineOf(ref.to);
            if (!m) {
              // A quote in a quote: the outer one draws a border per level.
              if (node.parent?.name === "Blockquote") return;
              const depth = new Map<number, number>();
              const nest = (q: SyntaxNode, d: number) => {
                for (let l = lineOf(q.from); l <= lineOf(q.to); l++) depth.set(l, d);
                for (let c = q.firstChild; c; c = c.nextSibling)
                  if (c.name === "Blockquote") nest(c, d + 1);
              };
              nest(node, 1);
              for (let l = first.number; l <= last; l++) {
                const d = depth.get(l) ?? 1;
                line(doc.line(l).from, "cm-quote", d > 1 ? `--quote-depth:${d}` : undefined);
              }
              return;
            }
            const [, lead = "", type = "", fold = "", gap = "", title = ""] = m;
            const kind = calloutKind(type);
            for (let l = first.number; l <= last; l++) {
              const cls = [`cm-callout cm-callout-${kind}`];
              if (l === first.number) cls.push("cm-callout-first cm-callout-title");
              if (l === last) cls.push("cm-callout-last");
              line(doc.line(l).from, cls.join(" "));
            }
            const markFrom = ref.from + lead.length;
            const markTo = markFrom + type.length + 3 + fold.length;
            const titleFrom = markTo + gap.length;
            mark(titleFrom, titleFrom + title.trimEnd().length, "cm-callout-title-text");
            if (active(ref.from)) {
              mark(markFrom, markTo, "cm-marker");
            } else {
              specs.push({
                type: "widget",
                from: markFrom,
                to: titleFrom,
                widget: "callout",
                callout: kind,
                ...(title.trim()
                  ? {}
                  : { label: type[0]!.toUpperCase() + type.slice(1).toLowerCase() }),
              });
            }
            return;
          }
          case "QuoteMark":
            if (!active(ref.from)) hide(ref.from, withSpace(ref.to));
            else mark(ref.from, ref.to, "cm-marker");
            return;
          case "Table": {
            if (inTable) return;
            // Rendered tables are block widgets (tableSpecs), not decorated here.
            if (tableRendered(state, node, opts)) return false;
            for (let pos = ref.from; pos <= ref.to;) {
              const l = doc.lineAt(pos);
              line(l.from, "cm-table-source");
              pos = l.to + 1;
            }
            for (let row = node.firstChild; row; row = row.nextSibling) {
              if (row.name === "TableDelimiter") mark(row.from, row.to, "cm-marker");
              else if (row.name === "QuoteMark") {
                // A table in a quote holds the "> " of its later lines.
                if (!active(row.from)) hide(row.from, withSpace(row.to));
                else mark(row.from, row.to, "cm-marker");
              } else
                for (const p of children(row, "TableDelimiter")) mark(p.from, p.to, "cm-marker");
            }
            return false;
          }
          case "HorizontalRule":
            line(ref.from, active(ref.from) ? "cm-hr-source" : "cm-hr");
            if (!active(ref.from)) hide(ref.from, ref.to);
            return false;
          case "FencedCode": {
            const first = lineOf(ref.from);
            const last = lineOf(ref.to);
            for (let l = first; l <= last; l++) {
              const ln = doc.line(l);
              line(
                ln.from,
                l === first
                  ? "cm-codeblock cm-codeblock-first"
                  : l === last
                    ? "cm-codeblock cm-codeblock-last"
                    : "cm-codeblock",
              );
            }
            for (const c of [...children(node, "CodeMark"), ...children(node, "CodeInfo")])
              mark(c.from, c.to, "cm-fence");
            return false;
          }
          case "ListItem": {
            const lm = children(node, "ListMark")[0];
            if (!lm) return;
            const l = doc.lineAt(lm.from);
            const bullet = node.parent?.name === "BulletList";
            const task = node.getChild("Task");
            const marker = task?.getChild("TaskMarker");
            const indent = lm.from - l.from;
            const isActive = active(lm.from);
            // Hanging indent: wrapped lines line up with the text.
            let prefix = indent + (lm.to - lm.from) + 1;
            if (marker) {
              const checked = /x/i.test(doc.sliceString(marker.from, marker.to));
              if (!isActive) hide(lm.from, withSpace(lm.to));
              if (!touched(marker.from, marker.to))
                specs.push({
                  type: "widget",
                  from: marker.from,
                  to: marker.to,
                  widget: "checkbox",
                  checked,
                });
              if (checked) mark(withSpace(marker.to), task!.to, "cm-task-done");
              prefix = isActive ? indent + (marker.to - lm.from) + 1 : indent + 3;
            } else if (
              STATUS_TASK.test(doc.sliceString(withSpace(lm.to), l.to)) &&
              TASK_STATUSES.includes(doc.sliceString(withSpace(lm.to) + 1, withSpace(lm.to) + 2))
            ) {
              const from = withSpace(lm.to);
              const status = doc.sliceString(from + 1, from + 2);
              if (!isActive) hide(lm.from, from);
              if (!touched(from, from + 3))
                specs.push({ type: "widget", from, to: from + 3, widget: "checkbox", status });
              if (status === "-") mark(withSpace(from + 3), node.to, "cm-task-done");
              prefix = isActive ? indent + (from + 3 - lm.from) + 1 : indent + 3;
            } else if (bullet && !isActive) {
              specs.push({ type: "widget", from: lm.from, to: lm.to, widget: "bullet" });
            } else {
              mark(lm.from, lm.to, "cm-list-mark");
            }
            line(l.from, "cm-list", `--hang:${prefix}ch`);
            return;
          }
        }
        return;
      },
    });
  }
  return specs;
}

/** The change that ticks or unticks the task whose marker starts at pos. */
export function toggleTaskAt(
  state: EditorState,
  pos: number,
): { from: number; to: number; insert: string } | null {
  // Another status ([/], [-] …) is a list item's text, not a TaskMarker:
  // a click marks it done.
  const status = state.doc.sliceString(pos, pos + 3);
  if (/^\[.\]$/.test(status) && TASK_STATUSES.includes(status[1]!))
    return { from: pos + 1, to: pos + 2, insert: "x" };
  let found: { from: number; to: number } | null = null;
  syntaxTree(state).iterate({
    from: pos,
    to: pos + 3,
    enter: (n) => {
      if (n.name === "TaskMarker" && n.from <= pos && n.to > pos)
        found = { from: n.from, to: n.to };
    },
  });
  if (!found) return null;
  const { from, to } = found;
  const checked = /x/i.test(state.doc.sliceString(from, to));
  return { from: from + 1, to: from + 2, insert: checked ? " " : "x" };
}
