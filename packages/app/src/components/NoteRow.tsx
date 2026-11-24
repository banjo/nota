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
import { FileText, FolderInput, MoreHorizontal, Trash2 } from "lucide-react";
import { deleteNote } from "../actions.ts";
import { useClient } from "../context.ts";
import { useLongPress } from "../lib/longPress.ts";
import { useNarrow } from "../lib/viewport.ts";
import { useContributions } from "../registry.ts";
import { useUI } from "../state.ts";
import { RenameRow, useRenaming } from "./Rename.tsx";
import { Row } from "./Row.tsx";
import { openRowSheet } from "./RowSheet.tsx";

/**
 * A note in the sidebar: opens on click. Its "…" menu shows on hover and
 * focus; under a finger a long press opens the same actions as a sheet,
 * so the tree doesn't carry a "…" on every row.
 */
export function NoteRow({
  note,
  depth,
  className,
}: {
  note: NoteInfo;
  depth: number;
  className?: string;
}) {
  const route = useUI((s) => s.route);
  const navigate = useUI((s) => s.navigate);
  const active = route.kind === "note" && route.path === note.path;
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
}

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
      <DropdownMenuTrigger
        aria-label="Note actions"
        className={cn(
          "absolute top-1/2 right-1 grid size-6 -translate-y-1/2 place-items-center rounded-xs text-faint opacity-0 outline-none hover:bg-selected hover:text-foreground focus-visible:opacity-100 data-[state=open]:opacity-100",
          "group-hover/note:opacity-100",
          inBar ? "pointer-coarse:size-11 pointer-coarse:opacity-100" : "pointer-coarse:hidden",
          active && "opacity-100",
        )}
      >
        <MoreHorizontal className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="right">
        <DropdownMenuItem onSelect={() => openPalette("move", path)}>
          <FolderInput /> Move to…
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => void deleteNote(client, qc, path)}>
          <Trash2 /> Move to trash
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
