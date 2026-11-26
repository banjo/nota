import type { ReplaceMatch, ReplaceNote, ReplacePick, ReplaceQuery } from "@nota/client";
import { Button } from "@nota/ui/button";
import { Input } from "@nota/ui/input";
import { cn } from "@nota/ui/utils";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, TextSearch } from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { create } from "zustand";
import { replaceInNotes } from "../actions.ts";
import { useClient } from "../context.ts";
import { useDebounced } from "../lib/debounce.ts";
import { folderLabel, folderOf, stem } from "../lib/notes.ts";
import { label } from "../lib/shortcuts.ts";
import { keys } from "../queries.ts";
import { useUI } from "../state.ts";
import { EmptyState } from "./EmptyState.tsx";
import { TopBar } from "./TopBar.tsx";

// Find and replace across notes (⌘⇧F): every match, note by note, with
// what it becomes. Untick the ones to keep, then replace; Undo is in the
// toast. Only note text changes: properties, link targets and (unless
// asked) code are left alone, as with `nota replace`.

type Form = Required<Pick<ReplaceQuery, "find" | "replace" | "regex" | "case" | "word" | "code">>;

/** The form outlives the page, so a match opened in its note can be come back to. */
const useForm = create<Form & { set: (p: Partial<Form>) => void }>((set) => ({
  find: "",
  replace: "",
  regex: false,
  case: false,
  word: false,
  code: false,
  set,
}));

const key = (n: ReplaceNote, i: number) => `${n.path}\0${n.hash}\0${i}`;

export function ReplaceView() {
  const client = useClient();
  const qc = useQueryClient();
  const navigate = useUI((s) => s.navigate);
  const { set, ...form } = useForm();
  // Debounced as a string: a new object each render would never settle.
  const formKey = JSON.stringify(form);
  const queryKey = useDebounced(formKey, 200);
  const query = useMemo(() => JSON.parse(queryKey) as Form, [queryKey]);
  const results = useQuery({
    queryKey: [...keys.replace, query],
    queryFn: () => client.findReplace!(query),
    enabled: query.find !== "" && !!client.findReplace,
    placeholderData: keepPreviousData,
    retry: false,
  });
  const notes = query.find ? (results.data ?? []) : [];
  // Which matches are ticked: a new search ticks them all; what is left
  // after a replace (the ones kept, and any new) starts unticked.
  const [ticks, setTicks] = useState<{ all: boolean; set: ReadonlyMap<string, boolean> }>({
    all: true,
    set: new Map(),
  });
  useEffect(
    () => setTicks({ all: true, set: new Map() }),
    [query.find, query.regex, query.case, query.word, query.code],
  );
  const isOn = (k: string) => ticks.set.get(k) ?? ticks.all;
  const [busy, setBusy] = useState(false);
  const findRef = useRef<HTMLInputElement>(null);
  useEffect(() => findRef.current?.select(), []);

  const total = notes.reduce((n, x) => n + x.matches.length, 0);
  const picked = notes.reduce((n, x) => n + x.matches.filter((_, i) => isOn(key(x, i))).length, 0);
  const toggle = (ks: string[], on: boolean) =>
    setTicks((t) => {
      const set = new Map(t.set);
      for (const k of ks) set.set(k, on);
      return { ...t, set };
    });

  async function run() {
    if (!picked || busy) return;
    const picks: ReplacePick[] = [];
    for (const n of notes) {
      const on = n.matches.flatMap((_, i) => (isOn(key(n, i)) ? [i] : []));
      if (on.length)
        picks.push({
          path: n.path,
          hash: n.hash,
          matches: on.length === n.matches.length ? undefined : on,
        });
    }
    setBusy(true);
    const r = await replaceInNotes(client, qc, query, picks);
    if (r) setTicks({ all: false, set: new Map() });
    setBusy(false);
  }

  const open = (n: ReplaceNote, m: ReplaceMatch) =>
    navigate({
      kind: "note",
      path: n.path,
      find: {
        search: query.find,
        replace: query.replace,
        caseSensitive: query.case,
        wholeWord: query.word,
        regexp: query.regex,
        near: { before: m.before, text: m.text },
      },
    });

  const invalid = results.isError && query.find === form.find;
  const searching = form.find !== "" && (results.isFetching || queryKey !== formKey);
  const summary =
    form.find === ""
      ? ""
      : invalid
        ? ""
        : total === 0
          ? results.isPending || searching
            ? ""
            : "No matches"
          : `${picked === total ? "" : `${picked} of `}${total} ${total === 1 ? "match" : "matches"} in ${notes.length} ${notes.length === 1 ? "note" : "notes"}`;

  return (
    <div className="flex h-full flex-col">
      <TopBar crumbs={[{ label: "Find in notes" }]} />
      <form
        className="mx-auto flex w-full max-w-[820px] shrink-0 flex-col gap-2 px-4 pt-2 pb-3 max-[720px]:px-3"
        onSubmit={(e) => {
          e.preventDefault();
          void run();
        }}
      >
        <div className="relative">
          <Input
            ref={findRef}
            aria-label="Find"
            placeholder="Find"
            value={form.find}
            onChange={(e) => set({ find: e.target.value })}
            aria-invalid={invalid || undefined}
            className="pr-[100px] pointer-coarse:pr-[124px]"
            spellCheck={false}
            autoComplete="off"
          />
          <div className="absolute inset-y-0 right-1 flex items-center gap-0.5">
            <Toggle on={form.case} label="Match case" onClick={() => set({ case: !form.case })}>
              Aa
            </Toggle>
            <Toggle on={form.word} label="Whole word" onClick={() => set({ word: !form.word })}>
              <u>ab</u>
            </Toggle>
            <Toggle
              on={form.regex}
              label="Regular expression"
              onClick={() => set({ regex: !form.regex })}
            >
              .*
            </Toggle>
          </div>
        </div>
        <Input
          aria-label="Replace with"
          placeholder="Replace with"
          value={form.replace}
          onChange={(e) => set({ replace: e.target.value })}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              void run();
            }
          }}
          spellCheck={false}
          autoComplete="off"
        />
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <label className="flex cursor-pointer items-center gap-2 text-ui text-text-soft pointer-coarse:min-h-11">
            <input
              type="checkbox"
              checked={form.code}
              onChange={(e) => set({ code: e.target.checked })}
              className="size-4 accent-[var(--ink)]"
            />
            Include code
          </label>
          <p
            role="status"
            className={cn(
              "min-w-0 flex-1 truncate text-small",
              invalid ? "text-danger" : "text-muted-foreground",
            )}
          >
            {invalid ? message(results.error) : summary}
          </p>
          <Button type="submit" disabled={!picked || busy || searching}>
            {busy ? <LoaderCircle className="animate-spin" /> : null}
            {picked ? `Replace ${picked}` : "Replace"}
          </Button>
        </div>
      </form>
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain border-t border-hairline">
        {form.find === "" ? (
          <EmptyState
            icon={TextSearch}
            title="Find in notes"
            body={`Every match, note by note. Properties and link targets are never changed. ${label("mod+enter")} in Replace with replaces.`}
          />
        ) : (
          <ul
            aria-label="Matches"
            className={cn(
              "mx-auto max-w-[820px] px-2 py-2 transition-opacity duration-150 max-[720px]:px-1",
              searching && "opacity-60",
            )}
          >
            {notes.map((n) => (
              <NoteMatches
                key={n.path}
                note={n}
                isOn={isOn}
                toggle={toggle}
                open={(m) => open(n, m)}
                showWith={form.replace !== "" || query.replace !== ""}
              />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

const message = (e: unknown) =>
  (e instanceof Error ? e.message : String(e)).replace(
    /^invalid regular expression: /i,
    "Invalid regular expression: ",
  );

function Toggle({
  on,
  label,
  onClick,
  children,
}: {
  on: boolean;
  label: string;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={on}
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        "grid h-6 min-w-7 place-items-center rounded-[5px] px-1 font-mono text-[12px] outline-none focus-visible:shadow-[0_0_0_2px_var(--ink)] pointer-coarse:h-9 pointer-coarse:min-w-9",
        on
          ? "bg-ink/18 text-ink-strong"
          : "text-muted-foreground hover:bg-hover hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

function NoteMatches({
  note,
  isOn,
  toggle,
  open,
  showWith,
}: {
  note: ReplaceNote;
  isOn: (key: string) => boolean;
  toggle: (keys: string[], on: boolean) => void;
  open: (m: ReplaceMatch) => void;
  showWith: boolean;
}) {
  const all = note.matches.map((_, i) => key(note, i));
  const on = all.filter(isOn).length;
  const folder = folderOf(note.path);
  return (
    <li className="mb-2">
      <label className="flex cursor-pointer items-center gap-2.5 rounded-sm px-2 py-1.5 hover:bg-hover pointer-coarse:py-2.5">
        <input
          type="checkbox"
          checked={on === all.length}
          ref={(el) => {
            if (el) el.indeterminate = on > 0 && on < all.length;
          }}
          onChange={(e) => toggle(all, e.target.checked)}
          aria-label={`Replace in ${stem(note.path)}`}
          className="size-4 shrink-0 accent-[var(--ink)]"
        />
        <span className="min-w-0 truncate text-ui font-medium text-foreground">
          {note.title || stem(note.path)}
        </span>
        {folder ? (
          <span className="min-w-0 shrink-[2] truncate text-small text-faint">
            {folderLabel(folder)}
          </span>
        ) : null}
        <span className="ml-auto shrink-0 text-small text-faint tabular-nums">
          {note.matches.length}
        </span>
      </label>
      <ul>
        {note.matches.map((m, i) => {
          const k = key(note, i);
          const checked = isOn(k);
          return (
            <li key={k} className="flex items-start gap-2.5 rounded-sm pr-2 pl-8 hover:bg-hover">
              <input
                type="checkbox"
                checked={checked}
                onChange={(e) => toggle([k], e.target.checked)}
                aria-label={`Replace on line ${m.line}`}
                className="mt-[7px] size-4 shrink-0 accent-[var(--ink)] pointer-coarse:mt-3.5"
              />
              <button
                type="button"
                onClick={() => open(m)}
                title="Open the note at this match"
                className={cn(
                  "flex min-w-0 flex-1 gap-2 py-1 text-left text-ui text-text-soft outline-none focus-visible:underline pointer-coarse:py-2.5",
                  !checked && "opacity-55",
                )}
              >
                <span className="w-7 shrink-0 pt-px text-right text-small text-faint tabular-nums">
                  {m.line}
                </span>
                <span className="min-w-0 break-words">
                  {m.before}
                  <del
                    className={cn(
                      "rounded-[3px] bg-danger/12 px-px text-foreground",
                      checked && showWith ? "line-through decoration-danger/70" : "no-underline",
                    )}
                  >
                    {m.text}
                  </del>
                  {checked && showWith && m.with ? (
                    <ins className="rounded-[3px] bg-success/18 px-px text-foreground no-underline">
                      {m.with}
                    </ins>
                  ) : null}
                  {m.after}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </li>
  );
}
