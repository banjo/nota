import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import { FileText, Folder } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { create } from "zustand";
import { renameFolder, renameNote } from "../actions.ts";
import { useClient } from "../context.ts";
import { checkFolderRename, checkNoteRename, nameOf, type RenameTarget } from "../lib/rename.ts";
import { useTree } from "../queries.ts";
import { useUI } from "../state.ts";

// Renaming in the tree, where the row is: the row turns into a name
// field. Enter renames, Esc cancels, an empty or unchanged name cancels,
// and clicking away keeps a good name (as Finder does). Rows, the
// right-click menu, the touch sheet and ⌘K all start it the same way.

export const useRename = create<{ target: RenameTarget | null }>(() => ({ target: null }));

export const startRename = (target: RenameTarget) => {
  const ui = useUI.getState();
  ui.toggle("section:folders", true);
  ui.reveal(`${target.path}/_`);
  // A phone shows one pane at a time; the field lives in the tree.
  if (ui.pane === "main") ui.back();
  if (!ui.sidebarOpen) ui.toggleSidebar();
  useRename.setState({ target });
};

export const cancelRename = () => useRename.setState({ target: null });

/** Whether this row is the one being renamed. */
export const useRenaming = (kind: RenameTarget["kind"], path: string) =>
  useRename((s) => s.target?.kind === kind && s.target.path === path);

/** The inline name row: `indent` lines it up with the row it replaces. */
export function RenameRow({
  target,
  title,
  indent,
}: {
  target: RenameTarget;
  /** The note's current title (its row's label). */
  title?: string;
  indent: number;
}) {
  const client = useClient();
  const qc = useQueryClient();
  const tree = useTree();
  const [value, setValue] = useState(nameOf(target, title));
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const done = useRef(false);
  const pending = useRef(false);
  const input = useRef<HTMLInputElement>(null);
  const isNote = target.kind === "note";

  useEffect(() => {
    input.current?.focus();
    input.current?.select();
  }, []);

  const check = (): { go: boolean; error: string; to: string } => {
    if (isNote) {
      const sibs = (tree.data?.notes ?? [])
        .filter(
          (n) =>
            n.folder === (target.path.includes("/") ? target.path.replace(/\/[^/]*$/, "") : ""),
        )
        .map((n) => n.path);
      const r = checkNoteRename(value, target.path, sibs);
      return { go: !!r.title && r.title !== nameOf(target, title), error: r.error, to: r.title };
    }
    const r = checkFolderRename(value, target.path, tree.data ?? { inbox: "", folders: [] });
    return { go: !!r.to, error: r.error, to: r.to };
  };
  const finish = () => {
    done.current = true;
    cancelRename();
  };
  const submit = async () => {
    if (done.current || pending.current) return;
    const r = check();
    if (r.error) {
      setError(r.error);
      return;
    }
    if (!r.go) return finish();
    pending.current = true;
    setBusy(true);
    const ok = isNote
      ? await renameNote(client, qc, target.path, r.to, { onError: setError })
      : await renameFolder(client, qc, target.path, r.to, { onError: setError });
    pending.current = false;
    setBusy(false);
    if (ok === undefined) {
      requestAnimationFrame(() => input.current?.focus());
      return;
    }
    finish();
  };

  const Icon = isNote ? FileText : Folder;
  return (
    <div
      className="relative flex h-full items-center gap-[7px] pr-2"
      style={{ paddingLeft: indent }}
    >
      <span className="grid shrink-0 place-items-center text-muted-foreground [&_svg]:size-[15px]">
        <Icon strokeWidth={1.75} />
      </span>
      <input
        ref={input}
        value={value}
        readOnly={busy}
        aria-busy={busy || undefined}
        aria-label={isNote ? "Note name" : "Folder name"}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? "rename-error" : undefined}
        spellCheck={false}
        autoComplete="off"
        onChange={(e) => {
          setValue(e.target.value);
          if (error) setError("");
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.nativeEvent.isComposing) {
            e.preventDefault();
            void submit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            finish();
          }
        }}
        onBlur={() => {
          if (pending.current || done.current) return;
          if (check().go) void submit();
          else finish();
        }}
        className={cn(
          "h-[22px] min-w-0 flex-1 rounded-[4px] bg-surface px-1.5 text-ui text-foreground shadow-[0_0_0_1px_var(--ink),0_0_0_3px_var(--ink-soft)] outline-none pointer-coarse:h-8 pointer-coarse:text-[15px]",
          error && "shadow-[0_0_0_1px_var(--danger)]",
        )}
      />
      {error ? (
        <p
          id="rename-error"
          role="alert"
          className="absolute top-full right-2 left-8 z-10 rounded-sm bg-surface px-2 py-1 text-small leading-[1.4] text-danger shadow-card"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}
