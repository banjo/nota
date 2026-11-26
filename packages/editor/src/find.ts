import {
  closeSearchPanel,
  findNext,
  findPrevious,
  getSearchQuery,
  openSearchPanel,
  replaceAll,
  replaceNext,
  search,
  SearchQuery,
  setSearchQuery,
} from "@codemirror/search";
import { EditorSelection, type EditorState, type Extension } from "@codemirror/state";
import { EditorView, keymap, type Panel, type ViewUpdate } from "@codemirror/view";

// Find (⌘F) and replace (⌥⌘F) in the open note: CodeMirror's search
// state and commands under a bar of Nota's own. It floats at the top of
// the editor, below the page's top bar, without pushing the text down.
// Loaded the first time it opens (see openFind in index.ts).

/** Counting stops here: past it the bar says "1000+". */
const MAX_COUNT = 1000;

/** Where the selection is among the query's matches, and how many there are. */
export function matchPosition(
  state: EditorState,
  query: SearchQuery,
): { index: number; total: number; more: boolean } {
  if (!query.valid) return { index: -1, total: 0, more: false };
  const sel = state.selection.main;
  const cursor = query.getCursor(state);
  let total = 0;
  let index = -1;
  for (let m = cursor.next(); !m.done; m = cursor.next()) {
    if (m.value.from === sel.from && m.value.to === sel.to) index = total;
    if (++total >= MAX_COUNT) return { index, total, more: !cursor.next().done };
  }
  return { index, total, more: false };
}

const ICONS = {
  up: "<path d='m18 15-6-6-6 6'/>",
  down: "<path d='m6 9 6 6 6-6'/>",
  close: "<path d='M18 6 6 18'/><path d='m6 6 12 12'/>",
  replace: "<path d='m9 18 6-6-6-6'/>",
};
const svg = (paths: string) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string> = {},
  html = "",
): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  if (html) e.innerHTML = html;
  return e;
}

const panels = new WeakMap<EditorView, FindPanel>();

class FindPanel implements Panel {
  dom: HTMLElement;
  top = true;
  private query: SearchQuery;
  private find: HTMLInputElement;
  private replace: HTMLInputElement;
  private count: HTMLElement;
  private replaceRow: HTMLElement;
  private expand: HTMLButtonElement;
  private toggles: Record<"caseSensitive" | "wholeWord" | "regexp", HTMLButtonElement>;
  private showReplace = false;

  constructor(private view: EditorView) {
    this.query = getSearchQuery(view.state);
    const input = (name: string, label: string) =>
      el("input", {
        type: "text",
        name,
        placeholder: label,
        "aria-label": label,
        autocomplete: "off",
        autocapitalize: "off",
        spellcheck: "false",
        enterkeyhint: name === "find" ? "search" : "done",
        class: "cm-nota-find-field",
      });
    const button = (name: string, label: string, html: string, run: () => void) => {
      const b = el("button", { type: "button", name, "aria-label": label, title: label }, html);
      b.addEventListener("click", run);
      return b;
    };
    const toggle = (key: "caseSensitive" | "wholeWord" | "regexp", label: string, text: string) => {
      const b = button(key, label, text, () => this.commit({ [key]: !this.query[key] }));
      b.className = "cm-nota-find-toggle";
      return b;
    };

    this.find = input("find", "Find");
    this.find.setAttribute("main-field", "true");
    this.replace = input("replace", "Replace");
    this.count = el("span", { class: "cm-nota-find-count", "aria-live": "polite" });
    this.toggles = {
      caseSensitive: toggle("caseSensitive", "Match case", "Aa"),
      wholeWord: toggle("wholeWord", "Whole word", "<u>ab</u>"),
      regexp: toggle("regexp", "Regular expression", ".*"),
    };
    this.expand = button("toggleReplace", "Replace", svg(ICONS.replace), () =>
      this.setReplace(!this.showReplace, true),
    );
    this.expand.className = "cm-nota-find-expand";

    const findRow = el("div", { class: "cm-nota-find-row" });
    const field = el("div", { class: "cm-nota-find-input" });
    field.append(this.find, this.count, ...Object.values(this.toggles));
    findRow.append(
      this.expand,
      field,
      button("prev", "Previous match (⇧↵)", svg(ICONS.up), () => findPrevious(view)),
      button("next", "Next match (↵)", svg(ICONS.down), () => findNext(view)),
      button("close", "Close (Esc)", svg(ICONS.close), () => closeSearchPanel(view)),
    );

    this.replaceRow = el("div", { class: "cm-nota-find-row cm-nota-find-replace" });
    const replaceField = el("div", { class: "cm-nota-find-input" });
    replaceField.append(this.replace);
    const one = button("replace", "Replace", "Replace", () => replaceNext(view));
    const all = button("replaceAll", "Replace all", "All", () => replaceAll(view));
    one.className = all.className = "cm-nota-find-text";
    this.replaceRow.append(replaceField, one, all);

    this.dom = el("div", { class: "cm-nota-find", role: "search" });
    this.dom.append(findRow, this.replaceRow);
    this.dom.addEventListener("keydown", (e) => this.keydown(e));
    for (const f of [this.find, this.replace])
      f.addEventListener("input", () =>
        this.commit({ search: this.find.value, replace: this.replace.value }),
      );
    this.sync();
    this.setReplace(false);
    panels.set(view, this);
  }

  mount() {
    this.find.select();
  }

  destroy() {
    panels.delete(this.view);
  }

  update(u: ViewUpdate) {
    const q = getSearchQuery(u.state);
    if (!q.eq(this.query)) {
      this.query = q;
      this.sync();
    } else if (u.docChanged || u.selectionSet) this.showCount();
    if (u.startState.readOnly !== u.state.readOnly) this.setReplace(this.showReplace);
  }

  /** Shows the replace row (not in a read-only mode); focus moves to it when asked. */
  setReplace(on: boolean, focus = false) {
    const can = !this.view.state.readOnly;
    this.showReplace = on;
    this.replaceRow.hidden = !(on && can);
    this.expand.hidden = !can;
    this.expand.setAttribute("aria-expanded", String(on && can));
    if (focus) (on && can ? this.replace : this.find).focus();
  }

  focusReplace() {
    this.setReplace(true);
    if (!this.replaceRow.hidden) this.replace.select();
  }

  private commit(change: Partial<ConstructorParameters<typeof SearchQuery>[0]>) {
    const q = this.query;
    const next = new SearchQuery({
      search: q.search,
      caseSensitive: q.caseSensitive,
      wholeWord: q.wholeWord,
      regexp: q.regexp,
      replace: q.replace,
      ...change,
    });
    if (next.eq(q)) return;
    this.query = next;
    this.view.dispatch({ effects: setSearchQuery.of(next) });
    this.sync();
  }

  private sync() {
    const q = this.query;
    if (this.find.value !== q.search) this.find.value = q.search;
    if (this.replace.value !== q.replace) this.replace.value = q.replace;
    for (const [k, b] of Object.entries(this.toggles))
      b.setAttribute("aria-pressed", String(q[k as keyof typeof this.toggles]));
    this.showCount();
  }

  private showCount() {
    const q = this.query;
    const invalid = q.search !== "" && !q.valid;
    this.find.setAttribute("aria-invalid", String(invalid));
    if (!q.search) {
      this.count.textContent = "";
      return;
    }
    if (invalid) {
      this.count.textContent = "Invalid";
      return;
    }
    const { index, total, more } = matchPosition(this.view.state, q);
    const of = `${total}${more ? "+" : ""}`;
    this.count.textContent =
      total === 0 ? "No results" : index >= 0 ? `${index + 1} of ${of}` : `${of} found`;
    this.dom.toggleAttribute("data-empty", total === 0);
  }

  private keydown(e: KeyboardEvent) {
    const mod = e.metaKey || e.ctrlKey;
    if (e.key === "Escape") {
      e.preventDefault();
      closeSearchPanel(this.view);
    } else if (e.key === "Enter" && e.target === this.find && !e.isComposing) {
      e.preventDefault();
      (e.shiftKey ? findPrevious : findNext)(this.view);
    } else if (e.key === "Enter" && e.target === this.replace && !e.isComposing) {
      e.preventDefault();
      (mod ? replaceAll : replaceNext)(this.view);
    } else if (mod && e.key.toLowerCase() === "g") {
      e.preventDefault();
      (e.shiftKey ? findPrevious : findNext)(this.view);
    } else if (mod && !e.altKey && e.key.toLowerCase() === "f") {
      e.preventDefault();
      this.find.select();
    } else if (mod && e.altKey && (e.code === "KeyF" || e.key.toLowerCase() === "f")) {
      e.preventDefault();
      this.focusReplace();
    }
  }
}

const v = (name: string, fallback: string) => `var(--${name}, ${fallback})`;

const findTheme = EditorView.theme({
  // The bar floats: a zero-height sticky strip, so the text doesn't move.
  ".cm-panels.cm-panels-top": {
    top: "var(--editor-sticky-top, 0px)",
    height: "0",
    overflow: "visible",
    backgroundColor: "transparent",
    border: "none",
    color: "inherit",
  },
  ".cm-nota-find": {
    position: "absolute",
    top: "4px",
    right: "0",
    display: "flex",
    flexDirection: "column",
    gap: "4px",
    width: "min(420px, 100%)",
    padding: "6px",
    borderRadius: "10px",
    background: v("surface", "#fff"),
    color: v("foreground", "#1b1b1d"),
    boxShadow: `0 0 0 1px ${v("hairline", "#e5e5e5")}, 0 8px 24px -8px rgb(0 0 0 / 0.25)`,
    fontFamily: "inherit",
    fontSize: "13px",
  },
  ".cm-nota-find-row": { display: "flex", alignItems: "center", gap: "2px" },
  ".cm-nota-find-row[hidden]": { display: "none" },
  ".cm-nota-find-input": {
    flex: "1",
    minWidth: "0",
    display: "flex",
    alignItems: "center",
    gap: "2px",
    height: "28px",
    padding: "0 3px 0 8px",
    borderRadius: "6px",
    background: v("background", "#fff"),
    boxShadow: `inset 0 0 0 1px ${v("hairline", "#e5e5e5")}`,
  },
  ".cm-nota-find-input:focus-within": {
    boxShadow: `inset 0 0 0 1px ${v("ink", "#1e7363")}, 0 0 0 3px ${v("ink-soft", "#ededed")}`,
  },
  ".cm-nota-find-field": {
    flex: "1",
    minWidth: "0",
    height: "100%",
    border: "none",
    outline: "none",
    background: "transparent",
    color: "inherit",
    font: "inherit",
    padding: "0",
  },
  ".cm-nota-find-field::placeholder": { color: v("faint", "#9ca0a8") },
  ".cm-nota-find-field[aria-invalid=true]": { color: v("danger", "#d4483b") },
  ".cm-nota-find-count": {
    flexShrink: "0",
    padding: "0 4px",
    color: v("faint", "#9ca0a8"),
    fontSize: "12px",
    fontVariantNumeric: "tabular-nums",
    whiteSpace: "nowrap",
  },
  ".cm-nota-find[data-empty] .cm-nota-find-count": { color: v("danger", "#d4483b") },
  ".cm-nota-find button": {
    flexShrink: "0",
    display: "grid",
    placeItems: "center",
    minWidth: "26px",
    height: "26px",
    padding: "0",
    border: "none",
    borderRadius: "5px",
    background: "transparent",
    color: v("muted-foreground", "#6b6f78"),
    font: "inherit",
    cursor: "default",
  },
  ".cm-nota-find button:hover": { background: v("hover", "#ececec"), color: "inherit" },
  ".cm-nota-find button:focus-visible": {
    outline: `2px solid ${v("ink", "#1e7363")}`,
    outlineOffset: "-2px",
  },
  ".cm-nota-find-toggle": {
    minWidth: "22px !important",
    height: "22px !important",
    fontSize: "12px",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace !important",
  },
  ".cm-nota-find-toggle[aria-pressed=true]": {
    background: `color-mix(in srgb, ${v("ink", "#1e7363")} 18%, transparent) !important`,
    color: `${v("ink-strong", "#175c4f")} !important`,
  },
  ".cm-nota-find-expand svg": { transition: "transform 150ms ease-out" },
  ".cm-nota-find-expand[aria-expanded=true] svg": { transform: "rotate(90deg)" },
  ".cm-nota-find-expand[hidden]": { display: "none" },
  ".cm-nota-find-replace": { paddingLeft: "28px" },
  ".cm-nota-find-text": { padding: "0 8px !important", fontSize: "12.5px" },
  // Matches in the warning colour (as ==highlights==), the current one stronger.
  ".cm-searchMatch": {
    backgroundColor: `color-mix(in srgb, ${v("warning", "#e2a93b")} 35%, transparent)`,
    borderRadius: "2px",
  },
  ".cm-searchMatch.cm-searchMatch-selected": {
    backgroundColor: `color-mix(in srgb, ${v("warning", "#e2a93b")} 75%, transparent)`,
    boxShadow: `0 0 0 1px ${v("warning", "#e2a93b")}`,
  },
  // Under a finger: 16px fields (iOS zooms into smaller ones) and 44px targets.
  "@media (pointer: coarse)": {
    ".cm-nota-find": { fontSize: "16px", width: "100%", top: "0", borderRadius: "12px" },
    // The field gets the whole first row; the buttons wrap below it.
    ".cm-nota-find-row": { flexWrap: "wrap", justifyContent: "flex-end" },
    ".cm-nota-find-input": { height: "40px", flexBasis: "100%", order: "-1" },
    ".cm-nota-find button": { minWidth: "40px", height: "40px" },
    ".cm-nota-find-toggle": { minWidth: "32px !important", height: "32px !important" },
    ".cm-nota-find-replace": { paddingLeft: "0" },
  },
});

/** The search state, the bar and its keys; added to an editor on first use. */
export const findExtension: Extension = [
  search({
    top: true,
    createPanel: (view) => new FindPanel(view),
    scrollToMatch: (range) => EditorView.scrollIntoView(range, { y: "center" }),
  }),
  keymap.of([
    {
      key: "Mod-g",
      run: findNext,
      shift: findPrevious,
      scope: "editor search-panel",
      preventDefault: true,
    },
    { key: "Escape", run: closeSearchPanel, scope: "editor search-panel" },
  ]),
  findTheme,
];

/**
 * A search to open the bar with, instead of the selection. `near` picks
 * the match to select: the first one after `before` + `text` (a line's
 * start up to the match, "…" where it was cut); without it, the first.
 */
export interface FindPreset {
  search: string;
  replace?: string;
  caseSensitive?: boolean;
  wholeWord?: boolean;
  regexp?: boolean;
  near?: { before: string; text: string };
}

/** The preset's query and the match it picks, if any. */
export function presetMatch(state: EditorState, preset: FindPreset) {
  const { near, ...spec } = preset;
  const q = new SearchQuery(spec);
  let from = 0;
  if (near) {
    const lead = near.before.replace(/^…/, "") + near.text;
    const at = state.doc.toString().indexOf(lead);
    if (at >= 0) from = at + lead.length - near.text.length;
  }
  const m = q.valid ? q.getCursor(state, from).next() : undefined;
  return { q, range: m && !m.done ? EditorSelection.range(m.value.from, m.value.to) : undefined };
}

/** Opens the bar (with the replace row when `replace`), with the selection as the query. */
export function open(view: EditorView, replace: boolean, preset?: FindPreset) {
  openSearchPanel(view);
  if (preset) {
    const { q, range } = presetMatch(view.state, preset);
    view.dispatch({
      effects: [
        setSearchQuery.of(q),
        ...(range ? [EditorView.scrollIntoView(range, { y: "center" })] : []),
      ],
      selection: range,
    });
  }
  const panel = panels.get(view);
  if (!panel) return;
  if (replace) panel.focusReplace();
  else panel.setReplace(false);
}
