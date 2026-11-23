import { ensureSyntaxTree } from "@codemirror/language";
import { EditorSelection, EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { describe, expect, test } from "vite-plus/test";
import {
  extensions,
  isEditorMode,
  modeExtensions,
  previewSpecs,
  type DecoSpec,
  type EditorMode,
} from "../src/index.ts";
import { modeCompartment } from "../src/mode.ts";

const DOC = [
  "# Title",
  "",
  "Some **bold**, _em_, ~~gone~~ and `code` with [[Other note|an alias]] and #tag.",
  "A [link](https://example.com) and a bare https://example.org.",
  "",
  "> quoted",
  "",
  "- item",
  "- [ ] open task",
  "- [x] done task",
  "1. first",
  "",
  "---",
  "",
  "```ts",
  "const a = 1;",
  "```",
].join("\n");

function parsed(doc: string, cursor: number) {
  const s = EditorState.create({ doc, extensions, selection: EditorSelection.cursor(cursor) });
  ensureSyntaxTree(s, s.doc.length, 5000);
  return s;
}

// Spec order inside a code fence depends on the parse, so compare sorted.
const reading = (s: EditorState) =>
  previewSpecs(s, { activeLines: new Set(), reading: true })
    .map((x) => JSON.stringify(x))
    .sort()
    .map((x) => JSON.parse(x) as DecoSpec);
const classes = (all: DecoSpec[]) =>
  all.flatMap((x) => (x.type === "mark" || x.type === "line" ? [x.class] : []));

describe("reading mode", () => {
  test("shows no marker source with the cursor on any line", () => {
    const first = reading(parsed(DOC, 0));
    expect(first.some((x) => x.type === "hide")).toBe(true);
    // Whatever the cursor does, the specs are the same: nothing has a cursor line.
    let at = 0;
    for (const line of DOC.split("\n")) {
      expect(reading(parsed(DOC, at + Math.floor(line.length / 2)))).toEqual(first);
      at += line.length + 1;
    }
    const cls = classes(first);
    expect(cls).not.toContain("cm-marker");
    expect(cls).not.toContain("cm-hr-source");
  });

  test("a cursor on a task does not turn its checkbox into source", () => {
    const at = DOC.indexOf("[ ]");
    const sp = reading(parsed(DOC, at + 1));
    const boxes = sp.filter((x) => x.type === "widget" && x.widget === "checkbox");
    expect(boxes).toHaveLength(2);
  });

  test("keeps links, tags and wikilinks clickable", () => {
    const sp = reading(parsed(DOC, 0));
    const attrs = sp.flatMap((x) => (x.type === "mark" && x.attrs ? Object.keys(x.attrs) : []));
    expect(attrs).toEqual(expect.arrayContaining(["data-wikilink", "data-tag", "data-href"]));
  });

  test("is read-only and not editable; live and source are not", () => {
    const make = (m: EditorMode) =>
      EditorState.create({ doc: DOC, extensions: [extensions, modeExtensions(m)] });
    expect(make("reading").readOnly).toBe(true);
    expect(make("reading").facet(EditorView.editable)).toBe(false);
    for (const m of ["live", "source"] as const) {
      expect(make(m).readOnly).toBe(false);
      expect(make(m).facet(EditorView.editable)).toBe(true);
    }
  });
});

describe("switching modes", () => {
  const start = () =>
    EditorState.create({
      doc: DOC,
      extensions: [extensions, modeCompartment.of(modeExtensions("live"))],
      selection: EditorSelection.cursor(20),
    });

  test("keeps the document and the selection through every mode", () => {
    let s = start();
    for (const m of ["reading", "source", "live", "source", "reading", "live"] as const) {
      s = s.update({ effects: modeCompartment.reconfigure(modeExtensions(m)) }).state;
      expect(s.doc.toString()).toBe(DOC);
      expect(s.selection.main.head).toBe(20);
      expect(s.readOnly).toBe(m === "reading");
    }
  });

  test("knows its modes", () => {
    expect(["live", "reading", "source"].every(isEditorMode)).toBe(true);
    expect(isEditorMode("edit")).toBe(false);
    expect(isEditorMode(undefined)).toBe(false);
  });
});
