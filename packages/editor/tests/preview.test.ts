import { ensureSyntaxTree, syntaxTree } from "@codemirror/language";
import { EditorSelection, EditorState } from "@codemirror/state";
import { describe, expect, test } from "vite-plus/test";
import {
  activeLines,
  calloutKind,
  footnotes,
  extensions,
  findTags,
  parseWikilink,
  previewSpecs,
  tableSpecs,
  toggleTaskAt,
  type DecoSpec,
} from "../src/index.ts";

/** A parsed state with the cursor at `cursor` (default: end of doc). */
function state(doc: string, cursor = doc.length) {
  const s = EditorState.create({ doc, extensions, selection: EditorSelection.cursor(cursor) });
  ensureSyntaxTree(s, s.doc.length, 5000);
  // syntaxTree(s) is still the tree of the state's first, time-boxed
  // parse (partial on a slow machine); a transaction picks up the rest.
  return s.update({}).state;
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

describe("tables", () => {
  const doc = [
    "Intro",
    "",
    "| Name | **Age** | Länk |",
    "|:--|:-:|--:|",
    "| Ann |  | [[Plan\\|planen]] |",
    "| Bo |",
    "",
    "slut",
  ].join("\n");
  const tables = (cursor: number, focused = true) => {
    const s = state(doc, cursor);
    return tableSpecs(s, { activeLines: activeLines(s, focused) });
  };

  test("render in place of their lines while the cursor is elsewhere", () => {
    const [t, ...rest] = tables(doc.length);
    expect(rest).toEqual([]);
    expect(text(doc, t!)).toBe(doc.split("\n").slice(2, 6).join("\n"));
    expect(t!.align).toEqual(["left", "center", "right"]);
    expect(t!.header.map((c) => c.text)).toEqual(["Name", "**Age**", "Länk"]);
    expect(t!.rows.map((r) => r.map((c) => c.text))).toEqual([
      ["Ann", "", "[[Plan\\|planen]]"],
      ["Bo", "", ""],
    ]);
  });

  test("cells carry their inline formatting", () => {
    const [t] = tables(doc.length);
    const age = t!.header[1]!;
    expect(age.specs.filter((s) => s.type === "hide").map((s) => text(age.text, s))).toEqual([
      "**",
      "**",
    ]);
    const link = t!.rows[0]![2]!;
    const wl = link.specs.find((s) => s.type === "mark" && s.class === "cm-wikilink");
    expect(wl && text(link.text, wl)).toBe("planen");
    expect(wl?.type === "mark" && wl.attrs).toEqual({ "data-wikilink": "Plan" });
  });

  test("cell offsets point at the cell's text in the source", () => {
    const [t] = tables(doc.length);
    for (const cell of [...t!.header, ...t!.rows.flat()].filter((c) => c.text))
      expect(doc.slice(t!.from + cell.offset).startsWith(cell.text)).toBe(true);
  });

  test("show their source, in monospace, while the cursor is in them", () => {
    const cursor = doc.indexOf("Ann");
    expect(tables(cursor)).toEqual([]);
    const { specs: sp } = specs(doc, { cursor });
    expect(lines(sp, "cm-table-source").length).toBe(4);
    expect(marks(doc, sp, "cm-marker").map((m) => m.text)).toContain("|:--|:-:|--:|");
    expect(hidden(doc, sp)).toEqual([]);
  });

  test("render when the editor is not focused, and are not decorated as source", () => {
    expect(tables(doc.indexOf("Ann"), false).length).toBe(1);
    const { specs: sp } = specs(doc, { cursor: doc.indexOf("Ann"), focused: false });
    expect(lines(sp, "cm-table-source")).toEqual([]);
  });

  test("in a quote keep their source", () => {
    const quoted = "> | a | b |\n> |---|---|\n> | 1 | 2 |\n\nslut";
    const s = state(quoted);
    expect(tableSpecs(s, { activeLines: new Set() })).toEqual([]);
    const sp = previewSpecs(s, { activeLines: new Set() });
    expect(hidden(quoted, sp)).toEqual(["> ", "> ", "> "]);
  });
});

describe("callouts", () => {
  const widgets = (all: DecoSpec[]) =>
    all.filter(
      (x): x is Extract<DecoSpec, { type: "widget" }> =>
        x.type === "widget" && x.widget === "callout",
    );

  test("a GitHub alert becomes a callout with its type as title", () => {
    const doc = "> [!WARNING]\n> Spara **först**.\n\nslut";
    const { specs: sp } = specs(doc);
    expect(lines(sp, "cm-callout-warning")).toEqual([0, doc.indexOf("> Spara")]);
    expect(lines(sp, "cm-callout-title")).toEqual([0]);
    expect(lines(sp, "cm-callout-last")).toEqual([doc.indexOf("> Spara")]);
    expect(lines(sp, "cm-quote")).toEqual([]);
    const [w, ...rest] = widgets(sp);
    expect(rest).toEqual([]);
    expect([text(doc, w!), w!.callout, w!.label]).toEqual(["[!WARNING]", "warning", "Warning"]);
    expect(hidden(doc, sp).filter((h) => h === "> ")).toHaveLength(2);
    expect(marks(doc, sp, "cm-strong").map((m) => m.text)).toEqual(["först"]);
  });

  test("an Obsidian callout keeps its own title, and maps its type", () => {
    const doc = "> [!info]- Mer om det  \n> text";
    const { specs: sp } = specs(doc, { cursor: doc.length });
    expect(lines(sp, "cm-callout-note")).toEqual([0, doc.indexOf("> text")]);
    const [w] = widgets(sp);
    expect([text(doc, w!), w!.callout, w!.label]).toEqual(["[!info]- ", "note", undefined]);
    expect(marks(doc, sp, "cm-callout-title-text").map((m) => m.text)).toEqual(["Mer om det"]);
  });

  test("Obsidian's types map onto GitHub's five; unknown ones are notes", () => {
    expect(
      ["tip", "success", "question", "bug", "attention", "Danger", "whatever"].map(calloutKind),
    ).toEqual(["tip", "tip", "important", "caution", "warning", "caution", "note"]);
  });

  test("show their source on the title line being edited", () => {
    const doc = "> [!TIP] Kort\n> text";
    const { specs: sp } = specs(doc, { cursor: 3 });
    expect(widgets(sp)).toEqual([]);
    expect(marks(doc, sp, "cm-marker").map((m) => m.text)).toContain("[!TIP]");
    expect(lines(sp, "cm-callout-tip")).toEqual([0, doc.indexOf("> text")]);
  });

  test("a plain quote stays a quote", () => {
    const doc = "> [inte] en callout\n> rad";
    const { specs: sp } = specs(doc);
    expect(lines(sp, "cm-quote")).toEqual([0, doc.indexOf("> rad")]);
    expect(widgets(sp)).toEqual([]);
  });
});

describe("footnotes", () => {
  const notes = (all: DecoSpec[], kind: "footnote" | "footnote-label") =>
    all
      .filter(
        (x): x is Extract<DecoSpec, { type: "widget" }> => x.type === "widget" && x.widget === kind,
      )
      .map((x) => [x.footnote, x.label]);
  const DOC = [
    "Andra[^b] och första[^A], igen[^b], okänd[^x].",
    "",
    "[^a]: Om **a**",
    "    fortsätter",
    "[^b]: Om b",
    "[^c]: Aldrig använd",
  ].join("\n");

  test("references are numbered by first use, case-insensitive", () => {
    const { s, specs: sp } = specs(DOC, { cursor: DOC.indexOf("fortsätter") });
    expect(notes(sp, "footnote")).toEqual([
      ["b", "1"],
      ["a", "2"],
      ["b", "1"],
    ]);
    // An undefined footnote stays text.
    expect(sp.some((x) => x.type !== "line" && text(DOC, x).includes("[^x]"))).toBe(false);
    expect(footnotes(s).numbers).toEqual(
      new Map([
        ["b", 1],
        ["a", 2],
      ]),
    );
    expect(footnotes(s).defs.get("a")).toBe(DOC.indexOf("[^a]:"));
    expect(footnotes(s).refs.get("b")).toBe(DOC.indexOf("[^b]"));
  });

  test("definitions are quiet lines with their number", () => {
    const { specs: sp } = specs(DOC, { cursor: 0 });
    const at = (t: string) => DOC.indexOf(t);
    expect(lines(sp, "cm-footnote-def")).toEqual([
      at("[^a]:"),
      at("    fortsätter"),
      at("[^b]:"),
      at("[^c]:"),
    ]);
    expect(notes(sp, "footnote-label")).toEqual([
      ["a", "2"],
      ["b", "1"],
      ["c", "c"],
    ]);
    expect(marks(DOC, sp, "cm-strong").map((m) => m.text)).toEqual(["a"]);
  });

  test("show their source on the line being edited", () => {
    const { specs: sp } = specs(DOC, { cursor: 2 });
    expect(notes(sp, "footnote")).toEqual([]);
    expect(marks(DOC, sp, "cm-footnote-source").map((m) => m.text)).toEqual([
      "[^b]",
      "[^A]",
      "[^b]",
    ]);
    const def = specs(DOC, { cursor: DOC.indexOf("Om b") });
    expect(marks(DOC, def.specs, "cm-marker").map((m) => m.text)).toContain("[^b]:");
  });

  test("a definition right after a paragraph line starts its own block", () => {
    const doc = "Text[^1]\n[^1]: Not";
    expect(notes(specs(doc, { cursor: 0 }).specs, "footnote-label")).toEqual([["1", "1"]]);
  });

  test("in a table cell, a reference is a mark that stands for its number", () => {
    const doc = "| a |\n| - |\n| x[^n] |\n\nslut\n\n[^n]: Not";
    const s = state(doc, doc.length);
    const [t] = tableSpecs(s, { activeLines: new Set() });
    const cell = t!.rows[0]![0]!;
    const [m, ...rest] = cell.specs;
    expect(rest).toEqual([]);
    expect([m!.type, cell.text.slice(m!.from, m!.to)]).toEqual(["mark", "[^n]"]);
    expect(m!.type === "mark" && m!.attrs).toEqual({ "data-footnote": "n", "data-text": "1" });
  });
});

describe("highlight", () => {
  test("==text== is highlighted and its markers hidden", () => {
    const doc = "Det här är ==viktigt **nu**== och klart.\n\nslut";
    const { specs: sp } = specs(doc);
    expect(marks(doc, sp, "cm-highlight").map((m) => m.text)).toEqual(["viktigt **nu**"]);
    expect(marks(doc, sp, "cm-strong").map((m) => m.text)).toEqual(["nu"]);
    expect(hidden(doc, sp).filter((h) => h === "==")).toHaveLength(2);
  });

  test("shows its markers on the line being edited", () => {
    const doc = "a ==b== c";
    const { specs: sp } = specs(doc, { cursor: 0 });
    expect(marks(doc, sp, "cm-highlight").map((m) => m.text)).toEqual(["b"]);
    expect(marks(doc, sp, "cm-marker").map((m) => m.text)).toEqual(["==", "=="]);
  });

  test("is not ===, a == comparison or an unclosed ==", () => {
    for (const doc of ["a ===b=== c", "if a == b == c", "x ==y", "a == b ==", "a=b"]) {
      expect(marks(doc, specs(doc, { cursor: 0 }).specs, "cm-highlight"), doc).toEqual([]);
    }
  });

  test("a setext heading's === underline stays a heading", () => {
    const doc = "Rubrik\n===\n\ntext";
    const { s } = specs(doc);
    expect(syntaxTree(s).topNode.firstChild!.name).toBe("SetextHeading1");
  });
});
