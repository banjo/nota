import { EditorView } from "@codemirror/view";

// The editor's look, in the app's CSS variables (packages/ui/styles.css)
// with fallbacks, so it follows light and dark mode with the rest.

const v = (name: string, fallback: string) => `var(--${name}, ${fallback})`;
/** A theme's Markdown colour (--md-*), else the app token it refines. */
const md = (name: string, token: string, fallback: string) =>
  `var(--md-${name}, ${v(token, fallback)})`;
const mono = 'ui-monospace, "JetBrains Mono", SFMono-Regular, Menlo, monospace';
const check =
  "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' fill='none' stroke='black' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M3.5 8.5l3 3 6-7'/%3E%3C/svg%3E\")";

/** A Lucide icon as a mask (black strokes; the colour comes from the element). */
const icon = (paths: string) =>
  `url("data:image/svg+xml,${encodeURIComponent(
    `<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='black' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'>${paths}</svg>`,
  )}")`;
const CALLOUT_ICONS = {
  note: icon("<circle cx='12' cy='12' r='10'/><path d='M12 16v-4'/><path d='M12 8h.01'/>"),
  tip: icon(
    "<path d='M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5'/><path d='M9 18h6'/><path d='M10 22h4'/>",
  ),
  important: icon(
    "<path d='M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z'/><path d='M12 7v2'/><path d='M12 13h.01'/>",
  ),
  warning: icon(
    "<path d='m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3'/><path d='M12 9v4'/><path d='M12 17h.01'/>",
  ),
  caution: icon(
    "<path d='M12 16h.01'/><path d='M12 8v4'/><path d='M15.312 2a2 2 0 0 1 1.414.586l4.688 4.688A2 2 0 0 1 22 8.688v6.624a2 2 0 0 1-.586 1.414l-4.688 4.688a2 2 0 0 1-1.414.586H8.688a2 2 0 0 1-1.414-.586l-4.688-4.688A2 2 0 0 1 2 15.312V8.688a2 2 0 0 1 .586-1.414l4.688-4.688A2 2 0 0 1 8.688 2z'/>",
  ),
};
const calloutIcons = Object.fromEntries(
  Object.entries(CALLOUT_ICONS).map(([kind, url]) => [
    `.cm-callout-icon[data-callout="${kind}"]`,
    { maskImage: url, WebkitMaskImage: url },
  ]),
);

export const notaTheme = EditorView.theme({
  "&": {
    fontSize: "15px",
    color: v("text", "#2a2c31"),
    backgroundColor: "transparent",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": {
    fontFamily: "inherit",
    lineHeight: "1.65",
    overflow: "visible",
  },
  ".cm-content": {
    padding: "0 0 40vh",
    caretColor: v("ink", "#1e7363"),
  },
  ".cm-line": { padding: "0" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: v("ink", "#1e7363"), borderLeftWidth: "1.5px" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": {
    backgroundColor: `color-mix(in srgb, ${v("ink", "#1e7363")} 22%, transparent) !important`,
  },
  ".cm-placeholder": { color: v("faint", "#9ca0a8") },

  // Headings: sized, with room above. Title-like h1, quieter h2–h6.
  ".cm-h": {
    fontWeight: "650",
    color: md("heading", "foreground", "#1d1f24"),
    letterSpacing: "-0.01em",
  },
  ".cm-h1": {
    fontSize: "1.6em",
    lineHeight: "1.3",
    paddingTop: "0.6em !important",
    letterSpacing: "-0.02em",
    fontWeight: "700",
  },
  ".cm-h2": { fontSize: "1.15em", lineHeight: "1.4", paddingTop: "0.35em !important" },
  ".cm-h3": { fontSize: "1.05em", paddingTop: "0.3em !important" },
  ".cm-h4, .cm-h5, .cm-h6": {
    fontSize: "1em",
    paddingTop: "0.5em !important",
    color: md("heading", "muted-foreground", "#6b6f78"),
  },

  ".cm-strong": { fontWeight: "650", color: md("bold", "foreground", "#1d1f24") },
  ".cm-em": { fontStyle: "italic", color: "var(--md-italic, inherit)" },
  ".cm-strike": { textDecoration: "line-through", color: v("faint", "#9ca0a8") },
  ".cm-inline-code": {
    fontFamily: mono,
    fontSize: "0.86em",
    color: "var(--md-code, inherit)",
    backgroundColor: v("code", "#f1f1ee"),
    borderRadius: "4px",
    padding: "0.1em 0.35em",
  },
  ".cm-marker": { color: v("faint", "#9ca0a8") },

  ".cm-wikilink, .cm-link": {
    color: md("link", "ink", "#1e7363"),
    textDecoration: "underline",
    textDecorationColor: `color-mix(in srgb, ${md("link", "ink", "#1e7363")} 32%, transparent)`,
    textUnderlineOffset: "3px",
    cursor: "pointer",
  },
  ".cm-tag": {
    color: md("tag", "ink-strong", "#2f40b3"),
    backgroundColor: v("ink-soft", "#e6f3f0"),
    borderRadius: "5px",
    padding: "0.05em 0.3em",
    fontSize: "0.9em",
    cursor: "pointer",
  },

  // Footnotes: references are raised numbers in the link colour, and
  // definitions are quiet, smaller lines.
  ".cm-footnote-ref": {
    color: md("link", "ink", "#1e7363"),
    fontSize: "0.75em",
    lineHeight: "0",
    verticalAlign: "super",
    padding: "0 0.1em",
    cursor: "pointer",
  },
  ".cm-footnote-source": { color: v("faint", "#9ca0a8") },
  ".cm-footnote-def": {
    fontSize: "0.9em",
    color: v("muted-foreground", "#6b6f78"),
  },
  ".cm-footnote-label": {
    color: md("link", "ink", "#1e7363"),
    fontVariantNumeric: "tabular-nums",
    marginRight: "0.4em",
    cursor: "pointer",
  },
  ".cm-footnote-flash": {
    backgroundColor: v("ink-soft", "#e6f3f0"),
    transition: "background-color 0.3s",
  },

  ".cm-quote": {
    borderLeft: `3px solid ${v("hairline", "#e7e7e4")}`,
    paddingLeft: "14px !important",
    color: md("quote", "muted-foreground", "#6b6f78"),
  },
  // Callouts: a quote with "[!type]" on its first line. The colour is
  // --callout, per kind; tip and warning use the code colours for strings
  // and numbers, which every theme keeps readable.
  ".cm-callout": {
    borderLeft: "3px solid var(--callout)",
    backgroundColor: "color-mix(in srgb, var(--callout) 8%, transparent)",
    padding: "0 14px !important",
  },
  ".cm-callout-note": { "--callout": md("callout-note", "ink", "#1e7363") },
  ".cm-callout-tip": {
    "--callout": `var(--md-callout-tip, var(--md-syntax-string, ${v("success", "#24703a")}))`,
  },
  ".cm-callout-important": { "--callout": md("callout-important", "ink-strong", "#175c4f") },
  ".cm-callout-warning": {
    "--callout": `var(--md-callout-warning, var(--md-syntax-number, ${v("warning", "#a0540a")}))`,
  },
  ".cm-callout-caution": { "--callout": md("callout-caution", "danger", "#d4483b") },
  ".cm-callout-first": {
    paddingTop: "6px !important",
    borderTopRightRadius: "6px",
  },
  ".cm-callout-last": {
    paddingBottom: "6px !important",
    borderBottomRightRadius: "6px",
  },
  ".cm-callout-title": { fontWeight: "600", color: "var(--callout)" },
  ".cm-callout-title .cm-marker": { fontWeight: "400" },
  ".cm-callout-icon": {
    display: "inline-block",
    width: "16px",
    height: "16px",
    marginRight: "6px",
    verticalAlign: "-3px",
    backgroundColor: "currentColor",
    maskSize: "16px 16px",
    maskRepeat: "no-repeat",
    WebkitMaskSize: "16px 16px",
    WebkitMaskRepeat: "no-repeat",
  },
  ...calloutIcons,

  ".cm-hr": { position: "relative", height: "1.65em" },
  ".cm-hr::after": {
    content: '""',
    position: "absolute",
    left: "0",
    right: "0",
    top: "50%",
    borderTop: `1px solid ${v("hairline", "#e7e7e4")}`,
  },
  ".cm-hr-source": { color: v("faint", "#9ca0a8") },

  ".cm-codeblock": {
    fontFamily: mono,
    fontSize: "13.5px",
    lineHeight: "1.55",
    backgroundColor: v("code", "#f1f1ee"),
    padding: "0 14px !important",
  },
  ".cm-codeblock-first": {
    borderTopLeftRadius: "8px",
    borderTopRightRadius: "8px",
    paddingTop: "8px !important",
  },
  ".cm-codeblock-last": {
    borderBottomLeftRadius: "8px",
    borderBottomRightRadius: "8px",
    paddingBottom: "8px !important",
  },
  ".cm-fence": { color: v("faint", "#9ca0a8") },

  // Tables: rendered while the cursor is elsewhere, source (monospace, so
  // the pipes line up) while it is in them. Wide tables scroll sideways.
  ".cm-table-widget": {
    // Its width comes from the column, not the table, so a wide table
    // scrolls here instead of widening the editor.
    contain: "inline-size",
    overflowX: "auto",
    padding: "0.35em 0 0.6em",
    lineHeight: "1.5",
  },
  // The editor breaks words anywhere (lineWrapping); a table scrolls instead.
  ".cm-table-widget table": {
    overflowWrap: "normal",
    wordBreak: "normal",
    borderCollapse: "collapse",
    fontSize: "0.95em",
    fontVariantNumeric: "tabular-nums",
  },
  ".cm-table-widget th, .cm-table-widget td": {
    border: `1px solid ${md("table-border", "hairline", "#e7e7e4")}`,
    padding: "5px 12px",
    textAlign: "left",
    verticalAlign: "top",
    minWidth: "3em",
  },
  ".cm-table-widget th": {
    fontWeight: "650",
    color: v("foreground", "#1d1f24"),
    backgroundColor: md("table-header", "code", "#f1f1ee"),
  },
  ".cm-table-widget tbody tr:nth-child(even) td": {
    backgroundColor: `color-mix(in srgb, ${md("table-header", "code", "#f1f1ee")} 45%, transparent)`,
  },
  ".cm-table-widget td:hover": { cursor: "text" },
  "&.cm-reading .cm-table-widget td:hover": { cursor: "auto" },
  ".cm-table-source": {
    fontFamily: mono,
    fontSize: "13.5px",
  },

  // Lists: a hanging indent so wrapped lines align with the text.
  ".cm-list": { paddingLeft: "var(--hang) !important", textIndent: "calc(-1 * var(--hang))" },
  ".cm-list-mark": { color: v("muted-foreground", "#6b6f78"), fontVariantNumeric: "tabular-nums" },
  ".cm-bullet": {
    display: "inline-block",
    width: "1ch",
    color: v("faint", "#9ca0a8"),
    textIndent: "0",
    marginRight: "1ch",
  },
  ".cm-task-checkbox": {
    display: "inline-block",
    boxSizing: "border-box",
    width: "15px",
    height: "15px",
    marginRight: "8px",
    verticalAlign: "-2px",
    borderRadius: "4px",
    border: `1.5px solid ${v("faint", "#9ca0a8")}`,
    backgroundColor: v("background", "#fff"),
    cursor: "pointer",
    textIndent: "0",
    transition: "background-color 120ms ease-out, border-color 120ms ease-out",
  },
  // The check is a mask painted in --ink-foreground, so it stays visible
  // on every theme's ink (near-white in Graphite dark).
  '.cm-task-checkbox[data-checked="true"]': {
    position: "relative",
    backgroundColor: v("ink", "#1e7363"),
    borderColor: v("ink", "#1e7363"),
  },
  '.cm-task-checkbox[data-checked="true"]::after': {
    content: '""',
    position: "absolute",
    inset: "0",
    backgroundColor: v("ink-foreground", "#ffffff"),
    maskImage: check,
    maskSize: "11px 11px",
    maskPosition: "center",
    maskRepeat: "no-repeat",
    WebkitMaskImage: check,
    WebkitMaskSize: "11px 11px",
    WebkitMaskPosition: "center",
    WebkitMaskRepeat: "no-repeat",
  },
  ".cm-task-done": { color: v("faint", "#9ca0a8"), textDecoration: "line-through" },
});
