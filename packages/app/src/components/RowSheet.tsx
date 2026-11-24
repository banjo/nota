import { Sheet, SheetContent, SheetItem, SheetSeparator } from "@nota/ui/sheet";
import { cn } from "@nota/ui/utils";
import { Fragment, useRef } from "react";
import { create } from "zustand";
import { useNoteMode, useUI } from "../state.ts";
import { MODES } from "./ModeSwitch.tsx";
import { type MenuTarget, useRowActions } from "./RowMenu.tsx";

// The touch twin of the tree's hover buttons and "…" menus: one bottom
// sheet for a note or a folder, opened by a row's "…" under a finger, a
// long press on a row, or the note page's "…" on a phone. Every action
// in it is also a ⌘K command (note.addToFolder, note.delete,
// note.newInFolder, note.rename, folder.rename, folder.add, view.mode.*),
// and the list comes from RowMenu, which the right-click menu uses too.

export type RowTarget =
  | { kind: "note"; path: string; title: string; page?: boolean }
  | { kind: "folder"; path: string; name: string }
  | { kind: "empty" };

export const useRowSheet = create<{ target: RowTarget | null }>(() => ({ target: null }));
export const openRowSheet = (target: RowTarget) => useRowSheet.setState({ target });
const close = () => useRowSheet.setState({ target: null });

/** Live / Reading / Source as one segmented control, sized for a thumb. */
function ModeSegments() {
  const mode = useNoteMode();
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
  const actionsFor = useRowActions();
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
          title={
            t.kind === "note" ? t.title || "Untitled" : t.kind === "folder" ? t.name : "Folders"
          }
          onOpenAutoFocus={(e) => {
            acted.current = false;
            // No focus ring on the first item for a finger.
            e.preventDefault();
          }}
          onCloseAutoFocus={(e) => {
            if (acted.current) e.preventDefault();
          }}
        >
          {t.kind === "note" && t.page ? <ModeSegments /> : null}
          {actionsFor(t satisfies MenuTarget).map((a, i, all) => (
            <Fragment key={a.id}>
              {i > 0 && all[i - 1]!.group !== a.group ? <SheetSeparator /> : null}
              <SheetItem variant={a.destructive ? "destructive" : undefined} onClick={act(a.run)}>
                <a.icon strokeWidth={1.75} /> {a.label}
              </SheetItem>
            </Fragment>
          ))}
        </SheetContent>
      ) : null}
    </Sheet>
  );
}
