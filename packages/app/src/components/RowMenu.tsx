import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@nota/ui/context-menu";
import { useQueryClient } from "@tanstack/react-query";
import {
  FilePlus2,
  FolderInput,
  FolderPlus,
  Globe,
  History,
  type LucideIcon,
  PenLine,
  Pin,
  PinOff,
  TextSearch,
  Trash2,
  FileDown,
} from "lucide-react";
import { type MouseEvent, type ReactNode, useState } from "react";
import type { Tree } from "@nota/client";
import { createNote, createNoteIn, deleteNote, newNoteFolder, setPinned } from "../actions.ts";
import { useClient } from "../context.ts";
import { requestFind } from "../lib/find.ts";
import { useLongPress } from "../lib/longPress.ts";
import { folderOf } from "../lib/notes.ts";
import { isPinned } from "../lib/pinned.ts";
import { keys, useInboxName } from "../queries.ts";
import { useUI } from "../state.ts";
import { openHistory } from "./History.tsx";
import { startNewFolder } from "./NewFolder.tsx";
import { startRename } from "./Rename.tsx";
import { openRowSheet, type RowTarget } from "./RowSheet.tsx";
import { openShare } from "./Share.tsx";
import { openExport } from "./Export.tsx";

// The actions of a sidebar row, as a right-click menu on a desk and as a
// bottom sheet under a finger (a long press): one list, two skins. Every
// action is also a ⌘K command or shares its function (note.new,
// folder.add, note.addToFolder, note.newInFolder, note.rename,
// folder.rename, note.pin, note.history, note.share, note.export, note.delete).

/** Puts the cursor in the open note's title field, selected: renaming it where it is. */
export const focusNoteTitle = (): void => {
  requestAnimationFrame(() =>
    document.dispatchEvent(new CustomEvent("nota:focus-title", { detail: { select: true } })),
  );
};

export type MenuTarget = RowTarget | { kind: "empty" };

export interface RowAction {
  id: string;
  label: string;
  icon: LucideIcon;
  run: () => void;
  destructive?: boolean;
  /** A separator goes between groups. */
  group: number;
}

/** The actions for what was pressed: a note, a folder, or the empty sidebar. */
export function useRowActions(): (t: MenuTarget) => RowAction[] {
  const client = useClient();
  const qc = useQueryClient();
  const inbox = useInboxName();
  const openPalette = useUI((s) => s.openPalette);
  return (t) => {
    if (t.kind === "folder") {
      return [
        {
          id: "newNote",
          label: "New note here",
          icon: FilePlus2,
          run: () => void createNoteIn(client, qc, t.path),
          group: 0,
        },
        {
          id: "newFolder",
          label: "New subfolder",
          icon: FolderPlus,
          run: () => startNewFolder(t.path),
          group: 0,
        },
        // The inbox is named in the vault's settings, not renamed here.
        ...(t.path === inbox
          ? []
          : [
              {
                id: "rename",
                label: "Rename folder",
                icon: PenLine,
                run: () => startRename({ kind: "folder", path: t.path }),
                group: 1,
              },
            ]),
        ...(client.export
          ? [
              {
                id: "export",
                label: "Export folder…",
                icon: FileDown,
                run: () => openExport({ kind: "folder", path: t.path }),
                group: 1,
              },
            ]
          : []),
      ];
    }
    if (t.kind === "note") {
      const folder = folderOf(t.path);
      const note = qc.getQueryData<Tree>(keys.tree)?.notes.find((n) => n.path === t.path);
      const pinned = !!note && isPinned(note);
      return [
        {
          id: "newNote",
          label: "New note",
          icon: FilePlus2,
          run: () => void createNoteIn(client, qc, folder || "/"),
          group: 0,
        },
        {
          id: "newFolder",
          label: "New folder",
          icon: FolderPlus,
          run: () => startNewFolder(folder),
          group: 0,
        },
        // On the note's own page (the phone's sheet): find in it.
        ...(t.page
          ? [
              {
                id: "find",
                label: "Find in note",
                icon: TextSearch,
                run: () => requestFind(false),
                group: 1,
              },
            ]
          : []),
        {
          id: "pin",
          label: pinned ? "Unpin" : "Pin",
          icon: pinned ? PinOff : Pin,
          run: () => void setPinned(client, qc, t.path, !pinned),
          group: 1,
        },
        {
          id: "rename",
          label: "Rename note",
          icon: PenLine,
          run: () =>
            // From the note's own page the title field is the place.
            t.page ? focusNoteTitle() : startRename({ kind: "note", path: t.path }),
          group: 1,
        },
        {
          id: "move",
          label: "Move to…",
          icon: FolderInput,
          run: () => openPalette("move", t.path),
          group: 1,
        },
        {
          id: "history",
          label: "History",
          icon: History,
          run: () => openHistory(t.path),
          group: 1,
        },
        ...(client.shares
          ? [
              {
                id: "share",
                label: "Share…",
                icon: Globe,
                run: () => openShare(t.path),
                group: 1,
              },
            ]
          : []),
        ...(client.export
          ? [
              {
                id: "export",
                label: "Export…",
                icon: FileDown,
                run: () => openExport({ kind: "note", path: t.path }),
                group: 1,
              },
            ]
          : []),
        {
          id: "delete",
          label: "Move to trash",
          icon: Trash2,
          run: () => void deleteNote(client, qc, t.path),
          destructive: true,
          group: 2,
        },
      ];
    }
    return [
      {
        id: "newNote",
        label: "New note",
        icon: FilePlus2,
        run: () => void createNote(client, qc, newNoteFolder(useUI.getState().route, inbox)),
        group: 0,
      },
      {
        id: "newFolder",
        label: "New folder",
        icon: FolderPlus,
        run: () => startNewFolder(""),
        group: 0,
      },
    ];
  };
}

/** What the press landed on: rows carry data-ctx-kind and data-ctx-path. */
export function menuTarget(el: EventTarget | null): MenuTarget {
  const row = (el as Element | null)?.closest?.<HTMLElement>("[data-ctx-kind]") ?? null;
  const path = row?.dataset.ctxPath;
  if (!row || path === undefined) return { kind: "empty" };
  if (row.dataset.ctxKind === "folder")
    return { kind: "folder", path, name: path.split("/").pop() ?? path };
  return { kind: "note", path, title: row.dataset.ctxTitle ?? "" };
}

const coarse = () => typeof matchMedia !== "undefined" && matchMedia("(pointer: coarse)").matches;

/**
 * Right-click (desk) or long press (touch) anywhere in the sidebar's
 * list: on a note row, a folder row, or the empty space and the "No
 * folders yet" note. One menu for the whole list, filled from what
 * was pressed.
 */
export function SidebarContextMenu({ children }: { children: ReactNode }) {
  const actions = useRowActions();
  const [target, setTarget] = useState<MenuTarget>({ kind: "empty" });
  // Under a finger the long press opens the sheet instead (no double menu).
  const press = useLongPress(() => openRowSheet({ kind: "empty" }));
  const onlyEmpty = (e: { target: EventTarget }, fn: () => void) => {
    if (e.target instanceof Element && !e.target.closest("button,a,input,[data-ctx-kind]")) fn();
  };
  const items = actions(target);
  return (
    <ContextMenu>
      <ContextMenuTrigger
        asChild
        disabled={coarse()}
        onContextMenu={(e: MouseEvent) => setTarget(menuTarget(e.target))}
      >
        <div
          className="contents"
          onPointerDown={(e) => onlyEmpty(e, () => press.onPointerDown(e))}
          onPointerMove={(e) => press.onPointerMove(e)}
          onPointerUp={() => press.onPointerUp()}
          onPointerCancel={() => press.onPointerCancel()}
        >
          {children}
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent
        aria-label={target.kind === "empty" ? "Sidebar actions" : "Row actions"}
        // Focus goes on to what the action opened (a name field, the palette),
        // not back to the list.
        onCloseAutoFocus={(e) => e.preventDefault()}
      >
        {items.map((a, i) => (
          <div key={a.id} role="none">
            {i > 0 && items[i - 1]!.group !== a.group ? <ContextMenuSeparator /> : null}
            <ContextMenuItem
              variant={a.destructive ? "destructive" : "default"}
              // After the menu has closed, so a name field it opens keeps focus.
              onSelect={() => void setTimeout(a.run, 0)}
            >
              <a.icon /> {a.label}
            </ContextMenuItem>
          </div>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}
