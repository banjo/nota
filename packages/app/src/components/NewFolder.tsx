import { Tooltip, TooltipContent, TooltipTrigger } from "@nota/ui/tooltip";
import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import { FilePlus2, Folder, FolderPlus, MoreHorizontal } from "lucide-react";
import {
  type ComponentProps,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
  useEffect,
  useRef,
  useState,
} from "react";
import { create } from "zustand";
import { addFolder, createNoteIn } from "../actions.ts";
import { useClient } from "../context.ts";
import { checkNewFolder, type FolderDraft } from "../lib/newFolder.ts";
import { useTree } from "../queries.ts";
import { useUI } from "../state.ts";
import { openRowSheet } from "./RowSheet.tsx";

// Folders made straight in the sidebar: the "New folder" button, the
// per-folder buttons on tree rows, and the inline name row they open.

/** The one inline new-folder row on screen, or null. */
export const useFolderDraft = create<{ draft: FolderDraft | null }>(() => ({ draft: null }));

/** Opens the name row inside `parent` ("" for the vault root), unfolding the way to it. */
export function startNewFolder(parent: string) {
  const ui = useUI.getState();
  ui.toggle("section:folders", true);
  if (parent) ui.reveal(`${parent}/_`);
  useFolderDraft.setState({ draft: { parent } });
}

export const cancelNewFolder = () => useFolderDraft.setState({ draft: null });

/** A quiet icon button with a tooltip; the label is also its accessible name. */
export function IconButton({
  label,
  children,
  className,
  ...props
}: Omit<ComponentProps<"button">, "aria-label"> & { label: string; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={label}
          className={cn(
            "grid shrink-0 place-items-center rounded-sm text-faint outline-none hover:bg-hover hover:text-foreground focus-visible:bg-hover focus-visible:text-foreground [&_svg]:size-[15px]",
            className,
          )}
          {...props}
        >
          {children}
        </button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/**
 * "New note in …" and "New folder in …" on a folder row, shown on hover
 * and keyboard focus. Under a finger, which has no hover, the row gets
 * one quiet "…" instead, which opens the same actions as a sheet.
 */
export function FolderRowActions({
  name,
  onNewNote,
  onNewFolder,
  onMore,
}: {
  name: string;
  onNewNote: () => void;
  onNewFolder: () => void;
  onMore?: () => void;
}) {
  return (
    <>
      <div className="absolute inset-y-0 right-1 flex items-center gap-px rounded-sm bg-hover opacity-0 group-hover/item:opacity-100 focus-within:opacity-100 pointer-coarse:hidden">
        <IconButton label={`New note in ${name}`} onClick={onNewNote} className="size-6">
          <FilePlus2 strokeWidth={1.75} />
        </IconButton>
        <IconButton label={`New folder in ${name}`} onClick={onNewFolder} className="size-6">
          <FolderPlus strokeWidth={1.75} />
        </IconButton>
      </div>
      {onMore ? (
        <button
          type="button"
          aria-label={`Actions for ${name}`}
          onClick={onMore}
          className="absolute inset-y-0 right-0 hidden w-11 place-items-center rounded-sm text-faint outline-none active:bg-hover pointer-coarse:grid"
        >
          <MoreHorizontal className="size-[18px]" strokeWidth={1.75} />
        </button>
      ) : null}
    </>
  );
}

/** The row actions wired to the shared actions (the same ones ⌘K runs). */
export function FolderActions({ path, name }: { path: string; name: string }) {
  const client = useClient();
  const qc = useQueryClient();
  return (
    <FolderRowActions
      name={name}
      onNewNote={() => void createNoteIn(client, qc, path)}
      onNewFolder={() => startNewFolder(path)}
      onMore={() => openRowSheet({ kind: "folder", path, name })}
    />
  );
}

/** The name field of the inline row, without data: what it shows for a value and an error. */
export function FolderNameField({
  depth,
  value,
  error,
  busy,
  inputRef,
  onChange,
  onKeyDown,
  onBlur,
}: {
  depth: number;
  value: string;
  error: string;
  busy?: boolean;
  inputRef?: Ref<HTMLInputElement>;
  onChange: (value: string) => void;
  onKeyDown?: (e: KeyboardEvent<HTMLInputElement>) => void;
  onBlur?: () => void;
}) {
  return (
    <div
      className="relative flex h-full items-center gap-[7px] pr-2"
      // Lines up with folder rows: their chevron, then the icon.
      style={{ paddingLeft: 8 + depth * 16 + 16 + 7 }}
    >
      <span className="grid shrink-0 place-items-center text-muted-foreground [&_svg]:size-[15px]">
        <Folder strokeWidth={1.75} />
      </span>
      <input
        ref={inputRef}
        value={value}
        readOnly={busy}
        aria-busy={busy || undefined}
        aria-label="New folder name"
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? "new-folder-error" : undefined}
        placeholder="Folder name"
        spellCheck={false}
        autoComplete="off"
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        onBlur={onBlur}
        className={cn(
          "h-[22px] min-w-0 flex-1 rounded-[4px] bg-surface px-1.5 text-ui text-foreground shadow-[0_0_0_1px_var(--ink),0_0_0_3px_var(--ink-soft)] outline-none placeholder:text-faint pointer-coarse:h-8 pointer-coarse:text-[15px]",
          error && "shadow-[0_0_0_1px_var(--danger)]",
        )}
      />
      {error ? (
        <p
          id="new-folder-error"
          role="alert"
          className="absolute top-full right-2 z-10 rounded-sm bg-surface px-2 py-1 text-small leading-[1.4] text-danger shadow-card"
          style={{ left: 8 + depth * 16 + 16 + 7 + 15 + 7 }}
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}

/** The inline new-folder row: Enter creates, Esc cancels, an empty name cancels. */
export function NewFolderRow({ parent, depth }: { parent: string; depth: number }) {
  const client = useClient();
  const qc = useQueryClient();
  const tree = useTree();
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const done = useRef(false);
  const pending = useRef(false);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    input.current?.focus();
  }, []);

  const check = () => checkNewFolder(value, parent, tree.data ?? { inbox: "", folders: [] });
  const submit = async () => {
    if (done.current || pending.current) return;
    const r = check();
    if (r.error) {
      setError(r.error);
      return;
    }
    if (!r.path) {
      done.current = true;
      cancelNewFolder();
      return;
    }
    pending.current = true;
    setBusy(true);
    const made = await addFolder(client, qc, r.path, { onError: setError });
    pending.current = false;
    setBusy(false);
    if (made === undefined) {
      requestAnimationFrame(() => input.current?.focus());
      return;
    }
    done.current = true;
    cancelNewFolder();
  };

  return (
    <FolderNameField
      depth={depth}
      value={value}
      error={error}
      busy={busy}
      inputRef={input}
      onChange={(v) => {
        setValue(v);
        if (error) setError("");
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          void submit();
        } else if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          done.current = true;
          cancelNewFolder();
        }
      }}
      // Clicking away keeps a good name (as Finder does) and drops the rest.
      onBlur={() => {
        if (pending.current || done.current) return;
        if (check().path) void submit();
        else {
          done.current = true;
          cancelNewFolder();
        }
      }}
    />
  );
}
