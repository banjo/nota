import type { Base, Note } from "@nota/client";
import {
  createEditor,
  openFind,
  type EditorView,
  refreshMedia,
  replaceDoc,
  revealAnchor,
  setEditorMode,
} from "@nota/editor";
import { toast } from "@nota/ui/sonner";
import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useEffect, useLayoutEffect, useRef } from "react";
import { fail, reportLinks } from "../actions.ts";
import { useClient } from "../context.ts";
import { noteHandlers } from "../editorHandlers.ts";
import { listenFind } from "../lib/find.ts";
import { label } from "../lib/shortcuts.ts";
import { filename, joinTitle, splitTitle, stem, type TitleParts } from "../lib/notes.ts";
import type { NoteMode } from "../lib/noteMode.ts";
import { keys, noteBase, noteWritten, setNoteBase } from "../queries.ts";
import { useNoteMode, useUI } from "../state.ts";

// The note's body: a title field (the first line) and the live-preview
// editor (the rest). Edits save themselves after a short pause; a save
// is checked against the version the editor started from, and a
// conflict asks what to keep. Text is never lost: until a save lands
// it is also kept in localStorage.

const SAVE_DELAY = 600;
const HINT_KEY = "nota:reading-hint";

/** What the editor holds: the whole body in Source mode, else the body below the title. */
const editorText = (mode: NoteMode, p: TitleParts) => (mode === "source" ? joinTitle(p) : p.rest);
const draftKey = (path: string) => `nota:draft:${path}`;

interface Draft {
  body: string;
  base: Base;
}

function readDraft(path: string): Draft | null {
  try {
    const raw = localStorage.getItem(draftKey(path));
    return raw ? (JSON.parse(raw) as Draft) : null;
  } catch {
    return null;
  }
}

const conflictTitle = (title: string) =>
  `${title || "Untitled"} (conflict ${new Date().toISOString().slice(0, 10)})`;

/** `between` renders between the title and the body (the properties panel). */
export function NoteEditor({ note, between }: { note: Note; between?: ReactNode }) {
  const client = useClient();
  const qc = useQueryClient();
  const host = useRef<HTMLDivElement>(null);
  const titleRef = useRef<HTMLTextAreaElement>(null);
  const view = useRef<EditorView | null>(null);
  const parts = useRef<TitleParts>(splitTitle(note.body));
  const saved = useRef(note.body); // the body as last written or loaded
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  // One write at a time: two saves started from the same base would make
  // the second a version conflict with our own first write.
  const inFlight = useRef(false);
  const queued = useRef(false);
  const inConflict = useRef(false);
  const path = note.path;
  const mode = useNoteMode();
  const modeRef = useRef(mode);
  const loadedTitle = useRef(splitTitle(note.body).title);
  // Where a click in Reading mode asked to start editing.
  const editAt = useRef<number | null>(null);

  const body = () => joinTitle(parts.current);

  async function save(force?: Base) {
    clearTimeout(timer.current);
    timer.current = undefined;
    if (inFlight.current) {
      queued.current = true;
      return;
    }
    const text = body();
    if (text === saved.current && !force) return;
    if (inConflict.current && !force) return;
    inFlight.current = true;
    try {
      const r = await client.save(path, text, force ?? noteBase(path));
      if (r.conflict) {
        conflict(text, r.current);
        return;
      }
      saved.current = r.note.body;
      noteWritten(qc, r.note);
      localStorage.removeItem(draftKey(path));
      void qc.invalidateQueries({ queryKey: keys.tree });
    } catch (e) {
      fail("Couldn't save the note; your text is kept on this device", e);
    } finally {
      inFlight.current = false;
      if (queued.current) {
        queued.current = false;
        void save();
      }
    }
  }

  function schedule() {
    const text = body();
    const base = noteBase(path);
    if (base)
      localStorage.setItem(draftKey(path), JSON.stringify({ body: text, base } satisfies Draft));
    clearTimeout(timer.current);
    timer.current = setTimeout(() => void save(), SAVE_DELAY);
  }

  function load(text: string) {
    parts.current = splitTitle(text);
    if (titleRef.current) titleRef.current.value = parts.current.title;
    if (view.current) replaceDoc(view.current, editorText(modeRef.current, parts.current));
    autosize();
  }

  function conflict(mine: string, current: Note | null) {
    inConflict.current = true;
    toast.warning("This note changed somewhere else", {
      id: `conflict:${path}`,
      description: "Keep your version, or use theirs (yours is kept as a copy).",
      duration: Infinity,
      action: {
        label: "Keep mine",
        onClick: () => {
          inConflict.current = false;
          void save(current ? { version: current.version, hash: current.hash } : undefined);
        },
      },
      cancel: {
        label: "Use theirs",
        onClick: () => {
          inConflict.current = false;
          void client
            .create({
              title: conflictTitle(parts.current.title),
              body: splitTitle(mine).rest,
              folder: note.folder || "/",
            })
            .then(() => void qc.invalidateQueries({ queryKey: keys.tree }))
            .catch((e: unknown) => fail("Couldn't keep your version as a copy", e));
          localStorage.removeItem(draftKey(path));
          if (current) {
            setNoteBase(current);
            saved.current = current.body;
            load(current.body);
            qc.setQueryData(keys.note(path), current);
          }
        },
      },
    });
  }

  // A file named after its title follows the title (on leaving the field).
  async function renameToTitle() {
    const t = parts.current.title.trim();
    const want = filename(t);
    const cur = stem(path);
    if (!t || want === cur) return;
    if (cur !== filename(loadedTitle.current) && !/^Untitled( \d+)?$/.test(cur)) return;
    const to = (note.folder ? `${note.folder}/` : "") + `${want}.md`;
    try {
      const r = await client.rename(path, to);
      useUI.getState().moved(r.from, r.path);
      void qc.invalidateQueries({ queryKey: keys.tree });
      if (r.links?.count) {
        // Other notes changed on disk: their cached copies are stale.
        void qc.invalidateQueries({ queryKey: ["note"] });
        reportLinks(r.links);
      }
    } catch {
      // Taken: keep the file name.
    }
  }

  function autosize() {
    const t = titleRef.current;
    if (!t) return;
    t.style.height = "0px";
    t.style.height = `${t.scrollHeight}px`;
  }

  // Mount the editor once per note.
  useLayoutEffect(() => {
    setNoteBase(note);
    // A draft that never reached the disk (crash, offline) wins over the file it was based on.
    const draft = readDraft(path);
    let initial = note.body;
    if (draft && draft.body !== note.body) {
      if (draft.base.hash === note.hash) {
        initial = draft.body;
        setTimeout(() => void save(), 0);
      } else {
        setTimeout(() => conflict(draft.body, note), 0);
      }
    }
    parts.current = splitTitle(initial);
    saved.current = note.body;
    if (titleRef.current) titleRef.current.value = parts.current.title;
    autosize();
    const v = createEditor(host.current!, {
      doc: editorText(modeRef.current, parts.current),
      mode: modeRef.current,
      placeholder: "Start writing…",
      onChange: (text) => {
        if (modeRef.current === "source") {
          // Source mode edits the title line too.
          parts.current = splitTitle(text);
          if (titleRef.current) titleRef.current.value = parts.current.title;
        } else {
          parts.current = { ...parts.current, rest: text };
        }
        schedule();
      },
      ...noteHandlers(client, qc, path),
    });
    view.current = v;
    const flush = () => void save();
    window.addEventListener("blur", flush);
    window.addEventListener("beforeunload", flush);
    const focusTitle = (e: Event) => {
      if (modeRef.current === "source") {
        v.focus();
        return;
      }
      titleRef.current?.focus();
      if ((e as CustomEvent<{ select?: boolean }>).detail?.select) titleRef.current?.select();
    };
    document.addEventListener("nota:focus-title", focusTitle);
    const offFind = listenFind(v);
    return () => {
      offFind();
      window.removeEventListener("blur", flush);
      window.removeEventListener("beforeunload", flush);
      document.removeEventListener("nota:focus-title", focusTitle);
      if (timer.current) void save();
      v.destroy();
      view.current = null;
    };
    // The editor is keyed by path; later note updates arrive below.
  }, [path]);

  // Images or embedded notes changed elsewhere (sync, another program):
  // they load again. The web's events don't say what changed.
  useEffect(
    () =>
      client.subscribe(({ paths }) => {
        const v = view.current;
        if (!v) return;
        refreshMedia(v, {
          files: paths.some((p) => /\.(?:png|jpe?g|gif|webp|avif|svg|bmp)$/i.test(p)),
          notes: paths.some((p) => /\.md$/i.test(p) && p !== path),
        });
      }),
    [client, path],
  );

  // A link asked for a heading or block of this note: scroll there once
  // the editor has it, then forget it (a reload starts at the top).
  const anchor = useUI((s) =>
    s.route.kind === "note" && s.route.path === path ? s.route.anchor : undefined,
  );
  useEffect(() => {
    const v = view.current;
    if (!v || !anchor) return;
    const id = requestAnimationFrame(() => {
      if (!revealAnchor(v, anchor)) toast(`No heading or block “${anchor}” in this note`);
      const r = useUI.getState().route;
      if (r.kind === "note" && r.path === path)
        useUI.setState({ route: { ...r, anchor: undefined } });
    });
    return () => cancelAnimationFrame(id);
  }, [anchor, path]);

  // Find in notes opened this note on a match: show it in the find bar.
  const find = useUI((s) =>
    s.route.kind === "note" && s.route.path === path ? s.route.find : undefined,
  );
  useEffect(() => {
    const v = view.current;
    if (!v || !find) return;
    const id = requestAnimationFrame(() => {
      void openFind(v, false, find);
      const r = useUI.getState().route;
      if (r.kind === "note" && r.path === path)
        useUI.setState({ route: { ...r, find: undefined } });
    });
    return () => cancelAnimationFrame(id);
  }, [find, path]);

  // The mode changed (⌘E, ⌘K, the top bar): same editor, other extensions.
  // Source holds the title line too, so the text swaps when entering or
  // leaving it.
  useEffect(() => {
    const v = view.current;
    const prev = modeRef.current;
    modeRef.current = mode;
    if (!v) return;
    if (prev !== mode) {
      if (prev === "source") {
        parts.current = splitTitle(v.state.doc.toString());
        if (titleRef.current) titleRef.current.value = parts.current.title;
        autosize();
      }
      replaceDoc(v, editorText(mode, parts.current));
      setEditorMode(v, mode);
      if (mode === "reading") v.contentDOM.blur();
    }
    const at = editAt.current;
    editAt.current = null;
    if (at !== null && mode === "live") {
      v.focus();
      v.dispatch({ selection: { anchor: Math.min(at, v.state.doc.length) }, scrollIntoView: true });
    }
  }, [mode]);

  // A click in Reading mode starts editing there, unless it opened a
  // link, ticked a task or selected text.
  useEffect(() => {
    const v = view.current;
    const el = host.current;
    if (mode !== "reading" || !v || !el) return;
    const onClick = (e: MouseEvent) => {
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      const t = e.target as HTMLElement | null;
      if (
        t?.closest(
          ".cm-task-checkbox, [data-wikilink], [data-tag], [data-href], [data-footnote], [data-footnote-def], .cm-embed, a",
        )
      )
        return;
      if (!(window.getSelection()?.isCollapsed ?? true)) return;
      editAt.current = v.posAtCoords({ x: e.clientX, y: e.clientY }, false);
      useUI.getState().setNoteMode("live");
    };
    el.addEventListener("click", onClick);
    return () => el.removeEventListener("click", onClick);
  }, [mode]);

  // Reading mode ignores typing; say why once.
  useEffect(() => {
    if (mode !== "reading") return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key.length !== 1 || e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t?.closest("input, textarea, [contenteditable=true], [role=dialog]")) return;
      try {
        if (localStorage.getItem(HINT_KEY)) return;
        localStorage.setItem(HINT_KEY, "1");
      } catch {
        // No storage: the hint shows each time, which is fine.
      }
      toast(`This note is in reading mode. Press ${label("mod+e")} to edit.`);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [mode]);

  // The note changed outside the editor (another app, sync, a property
  // edit). Without unsaved edits, show it; with some, the next save
  // finds the conflict.
  useEffect(() => {
    const base = noteBase(path);
    if (base?.hash === note.hash) return;
    if (timer.current || inConflict.current) return;
    setNoteBase(note);
    if (note.body !== saved.current) {
      saved.current = note.body;
      load(note.body);
    }
  }, [note, path]);

  return (
    <div>
      <textarea
        ref={titleRef}
        rows={1}
        aria-label="Title"
        placeholder="Untitled"
        data-text-size="own"
        spellCheck={false}
        readOnly={mode === "reading"}
        onClick={(e) => {
          if (mode !== "reading") return;
          if (e.currentTarget.selectionStart !== e.currentTarget.selectionEnd) return;
          useUI.getState().setNoteMode("live");
        }}
        onChange={(e) => {
          parts.current = { ...parts.current, title: e.target.value.replace(/\n/g, " ") };
          if (
            !parts.current.prefix &&
            !parts.current.newline &&
            parts.current.rest === "" &&
            note.body === ""
          )
            parts.current.prefix = "# ";
          autosize();
          schedule();
        }}
        onKeyDown={(e) => {
          if (
            e.key === "Enter" ||
            (e.key === "ArrowDown" &&
              e.currentTarget.selectionStart === e.currentTarget.value.length)
          ) {
            e.preventDefault();
            view.current?.focus();
            view.current?.dispatch({ selection: { anchor: 0 } });
          }
        }}
        onBlur={(e) => {
          // Moving into the body is still the same note: renaming here
          // would remount the editor and drop the first keystrokes.
          if (e.currentTarget.closest("article")?.contains(e.relatedTarget as Node | null)) return;
          void save().then(renameToTitle);
        }}
        className={cn(
          "mb-[18px] w-full resize-none overflow-hidden bg-transparent text-[34px] leading-[1.15] font-bold tracking-[-0.02em] text-foreground outline-none placeholder:text-faint pointer-coarse:text-[30px]",
          mode === "source" ? "hidden" : "block",
        )}
      />
      {mode === "reading" ? null : between}
      <div ref={host} className="nota-editor min-h-[40vh] text-editor" />
    </div>
  );
}
