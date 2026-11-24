import { Sheet, SheetContent, SheetItem, SheetSeparator } from "@nota/ui/sheet";
import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import { FilePlus2, FolderInput, FolderPlus, Trash2 } from "lucide-react";
import { useRef } from "react";
import { create } from "zustand";
import { createNoteIn, deleteNote } from "../actions.ts";
import { useClient } from "../context.ts";
import { useUI } from "../state.ts";
import { MODES } from "./ModeSwitch.tsx";
import { startNewFolder } from "./NewFolder.tsx";

// The touch twin of the tree's hover buttons and "…" menus: one bottom
// sheet for a note or a folder, opened by a row's "…" under a finger, a
// long press on a row, or the note page's "…" on a phone. Every action
// in it is also a ⌘K command (note.addToFolder, note.delete,
// note.newInFolder, folder.add, view.mode.*).

export type RowTarget =
  | { kind: "note"; path: string; title: string; page?: boolean }
  | { kind: "folder"; path: string; name: string };

export const useRowSheet = create<{ target: RowTarget | null }>(() => ({ target: null }));
export const openRowSheet = (target: RowTarget) => useRowSheet.setState({ target });
const close = () => useRowSheet.setState({ target: null });

/** Live / Reading / Source as one segmented control, sized for a thumb. */
function ModeSegments() {
  const mode = useUI((s) => s.noteMode);
  const setMode = useUI((s) => s.setNoteMode);
  return (
    <div role="group" aria-label="View mode" className="mx-1 mb-2 flex rounded-lg bg-hover p-1">
      {MODES.map((m) => {
        const on = mode === m.id;
        return (
          <button
            key={m.id}
            type="button"
            aria-pressed={on}
            onClick={() => setMode(m.id)}
            className={cn(
              "flex h-10 flex-1 items-center justify-center gap-1.5 rounded-md text-[15px] outline-none select-none focus-visible:ring-2 focus-visible:ring-ink/40",
              on ? "bg-background text-foreground shadow-xs" : "text-muted-foreground",
            )}
          >
            <m.icon className="size-4" strokeWidth={1.75} aria-hidden />
            {m.name}
          </button>
        );
      })}
    </div>
  );
}

export function RowSheet() {
  const client = useClient();
  const qc = useQueryClient();
  const openPalette = useUI((s) => s.openPalette);
  const target = useRowSheet((s) => s.target);
  // Keep the content while the sheet slides out.
  const last = useRef(target);
  if (target) last.current = target;
  const t = target ?? last.current;
  // An action that moves focus on (the new-folder field, the palette)
  // must not have it pulled back to the row when the sheet closes.
  const acted = useRef(false);
  const act = (fn: () => void) => () => {
    acted.current = true;
    close();
    fn();
  };

  return (
    <Sheet open={!!target} onOpenChange={(o) => (o ? null : close())}>
      {t ? (
        <SheetContent
          title={t.kind === "note" ? t.title || "Untitled" : t.name}
          onOpenAutoFocus={(e) => {
            acted.current = false;
            // No focus ring on the first item for a finger.
            e.preventDefault();
          }}
          onCloseAutoFocus={(e) => {
            if (acted.current) e.preventDefault();
          }}
        >
          {t.kind === "note" ? (
            <>
              {t.page ? <ModeSegments /> : null}
              <SheetItem onClick={act(() => openPalette("move", t.path))}>
                <FolderInput strokeWidth={1.75} /> Move to…
              </SheetItem>
              <SheetSeparator />
              <SheetItem
                variant="destructive"
                onClick={act(() => void deleteNote(client, qc, t.path))}
              >
                <Trash2 strokeWidth={1.75} /> Move to trash
              </SheetItem>
            </>
          ) : (
            <>
              <SheetItem onClick={act(() => void createNoteIn(client, qc, t.path))}>
                <FilePlus2 strokeWidth={1.75} /> New note in {t.name}
              </SheetItem>
              <SheetItem onClick={act(() => startNewFolder(t.path))}>
                <FolderPlus strokeWidth={1.75} /> New folder in {t.name}
              </SheetItem>
            </>
          )}
        </SheetContent>
      ) : null}
    </Sheet>
  );
}
