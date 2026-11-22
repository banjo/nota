import type { Base, Note } from "@nota/client";
import { createEditor, type EditorView, replaceDoc } from "@nota/editor";
import { toast } from "@nota/ui/sonner";
import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useEffect, useLayoutEffect, useRef } from "react";
import { fail, openExternal, openWikilink } from "../actions.ts";
import { useClient } from "../context.ts";
import { filename, joinTitle, splitTitle, stem, type TitleParts } from "../lib/notes.ts";
import { keys, noteBase, noteWritten, setNoteBase } from "../queries.ts";
import { useUI } from "../state.ts";

// The note's body: a title field (the first line) and the live-preview
// editor (the rest). Edits save themselves after a short pause; a save
// is checked against the version the editor started from, and a
// conflict asks what to keep. Text is never lost: until a save lands
// it is also kept in localStorage.

const SAVE_DELAY = 600;
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
  const loadedTitle = useRef(splitTitle(note.body).title);

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
    if (view.current) replaceDoc(view.current, parts.current.rest);
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
      doc: parts.current.rest,
      placeholder: "Start writing…",
      onChange: (rest) => {
        parts.current = { ...parts.current, rest };
        schedule();
      },
      onWikilink: (target) => void openWikilink(client, qc, target, path),
      onTag: (tag) => useUI.getState().navigate({ kind: "list", list: { type: "tag", tag } }),
      onLink: openExternal,
    });
    view.current = v;
    const flush = () => void save();
    window.addEventListener("blur", flush);
    window.addEventListener("beforeunload", flush);
    const focusTitle = (e: Event) => {
      titleRef.current?.focus();
      if ((e as CustomEvent<{ select?: boolean }>).detail?.select) titleRef.current?.select();
    };
    document.addEventListener("nota:focus-title", focusTitle);
    return () => {
      window.removeEventListener("blur", flush);
      window.removeEventListener("beforeunload", flush);
      document.removeEventListener("nota:focus-title", focusTitle);
      if (timer.current) void save();
      v.destroy();
      view.current = null;
    };
    // The editor is keyed by path; later note updates arrive below.
  }, [path]);

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
        spellCheck={false}
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
        className="mb-[18px] block w-full resize-none overflow-hidden bg-transparent text-[34px] leading-[1.15] font-bold tracking-[-0.02em] text-foreground outline-none placeholder:text-faint pointer-coarse:text-[30px]"
      />
      {between}
      <div ref={host} className="nota-editor min-h-[40vh] text-editor" />
    </div>
  );
}
