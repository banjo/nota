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
import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import { FileText, Folder, FolderPlus, FolderRoot, Inbox, Plus } from "lucide-react";
import { memo, useCallback, useDeferredValue, useEffect, useMemo, useState } from "react";
import {
  addFolder,
  addFolderThenNote,
  createFolderAndMove,
  createNote,
  createNoteIn,
  fileNote,
} from "../actions.ts";
import { useClient } from "../context.ts";
import { confirmMove, saveToVault, useOpened } from "../external.ts";
import {
  choiceLabel,
  fold,
  folderChoices,
  readRecentFolders,
  resolveNewFolder,
} from "../lib/folders.ts";
import { folderLabel, folderOf } from "../lib/notes.ts";
import { label } from "../lib/shortcuts.ts";
import { useList, useSearch, useTree, useTreeValue } from "../queries.ts";
import {
  type Command as RegisteredCommand,
  type CommandContext,
  useContributions,
} from "../registry.ts";
import { currentPath, type PaletteMode, useUI } from "../state.ts";

// ⌘K (the "Fokus" prototype): search notes, run commands, move notes.
// It opens instantly: keyboard-driven, used all day, so no animation.

/** Debounced search text, so typing never waits on the index. */
function useDebounced(value: string, ms: number) {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

// Rows are memoised: a keystroke re-renders the rows whose note or
// command changed, not every row in the list.

const NoteItem = memo(function NoteItem({
  note,
  open,
}: {
  note: NoteInfo;
  open: (path: string) => void;
}) {
  return (
    <CommandItem value={`note:${note.path}`} onSelect={() => open(note.path)}>
      <FileText className="size-[15px]" strokeWidth={1.75} />
      <span className="min-w-0 flex-1 truncate">{note.title}</span>
      <span className="ml-3 shrink-0 text-small text-faint">{folderLabel(note.folder)}</span>
    </CommandItem>
  );
});

const CommandRow = memo(function CommandRow({
  command: c,
  ctx,
  close,
}: {
  command: RegisteredCommand;
  ctx: CommandContext;
  close: () => void;
}) {
  return (
    <CommandItem
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
  );
});

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

  // Stable between keystrokes, so the memoised rows skip re-rendering.
  // Spelled out rather than left to the React Compiler: measured, the rows
  // re-rendered on every keystroke without it.
  const ctx = useMemo<CommandContext>(
    () => ({ client, queryClient: qc, route, notePath: currentPath(route), navigate }),
    [client, qc, route, navigate],
  );
  const open = useCallback(
    (path: string) => {
      close();
      navigate({ kind: "note", path });
    },
    [close, navigate],
  );

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
            <NoteItem key={n.path} note={n} open={open} />
          ))}
        </CommandGroup>
      ) : null}
      {cmds.length ? (
        <CommandGroup heading="Commands">
          {cmds.map((c) => (
            <CommandRow key={c.id} command={c} ctx={ctx} close={close} />
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

/**
 * Lists folders to file a note in (mode "move"), to make a note in
 * ("noteIn"), or to save an outside file in (`external`).
 */
function FolderMode({
  query,
  path,
  external,
  close,
}: {
  query: string;
  path?: string;
  external?: { id: string; move: boolean };
  close: () => void;
}) {
  const client = useClient();
  const qc = useQueryClient();
  const tree = useTree();
  const suggesters = useContributions("inboxSuggester");
  const inbox = tree.data?.inbox ?? "Inbox";
  const note = path && !external ? tree.data?.notes.find((n) => n.path === path) : undefined;
  const suggestion = external
    ? { folder: inbox, reason: "Inbox", score: 1 }
    : tree.data && note && note.folder === inbox
      ? suggesters
          .flatMap((s) => s.suggest(note, { tree: tree.data! }))
          .sort((a, b) => b.score - a.score)[0]
      : undefined;
  const choices = folderChoices({
    tree: tree.data ?? { inbox, folders: [] },
    query,
    from: path && !external ? folderOf(path) : undefined,
    recent: readRecentFolders(),
    suggestion,
  });
  const pick = (folder: string) => {
    close();
    if (external?.move) confirmMove(client, qc, external.id, folder);
    else if (external) void saveToVault(client, qc, external.id, folder);
    else if (path) void fileNote(client, qc, path, folder);
    else void createNoteIn(client, qc, folder);
  };
  const row = (f: string, hint?: string) => (
    <CommandItem key={f} value={`folder:${f}`} onSelect={() => pick(f)}>
      {f === "/" ? (
        <FolderRoot className="size-[15px]" strokeWidth={1.75} />
      ) : f === inbox ? (
        <Inbox className="size-[15px]" strokeWidth={1.75} />
      ) : (
        <Folder className="size-[15px]" strokeWidth={1.75} />
      )}
      <span className="flex-1 truncate">{choiceLabel(f)}</span>
      {hint ? <span className="ml-3 shrink-0 text-small text-faint">{hint}</span> : null}
    </CommandItem>
  );
  return (
    <>
      {choices.suggested ? (
        <CommandGroup heading="Suggested">
          {row(choices.suggested.folder, choices.suggested.reason)}
        </CommandGroup>
      ) : null}
      {choices.recent.length ? (
        <CommandGroup heading="Recent">{choices.recent.map((f) => row(f))}</CommandGroup>
      ) : null}
      <CommandGroup
        heading={
          external
            ? external.move
              ? "Move into"
              : "Save in"
            : path
              ? "Add to folder"
              : "New note in"
        }
      >
        {choices.rest.map((f) => row(f))}
      </CommandGroup>
      {choices.create ? (
        <CommandGroup>
          <CommandItem
            value="create-folder"
            onSelect={() => {
              if (external) return pick(choices.create!);
              close();
              if (path) void createFolderAndMove(client, qc, path, choices.create!);
              else void addFolderThenNote(client, qc, choices.create!);
            }}
          >
            <Plus className="size-[15px]" strokeWidth={1.75} />
            <span className="flex-1 truncate">
              Create “{choices.create}” and{" "}
              {external
                ? external.move
                  ? "move it there"
                  : "save it there"
                : path
                  ? "move here"
                  : "add a note"}
            </span>
          </CommandItem>
        </CommandGroup>
      ) : null}
    </>
  );
}

/** "Add folder…": the typed name, relative to the selected folder, is the one choice. */
function NewFolderMode({ query, base, close }: { query: string; base: string; close: () => void }) {
  const client = useClient();
  const qc = useQueryClient();
  const target = resolveNewFolder(query, base);
  return target ? (
    <CommandGroup
      heading={base && !query.trim().startsWith("/") ? `In ${folderLabel(base)}` : "In the vault"}
    >
      <CommandItem
        value="add-folder"
        onSelect={() => {
          close();
          void addFolder(client, qc, target);
        }}
      >
        <FolderPlus className="size-[15px]" strokeWidth={1.75} />
        <span className="flex-1 truncate">Create “{folderLabel(target)}”</span>
        <CommandShortcut>↵</CommandShortcut>
      </CommandItem>
    </CommandGroup>
  ) : (
    <div className="px-3.5 py-6 text-center text-small text-faint">
      {base ? `New folder in ${folderLabel(base)}. ` : ""}Use / for subfolders
      {base ? ", or start with / for the vault root" : ""}.
    </div>
  );
}

export function Palette() {
  const palette = useUI((s) => s.palette);
  const closePalette = useUI((s) => s.closePalette);
  const moving = palette.mode === "move" && palette.path ? palette.path : undefined;
  // Only the moved note's title: the closed palette ignores tree refetches.
  const movingTitle =
    useTreeValue((t) => (moving ? t.notes.find((n) => n.path === moving)?.title : undefined)) ??
    moving ??
    "";
  // A "move" palette without a note has nothing to move: fall back to search.
  const mode = palette.mode === "move" && !moving ? "all" : palette.mode;
  const fileName = useOpened(
    (s) => s.previews.find((p) => p.id === palette.path)?.name ?? "the file",
  );
  const title = {
    all: "Search and commands",
    move: "Add note to folder",
    noteIn: "New note in folder",
    newFolder: "Add folder",
    saveExternal: "Save to vault",
    moveExternal: "Move into vault",
  }[mode];
  const placeholder = {
    all: "Search notes or type a command",
    move: `Add “${movingTitle}” to folder…`,
    noteIn: "New note in folder…",
    newFolder: "Folder name, e.g. Projects/Ideas",
    saveExternal: `Save “${fileName}” in folder…`,
    moveExternal: `Move “${fileName}” into folder…`,
  }[mode];

  return (
    <Dialog open={palette.open} onOpenChange={(o) => !o && closePalette()}>
      <DialogContent
        instant
        className={cn(
          "top-[14vh] max-w-[580px] translate-y-0 overflow-hidden p-0",
          // A phone: a sheet from the bottom edge, as tall as the space
          // above the keyboard, the field on top where the eye is.
          "max-[720px]:top-auto max-[720px]:bottom-(--keyboard-inset,0px) max-[720px]:left-0 max-[720px]:flex max-[720px]:h-[calc(var(--visual-height,100dvh)-var(--safe-top)-8px)] max-[720px]:w-full max-[720px]:max-w-none max-[720px]:translate-x-0 max-[720px]:flex-col max-[720px]:rounded-b-none max-[720px]:pr-(--safe-right) max-[720px]:pb-(--safe-bottom) max-[720px]:pl-(--safe-left) max-[720px]:in-data-keyboard:pb-0",
        )}
        aria-describedby="palette-help"
      >
        <DialogTitle className="sr-only">{title}</DialogTitle>
        <DialogDescription id="palette-help" className="sr-only">
          Type to search; arrow keys select, Enter opens, Escape closes.
        </DialogDescription>
        {/* Mounted while open, once per mode: each open starts from an
            empty query, and typing re-renders this, not the dialog. */}
        <PaletteBody
          key={palette.mode}
          mode={mode}
          path={palette.path}
          moving={moving}
          placeholder={placeholder}
          close={closePalette}
        />
      </DialogContent>
    </Dialog>
  );
}

function PaletteBody({
  mode,
  path,
  moving,
  placeholder,
  close,
}: {
  mode: PaletteMode;
  path?: string;
  moving?: string;
  placeholder: string;
  close: () => void;
}) {
  const [query, setQuery] = useState("");
  const deferred = useDeferredValue(query);
  return (
    <Command shouldFilter={false} loop className="max-[720px]:min-h-0 max-[720px]:flex-1">
      <CommandInput
        value={query}
        onValueChange={setQuery}
        placeholder={placeholder}
        enterKeyHint="go"
        trailing={
          <button
            type="button"
            onClick={close}
            className="-mr-2 hidden h-11 shrink-0 px-2 text-[16px] text-ink outline-none active:opacity-60 max-[720px]:block"
          >
            Cancel
          </button>
        }
      />
      <CommandList className="max-[720px]:max-h-none max-[720px]:min-h-0 max-[720px]:flex-1">
        {mode === "newFolder" ? null : <CommandEmpty>Nothing found</CommandEmpty>}
        {mode === "saveExternal" || mode === "moveExternal" ? (
          <FolderMode
            query={deferred}
            external={{ id: path ?? "", move: mode === "moveExternal" }}
            close={close}
          />
        ) : mode === "move" ? (
          <FolderMode query={deferred} path={moving} close={close} />
        ) : mode === "noteIn" ? (
          <FolderMode query={deferred} close={close} />
        ) : mode === "newFolder" ? (
          <NewFolderMode query={deferred} base={path ?? ""} close={close} />
        ) : (
          <SearchMode query={deferred} close={close} />
        )}
      </CommandList>
      <div className="flex gap-3.5 border-t border-divider px-3.5 py-[9px] text-micro text-faint max-[720px]:hidden">
        <span>↑↓ select</span>
        <span>↵ open</span>
        <span>esc close</span>
      </div>
    </Command>
  );
}
