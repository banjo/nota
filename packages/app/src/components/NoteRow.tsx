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
import { useContributions } from "../registry.ts";
import { useUI } from "../state.ts";
import { Row } from "./Row.tsx";

/** A note in the sidebar: opens on click, with a menu that is always reachable. */
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
  return (
    <div className={cn("group/note relative", className)}>
      <Row
        icon={<FileText strokeWidth={1.75} />}
        label={note.title}
        depth={depth}
        active={active}
        muted
        className="pr-8"
        onClick={() => navigate({ kind: "note", path: note.path })}
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

export function NoteMenu({ path, active }: { path: string; active?: boolean }) {
  const client = useClient();
  const qc = useQueryClient();
  const openPalette = useUI((s) => s.openPalette);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label="Note actions"
        className={cn(
          "absolute top-1/2 right-1 grid size-6 -translate-y-1/2 place-items-center rounded-xs text-faint opacity-0 outline-none hover:bg-selected hover:text-foreground focus-visible:opacity-100 data-[state=open]:opacity-100",
          "group-hover/note:opacity-100 pointer-coarse:size-9 pointer-coarse:opacity-100",
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
