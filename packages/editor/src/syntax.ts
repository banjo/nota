import type { Element, MarkdownConfig } from "@lezer/markdown";
import { tags as t } from "@lezer/highlight";

// Nota's dialect on top of CommonMark + GFM (decision 4): Obsidian's
// [[wikilinks]], ![[embeds]] and #tags (nested #a/b), and footnotes
// ([^id] and [^id]: text) as on GitHub. The Go index (internal/markdown)
// reads the same links and tags.

const OPEN = 91; // [
const CLOSE = 93; // ]
const BANG = 33; // !
const HASH = 35; // #
const CARET = 94; // ^

/** "[^id]:" at the start of a footnote definition. */
const FOOTNOTE_DEF = /^\[\^([^\]\s]+)\]:/;
/** A line that continues a footnote definition: indented, not blank. */
const FOOTNOTE_MORE = /^(?: {2,}|\t)\S/;

/** A parsed [[target#heading|alias]]. */
export interface Wikilink {
  target: string;
  heading?: string;
  alias?: string;
  embed: boolean;
}

/** Splits the inside of a wikilink ("Plan#Mål|planen"). */
export function parseWikilink(inner: string, embed = false): Wikilink {
  const bar = inner.indexOf("|");
  // "\|" is the alias bar inside a table, where "|" splits cells.
  const ref = bar < 0 ? inner : inner.slice(0, bar).replace(/\\$/, "");
  const alias = bar < 0 ? undefined : inner.slice(bar + 1).trim() || undefined;
  const hash = ref.indexOf("#");
  const target = (hash < 0 ? ref : ref.slice(0, hash)).trim();
  const heading = hash < 0 ? undefined : ref.slice(hash + 1).trim() || undefined;
  return { target, heading, alias, embed };
}

const tagChar = /[\p{L}\p{N}_\-/]/u;
const tagStart = /[\p{L}_]/u;

/** The length of a #tag at text[i] ("#" included), or 0. */
export function tagLength(text: string, i: number): number {
  if (text.charCodeAt(i) !== HASH) return 0;
  const before = i > 0 ? text[i - 1]! : " ";
  if (!/\s|[([{,;]/.test(before)) return 0;
  let j = i + 1;
  let letter = false;
  while (j < text.length && tagChar.test(text[j]!)) {
    if (tagStart.test(text[j]!)) letter = true;
    j++;
  }
  // Obsidian needs one non-digit; a trailing slash is not part of it.
  while (j > i + 1 && text[j - 1] === "/") j--;
  return letter && j > i + 1 ? j - i : 0;
}

/** Every #tag in a line of text, without the "#". */
export function findTags(text: string): string[] {
  const out: string[] = [];
  for (let i = 0; i < text.length; i++) {
    const n = tagLength(text, i);
    if (n) {
      out.push(text.slice(i + 1, i + n));
      i += n - 1;
    }
  }
  return out;
}

/**
 * Lezer markdown extension: WikiLink (with WikiLinkMark), Tag, FootnoteRef,
 * and FootnoteDefinition (a FootnoteLabel, then the text's inline nodes).
 */
export const notaSyntax: MarkdownConfig = {
  defineNodes: [
    { name: "WikiLink", style: t.link },
    { name: "WikiLinkMark", style: t.processingInstruction },
    { name: "Tag", style: t.labelName },
    { name: "FootnoteRef", style: t.labelName },
    { name: "FootnoteDefinition", block: true },
    { name: "FootnoteLabel", style: t.labelName },
  ],
  parseBlock: [
    {
      name: "FootnoteDefinition",
      before: "LinkReference",
      parse(cx, line) {
        const m = FOOTNOTE_DEF.exec(line.text.slice(line.pos));
        if (!m || line.indent - line.baseIndent >= 4) return false;
        const from = cx.lineStart + line.pos;
        const children: Element[] = [cx.elt("FootnoteLabel", from, from + m[0].length)];
        const text = (start: number) =>
          children.push(...cx.parser.parseInline(line.text.slice(start), cx.lineStart + start));
        text(line.pos + m[0].length);
        let to = cx.lineStart + line.text.length;
        // Indented lines below continue it (top-level only: inside a quote
        // or list the next line carries that container's markers).
        while (line.baseIndent === 0 && FOOTNOTE_MORE.test(cx.peekLine())) {
          cx.nextLine();
          text(line.pos);
          to = cx.lineStart + line.text.length;
        }
        cx.nextLine();
        cx.addElement(cx.elt("FootnoteDefinition", from, to, children));
        return true;
      },
      // A definition right after a paragraph line starts its own block.
      endLeaf: (_, line) => FOOTNOTE_DEF.test(line.text.slice(line.pos)),
    },
  ],
  parseInline: [
    {
      name: "FootnoteRef",
      before: "Link",
      parse(cx, next, pos) {
        if (next !== OPEN || cx.char(pos + 1) !== CARET) return -1;
        for (let i = pos + 2; i < cx.end; i++) {
          const c = cx.char(i);
          if (c === CLOSE)
            return i > pos + 2 ? cx.addElement(cx.elt("FootnoteRef", pos, i + 1)) : -1;
          if (c === OPEN || c === 32 || c === 9 || c === 10) return -1;
        }
        return -1;
      },
    },
    {
      name: "WikiLink",
      before: "Link",
      parse(cx, next, pos) {
        const embed = next === BANG;
        const start = embed ? pos + 1 : pos;
        if (cx.char(start) !== OPEN || cx.char(start + 1) !== OPEN) return -1;
        for (let i = start + 2; i < cx.end - 1; i++) {
          const c = cx.char(i);
          if (c === 10 || c === OPEN) return -1;
          if (c === CLOSE) {
            if (cx.char(i + 1) !== CLOSE || i === start + 2) return -1;
            return cx.addElement(
              cx.elt("WikiLink", pos, i + 2, [
                cx.elt("WikiLinkMark", pos, start + 2),
                cx.elt("WikiLinkMark", i, i + 2),
              ]),
            );
          }
        }
        return -1;
      },
    },
    {
      name: "Tag",
      parse(cx, next, pos) {
        if (next !== HASH) return -1;
        const before = pos > cx.offset ? cx.slice(pos - 1, pos) : " ";
        const text = before + cx.slice(pos, Math.min(cx.end, pos + 200));
        const n = tagLength(text, 1);
        return n ? cx.addElement(cx.elt("Tag", pos, pos + n)) : -1;
      },
    },
  ],
};
