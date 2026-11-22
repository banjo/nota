import type { NoteInfo } from "@nota/client";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@nota/ui/command";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@nota/ui/dialog";
import { useQueryClient } from "@tanstack/react-query";
import { FileText, Folder, FolderRoot, Inbox, Plus } from "lucide-react";
import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { createNote, moveNote } from "../actions.ts";
import { useClient } from "../context.ts";
import { compareNames, folderLabel, folderOf } from "../lib/notes.ts";
import { label } from "../lib/shortcuts.ts";
import { useList, useSearch, useTree } from "../queries.ts";
import { type CommandContext, useContributions } from "../registry.ts";
import { currentPath, useUI } from "../state.ts";

// ⌘K (the "Fokus" prototype): search notes, run commands, move notes.
// It opens instantly: keyboard-driven, used all day, so no animation.

const fold = (s: string) => s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();

/** Debounced search text, so typing never waits on the index. */
function useDebounced(value: string, ms: number) {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

function NoteItem({
  note,
  onSelect,
  value,
}: {
  note: NoteInfo;
  onSelect: () => void;
  value: string;
}) {
  return (
    <CommandItem value={value} onSelect={onSelect}>
      <FileText className="size-[15px]" strokeWidth={1.75} />
      <span className="min-w-0 flex-1 truncate">{note.title}</span>
      <span className="ml-3 shrink-0 text-small text-faint">{folderLabel(note.folder)}</span>
    </CommandItem>
  );
}

function SearchMode({ query, close }: { query: string; close: () => void }) {
  const client = useClient();
  const qc = useQueryClient();
  const tree = useTree();
  const recent = useList({ sort: "modified desc", limit: 6 });
  const debounced = useDebounced(query.trim(), 120);
  const search = useSearch(debounced);
  const commands = useContributions("command");
  const navigate = useUI((s) => s.navigate);
  const route = useUI((s) => s.route);
  const q = fold(query.trim());

  const ctx: CommandContext = {
    client,
    queryClient: qc,
    route,
    notePath: currentPath(route),
    navigate,
  };
  const open = (path: string) => {
    close();
    navigate({ kind: "note", path });
  };

  // Titles match instantly from the tree; full text follows from the index.
  const notes = useMemo(() => {
    if (!q) return recent.data ?? [];
    const byTitle = (tree.data?.notes ?? []).filter((n) => fold(n.title).includes(q)).slice(0, 8);
    const seen = new Set(byTitle.map((n) => n.path));
    const fromIndex = debounced ? (search.data ?? []).filter((n) => !seen.has(n.path)) : [];
    return [...byTitle, ...fromIndex].slice(0, 12);
  }, [q, recent.data, tree.data, search.data, debounced]);

  const cmds = commands.filter(
    (c) =>
      (!c.when || c.when(ctx)) &&
      (!q || fold(c.title).includes(q) || (c.keywords ?? []).some((k) => fold(k).includes(q))),
  );

  return (
    <>
      {notes.length ? (
        <CommandGroup heading={q ? "Notes" : "Recent"}>
          {notes.map((n) => (
            <NoteItem
              key={n.path}
              note={n}
              value={`note:${n.path}`}
              onSelect={() => open(n.path)}
            />
          ))}
        </CommandGroup>
      ) : null}
      {cmds.length ? (
        <CommandGroup heading="Commands">
          {cmds.map((c) => (
            <CommandItem
              key={c.id}
              value={`cmd:${c.id}`}
              onSelect={() => {
                close();
                void c.run(ctx);
              }}
            >
              {c.icon ? <c.icon className="size-[15px]" strokeWidth={1.75} /> : null}
              <span className="flex-1 truncate">{c.title}</span>
              {c.shortcut ? <CommandShortcut>{label(c.shortcut)}</CommandShortcut> : null}
            </CommandItem>
          ))}
        </CommandGroup>
      ) : null}
      {q ? (
        <CommandGroup heading={notes.length || cmds.length ? undefined : "Nothing found"}>
          <CommandItem
            value="create"
            onSelect={() => {
              close();
              const inbox = tree.data?.inbox ?? "Inbox";
              void createNote(client, qc, inbox, query.trim());
            }}
          >
            <Plus className="size-[15px]" strokeWidth={1.75} />
            <span className="flex-1 truncate">Create “{query.trim()}”</span>
            <CommandShortcut>↵</CommandShortcut>
          </CommandItem>
        </CommandGroup>
      ) : null}
    </>
  );
}

function MoveMode({ query, path, close }: { query: string; path: string; close: () => void }) {
  const client = useClient();
  const qc = useQueryClient();
  const tree = useTree();
  const q = fold(query.trim());
  const from = folderOf(path);
  const inbox = tree.data?.inbox ?? "Inbox";
  const folders = useMemo(() => {
    const all = new Set((tree.data?.folders ?? []).map((f) => f.path));
    all.add(inbox);
    return [...all].sort(compareNames);
  }, [tree.data, inbox]);
  const shown = ["/", ...folders].filter(
    (f) => f !== (from || "/") && (!q || fold(f === "/" ? "vault root" : f).includes(q)),
  );
  const newFolder = query.trim().replace(/^\/+|\/+$/g, "");
  return (
    <>
      <CommandGroup heading="Move to">
        {shown.map((f) => (
          <CommandItem
            key={f}
            value={`folder:${f}`}
            onSelect={() => {
              close();
              void moveNote(client, qc, path, f);
            }}
          >
            {f === "/" ? (
              <FolderRoot className="size-[15px]" strokeWidth={1.75} />
            ) : f === inbox ? (
              <Inbox className="size-[15px]" strokeWidth={1.75} />
            ) : (
              <Folder className="size-[15px]" strokeWidth={1.75} />
            )}
            <span className="flex-1 truncate">{f === "/" ? "Vault root" : folderLabel(f)}</span>
          </CommandItem>
        ))}
      </CommandGroup>
      {newFolder && !folders.some((f) => fold(f) === fold(newFolder)) ? (
        <CommandGroup>
          <CommandItem
            value="new-folder"
            onSelect={() => {
              close();
              void moveNote(client, qc, path, newFolder);
            }}
          >
            <Plus className="size-[15px]" strokeWidth={1.75} />
            <span className="flex-1 truncate">New folder “{newFolder}”</span>
          </CommandItem>
        </CommandGroup>
      ) : null}
    </>
  );
}

export function Palette() {
  const palette = useUI((s) => s.palette);
  const closePalette = useUI((s) => s.closePalette);
  const tree = useTree();
  const [query, setQuery] = useState("");
  const deferred = useDeferredValue(query);
  const moving = palette.mode === "move" && palette.path ? palette.path : undefined;
  const movingTitle = moving
    ? (tree.data?.notes.find((n) => n.path === moving)?.title ?? moving)
    : "";

  useEffect(() => {
    if (palette.open) setQuery("");
  }, [palette.open, palette.mode]);

  return (
    <Dialog open={palette.open} onOpenChange={(o) => !o && closePalette()}>
      <DialogContent
        instant
        className="top-[14vh] max-w-[580px] translate-y-0 overflow-hidden p-0 max-[720px]:top-3"
        aria-describedby="palette-help"
      >
        <DialogTitle className="sr-only">
          {moving ? "Move note" : "Search and commands"}
        </DialogTitle>
        <DialogDescription id="palette-help" className="sr-only">
          Type to search; arrow keys select, Enter opens, Escape closes.
        </DialogDescription>
        <Command shouldFilter={false} loop>
          <CommandInput
            value={query}
            onValueChange={setQuery}
            placeholder={moving ? `Move “${movingTitle}” to…` : "Search notes or type a command"}
          />
          <CommandList>
            <CommandEmpty>Nothing found</CommandEmpty>
            {moving ? (
              <MoveMode query={deferred} path={moving} close={closePalette} />
            ) : (
              <SearchMode query={deferred} close={closePalette} />
            )}
          </CommandList>
          <div className="flex gap-3.5 border-t border-divider px-3.5 py-[9px] text-micro text-faint max-[720px]:hidden">
            <span>↑↓ select</span>
            <span>↵ open</span>
            <span>esc close</span>
          </div>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
