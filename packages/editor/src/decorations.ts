import { syntaxTree } from "@codemirror/language";
import type { EditorState } from "@codemirror/state";
import type { SyntaxNode } from "@lezer/common";
import { parseWikilink } from "./syntax.ts";

// Live preview: markdown stays plain text in the document, and
// decorations render it. Markers (#, **, `, [[ ]] …) are hidden except
// on the lines the cursor is on, as in Obsidian and Bear. The specs are
// computed from the editor state alone, so they can be tested without
// a DOM; livePreview() turns them into CodeMirror decorations.

export type WidgetKind = "checkbox" | "bullet";

export type DecoSpec =
  /** Hide a marker (replace it with nothing). */
  | { type: "hide"; from: number; to: number }
  /** Style a range: class plus data attributes. */
  | { type: "mark"; from: number; to: number; class: string; attrs?: Record<string, string> }
  /** Style a whole line (at its start). */
  | { type: "line"; from: number; class: string; style?: string }
  /** Replace a range with a widget. */
  | { type: "widget"; from: number; to: number; widget: WidgetKind; checked?: boolean };

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
  InlineCode: { cls: "cm-inline-code", mark: "CodeMark" },
};

function children(node: SyntaxNode, name: string): SyntaxNode[] {
  const out: SyntaxNode[] = [];
  for (let c = node.firstChild; c; c = c.nextSibling) if (c.name === name) out.push(c);
  return out;
}

/** Computes the live-preview decorations of a state. */
export function previewSpecs(state: EditorState, opts: PreviewOptions): DecoSpec[] {
  const doc = state.doc;
  const specs: DecoSpec[] = [];
  const lineOf = (pos: number) => doc.lineAt(pos).number;
  const active = (pos: number) => opts.activeLines.has(lineOf(pos));
  // The selection overlaps [from, to]: show that marker's source.
  const touched = (from: number, to: number) =>
    !opts.reading && state.selection.ranges.some((r) => r.from <= to && r.to >= from);
  const lineDone = new Set<string>();
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
          case "Tag":
            mark(ref.from, ref.to, "cm-tag", { "data-tag": doc.sliceString(ref.from + 1, ref.to) });
            return false;
          case "Link": {
            const linkMarks = children(node, "LinkMark");
            const url = children(node, "URL")[0];
            const openMark = linkMarks[0];
            const textEnd = linkMarks[1];
            if (!openMark || !textEnd || !url) return false; // [text] without a target: plain text
            const href = doc.sliceString(url.from, url.to);
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
            // A bare or <autolink> URL (link targets returned above).
            mark(ref.from, ref.to, "cm-link", { "data-href": doc.sliceString(ref.from, ref.to) });
            return false;
          case "Blockquote":
            for (let pos = ref.from; pos <= ref.to;) {
              const l = doc.lineAt(pos);
              line(l.from, "cm-quote");
              pos = l.to + 1;
            }
            return;
          case "QuoteMark":
            if (!active(ref.from)) hide(ref.from, withSpace(ref.to));
            else mark(ref.from, ref.to, "cm-marker");
            return;
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
