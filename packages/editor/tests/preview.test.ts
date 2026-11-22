import { ensureSyntaxTree } from "@codemirror/language";
import { EditorSelection, EditorState } from "@codemirror/state";
import { describe, expect, test } from "vite-plus/test";
import {
  activeLines,
  extensions,
  findTags,
  parseWikilink,
  previewSpecs,
  toggleTaskAt,
  type DecoSpec,
} from "../src/index.ts";

/** A parsed state with the cursor at `cursor` (default: end of doc). */
function state(doc: string, cursor = doc.length) {
  const s = EditorState.create({ doc, extensions, selection: EditorSelection.cursor(cursor) });
  ensureSyntaxTree(s, s.doc.length, 5000);
  return s;
}

/** Specs with the cursor's line active (focused) or none (blurred). */
function specs(doc: string, opts: { cursor?: number; focused?: boolean } = {}) {
  const s = state(doc, opts.cursor);
  return { s, specs: previewSpecs(s, { activeLines: activeLines(s, opts.focused ?? true) }) };
}

const text = (doc: string, sp: { from: number; to: number }) => doc.slice(sp.from, sp.to);
const hidden = (doc: string, all: DecoSpec[]) =>
  all.filter((x) => x.type === "hide").map((x) => text(doc, x as { from: number; to: number }));
const marks = (doc: string, all: DecoSpec[], cls: string) =>
  all
    .filter((x): x is Extract<DecoSpec, { type: "mark" }> => x.type === "mark" && x.class === cls)
    .map((x) => ({ text: text(doc, x), attrs: x.attrs }));
const lines = (all: DecoSpec[], cls: string) =>
  all
    .filter(
      (x): x is Extract<DecoSpec, { type: "line" }> =>
        x.type === "line" && x.class.split(" ").includes(cls),
    )
    .map((x) => x.from);

describe("headings", () => {
  const doc = "# Sync-protokoll\n\nText\n\n### Flöde";
  test("are sized, with the marker hidden off the active line", () => {
    const { specs: sp } = specs(doc, { cursor: doc.indexOf("Text") });
    expect(lines(sp, "cm-h1")).toEqual([0]);
    expect(lines(sp, "cm-h3")).toEqual([doc.indexOf("###")]);
    expect(hidden(doc, sp)).toEqual(["# ", "### "]);
  });
  test("show the marker on the line being edited", () => {
    const { specs: sp } = specs(doc, { cursor: 3 });
    expect(hidden(doc, sp)).toEqual(["### "]);
  });
  test("hide every marker when the editor is not focused", () => {
    const { specs: sp } = specs(doc, { cursor: 3, focused: false });
    expect(hidden(doc, sp)).toEqual(["# ", "### "]);
  });
});

describe("inline formatting", () => {
  const doc = "Det är **fett**, *kursivt*, ~~struket~~ och `kod`.\n";
  test("styles the text and hides the markers", () => {
    const { specs: sp } = specs(doc);
    expect(marks(doc, sp, "cm-strong").map((m) => m.text)).toEqual(["fett"]);
    expect(marks(doc, sp, "cm-em").map((m) => m.text)).toEqual(["kursivt"]);
    expect(marks(doc, sp, "cm-strike").map((m) => m.text)).toEqual(["struket"]);
    expect(marks(doc, sp, "cm-inline-code").map((m) => m.text)).toEqual(["kod"]);
    expect(hidden(doc, sp)).toEqual(["**", "**", "*", "*", "~~", "~~", "`", "`"]);
  });
  test("keeps markers visible but faint on the active line", () => {
    const { specs: sp } = specs(doc, { cursor: 2 });
    expect(hidden(doc, sp)).toEqual([]);
    expect(marks(doc, sp, "cm-marker").map((m) => m.text)).toEqual([
      "**",
      "**",
      "*",
      "*",
      "~~",
      "~~",
      "`",
      "`",
    ]);
  });
});

describe("wikilinks and tags", () => {
  test("parse targets, headings and aliases", () => {
    expect(parseWikilink("Sync-protokoll")).toEqual({
      target: "Sync-protokoll",
      heading: undefined,
      alias: undefined,
      embed: false,
    });
    expect(parseWikilink("Plan#Mål|planen", true)).toEqual({
      target: "Plan",
      heading: "Mål",
      alias: "planen",
      embed: true,
    });
  });
  test("render as clickable links without brackets", () => {
    const doc = "Se [[CLI-kommandon]] och [[Sync-protokoll#Flöde|flödet]].\n";
    const { specs: sp } = specs(doc);
    expect(marks(doc, sp, "cm-wikilink")).toEqual([
      { text: "CLI-kommandon", attrs: { "data-wikilink": "CLI-kommandon" } },
      { text: "flödet", attrs: { "data-wikilink": "Sync-protokoll", "data-heading": "Flöde" } },
    ]);
    expect(hidden(doc, sp)).toEqual(["[[", "]]", "[[Sync-protokoll#Flöde|", "]]"]);
  });
  test("show their source on the active line", () => {
    const doc = "Se [[Sync-protokoll|flödet]]";
    const { specs: sp } = specs(doc);
    expect(hidden(doc, sp)).toEqual([]);
    expect(marks(doc, sp, "cm-wikilink").map((m) => m.text)).toEqual(["Sync-protokoll|flödet"]);
  });
  test("tags need a letter and a boundary", () => {
    expect(
      findTags("#nota och #a/b, (#idé) men inte rubrik#x eller #123 eller #2026-mål/"),
    ).toEqual(["nota", "a/b", "idé", "2026-mål"]);
    const doc = "Taggat #nota och #fotboll/junior\n";
    const { specs: sp } = specs(doc);
    expect(marks(doc, sp, "cm-tag")).toEqual([
      { text: "#nota", attrs: { "data-tag": "nota" } },
      { text: "#fotboll/junior", attrs: { "data-tag": "fotboll/junior" } },
    ]);
  });
  test("a heading is not a tag", () => {
    const doc = "# Rubrik\n\n#tagg";
    const { specs: sp } = specs(doc, { cursor: 0, focused: false });
    expect(marks(doc, sp, "cm-tag").map((m) => m.text)).toEqual(["#tagg"]);
  });
});

describe("links, quotes, rules and code", () => {
  test("links show their text and keep the target", () => {
    const doc = "Läs [docs](https://nota.banjoanton.com/docs) och https://example.com\n";
    const { specs: sp } = specs(doc);
    expect(marks(doc, sp, "cm-link")).toEqual([
      { text: "docs", attrs: { "data-href": "https://nota.banjoanton.com/docs" } },
      { text: "https://example.com", attrs: { "data-href": "https://example.com" } },
    ]);
    expect(hidden(doc, sp)).toEqual(["[", "](https://nota.banjoanton.com/docs)"]);
  });
  test("blockquotes, rules and fenced code get line styles", () => {
    const doc =
      "> Filen på disk är sanningen.\n> Alltid.\n\n---\n\n```go\nfmt.Println(1)\n```\n\nslut";
    const { specs: sp } = specs(doc);
    expect(lines(sp, "cm-quote")).toEqual([0, doc.indexOf("> Alltid")]);
    expect(lines(sp, "cm-hr")).toEqual([doc.indexOf("---")]);
    expect(lines(sp, "cm-codeblock")).toEqual([
      doc.indexOf("```go"),
      doc.indexOf("fmt."),
      doc.lastIndexOf("```"),
    ]);
    expect(hidden(doc, sp)).toEqual(["> ", "> ", "---"]);
    expect(
      marks(doc, sp, "cm-fence")
        .map((m) => m.text)
        .sort(),
    ).toEqual(["```", "```", "go"]);
  });
});

describe("lists and tasks", () => {
  const doc = "- [x] Välj diff-lib\n- [ ] Namnformat\n  - inre punkt\n1. först\n\nslut";
  test("tasks become checkboxes, bullets become dots", () => {
    const { specs: sp } = specs(doc);
    const widgets = sp.filter(
      (x): x is Extract<DecoSpec, { type: "widget" }> => x.type === "widget",
    );
    expect(widgets.map((w) => [w.widget, text(doc, w), w.checked])).toEqual([
      ["checkbox", "[x]", true],
      ["checkbox", "[ ]", false],
      ["bullet", "-", undefined],
    ]);
    expect(hidden(doc, sp)).toEqual(["- ", "- "]);
    expect(marks(doc, sp, "cm-task-done").map((m) => m.text)).toEqual(["Välj diff-lib"]);
    expect(marks(doc, sp, "cm-list-mark").map((m) => m.text)).toEqual(["1."]);
    const hang = sp
      .filter(
        (x): x is Extract<DecoSpec, { type: "line" }> => x.type === "line" && x.class === "cm-list",
      )
      .map((x) => x.style);
    expect(hang).toEqual(["--hang:3ch", "--hang:3ch", "--hang:4ch", "--hang:3ch"]);
  });
  test("the cursor inside a checkbox shows its source", () => {
    const { specs: sp } = specs(doc, { cursor: 3 });
    const boxes = sp.filter((x) => x.type === "widget" && x.widget === "checkbox");
    expect(boxes.length).toBe(1);
  });
  test("toggleTaskAt ticks and unticks", () => {
    const s = state(doc, doc.length);
    const off = toggleTaskAt(s, 2);
    expect(off).toEqual({ from: 3, to: 4, insert: " " });
    const second = doc.indexOf("[ ]");
    expect(toggleTaskAt(s, second)).toEqual({ from: second + 1, to: second + 2, insert: "x" });
    expect(toggleTaskAt(s, doc.indexOf("slut"))).toBeNull();
    const after = s.update({ changes: toggleTaskAt(s, second)! }).state;
    expect(after.doc.line(2).text).toBe("- [x] Namnformat");
  });
});
