import type { NoteInfo } from "@nota/client";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@nota/ui/dropdown-menu";
import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import {
  FileText,
  FolderInput,
  Globe,
  History,
  MoreHorizontal,
  Pin,
  PinOff,
  Trash2,
} from "lucide-react";
import { memo } from "react";
import { deleteNote, setPinned } from "../actions.ts";
import { useClient } from "../context.ts";
import { useArmed } from "../lib/armed.ts";
import { useLongPress } from "../lib/longPress.ts";
import { isPinned } from "../lib/pinned.ts";
import { useNarrow } from "../lib/viewport.ts";
import { useTreeValue } from "../queries.ts";
import { useContributions } from "../registry.ts";
import { useUI } from "../state.ts";
import { openHistory } from "./History.tsx";
import { openShare } from "./Share.tsx";
import { RenameRow, useRenaming } from "./Rename.tsx";
import { Row } from "./Row.tsx";
import { openRowSheet } from "./RowSheet.tsx";

/**
 * A note in the sidebar: opens on click. Its "…" menu shows on hover and
 * focus; under a finger a long press opens the same actions as a sheet,
 * so the tree doesn't carry a "…" on every row.
 */
export const NoteRow = memo(function NoteRow({
  note,
  depth,
  className,
}: {
  note: NoteInfo;
  depth: number;
  className?: string;
}) {
  // Only whether this row is the open note: a navigation re-renders the
  // two rows it changes, not the whole tree.
  const active = useUI((s) => s.route.kind === "note" && s.route.path === note.path);
  const navigate = useUI((s) => s.navigate);
  const decorators = useContributions("treeDecorator");
  const decos = decorators
    .map((d) => d.decorate({ kind: "note", path: note.path, note }))
    .filter((d) => d !== null);
  const longPress = useLongPress(() =>
    openRowSheet({ kind: "note", path: note.path, title: note.title }),
  );
  const renaming = useRenaming("note", note.path);
  if (renaming)
    return (
      <div className={cn("h-7 pointer-coarse:h-11", className)}>
        <RenameRow
          target={{ kind: "note", path: note.path }}
          title={note.title}
          indent={8 + depth * 16}
        />
      </div>
    );
  return (
    <div className={cn("group/note relative", className)}>
      <Row
        data-ctx-kind="note"
        data-ctx-path={note.path}
        data-ctx-title={note.title}
        icon={<FileText strokeWidth={1.75} />}
        label={note.title}
        depth={depth}
        active={active}
        muted
        className="pr-8 pointer-coarse:pr-2"
        onClick={() => navigate({ kind: "note", path: note.path })}
        {...longPress}
        trailing={decos.map((d, i) =>
          d.dot ? (
            <span
              key={i}
              aria-label={d.label}
              title={d.label}
              className="size-[7px] shrink-0 rounded-full"
              style={{ background: d.dot }}
            />
          ) : null,
        )}
        count={decos.find((d) => d.badge !== undefined)?.badge}
      />
      <NoteMenu path={note.path} active={active} />
    </div>
  );
});

/**
 * A note's actions. In the tree (`title` absent) it is the row's hover
 * "…", hidden under a finger. In the page's top bar (`title` given) it is
 * always there; on a phone it opens the note's sheet, which also holds
 * the view modes.
 */
export function NoteMenu({
  path,
  active,
  title,
}: {
  path: string;
  active?: boolean;
  title?: string;
}) {
  const client = useClient();
  const qc = useQueryClient();
  const openPalette = useUI((s) => s.openPalette);
  const narrow = useNarrow();
  const inBar = title !== undefined;
  // In the tree every row has one: the menu mounts on first use (lib/armed.ts).
  const { armed, ref, standIn } = useArmed();
  const triggerClass = cn(
    "absolute top-1/2 right-1 grid size-6 -translate-y-1/2 place-items-center rounded-xs text-faint opacity-0 outline-none hover:bg-selected hover:text-foreground focus-visible:opacity-100 data-[state=open]:opacity-100",
    "group-hover/note:opacity-100",
    inBar ? "pointer-coarse:size-11 pointer-coarse:opacity-100" : "pointer-coarse:hidden",
    active && "opacity-100",
  );
  if (!inBar && !armed)
    return (
      <button
        type="button"
        aria-label="Note actions"
        aria-haspopup="menu"
        aria-expanded={false}
        data-state="closed"
        className={triggerClass}
        {...standIn}
      >
        <MoreHorizontal className="size-4" />
      </button>
    );
  if (inBar && narrow)
    return (
      <button
        type="button"
        aria-label="Note actions"
        onClick={() => openRowSheet({ kind: "note", path, title, page: true })}
        className="grid size-11 place-items-center rounded-sm text-muted-foreground outline-none active:bg-hover"
      >
        <MoreHorizontal className="size-5" />
      </button>
    );
  return (
    <DropdownMenu>
      <DropdownMenuTrigger ref={ref} aria-label="Note actions" className={triggerClass}>
        <MoreHorizontal className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="right">
        <PinItem path={path} />
        <DropdownMenuItem onSelect={() => openPalette("move", path)}>
          <FolderInput /> Move to…
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => openHistory(path)}>
          <History /> History
        </DropdownMenuItem>
        {client.shares ? (
          <DropdownMenuItem onSelect={() => openShare(path)}>
            <Globe /> Share…
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => void deleteNote(client, qc, path)}>
          <Trash2 /> Move to trash
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function PinItem({ path }: { path: string }) {
  const client = useClient();
  const qc = useQueryClient();
  const pinned = useTreeValue((t) => {
    const n = t.notes.find((x) => x.path === path);
    return !!n && isPinned(n);
  });
  return (
    <DropdownMenuItem onSelect={() => void setPinned(client, qc, path, !pinned)}>
      {pinned ? <PinOff /> : <Pin />} {pinned ? "Unpin" : "Pin"}
    </DropdownMenuItem>
  );
}
