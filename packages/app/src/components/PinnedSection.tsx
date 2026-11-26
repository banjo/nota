import type { Tree } from "@nota/client";
import { cn } from "@nota/ui/utils";
import { useQueryClient } from "@tanstack/react-query";
import { type DragEvent, useState } from "react";
import { movePinned } from "../actions.ts";
import { useClient } from "../context.ts";
import { pinnedNotes } from "../lib/pinned.ts";
import { useCoarse } from "../lib/viewport.ts";
import { useTreeValue } from "../queries.ts";
import { useUI } from "../state.ts";
import { NoteRow } from "./NoteRow.tsx";
import { SectionHeader } from "./Row.tsx";

const pinnedOf = (t: Tree) => pinnedNotes(t.notes);
const DRAG_TYPE = "application/x-nota-pinned";

/** "Pinned": the pinned notes in their order; drag one to move it. */
export function PinnedSection() {
  const client = useClient();
  const qc = useQueryClient();
  const pinned = useTreeValue(pinnedOf);
  const open = useUI((s) => s.expanded["section:pinned"] !== false);
  const toggle = useUI((s) => s.toggle);
  const coarse = useCoarse();
  const [dragging, setDragging] = useState<string | null>(null);
  // Where the note would land: before the pinned note at this index.
  const [target, setTarget] = useState<number | null>(null);
  if (!pinned?.length) return null;

  const end = () => {
    setDragging(null);
    setTarget(null);
  };
  const over = (e: DragEvent<HTMLDivElement>, i: number) => {
    if (!e.dataTransfer.types.includes(DRAG_TYPE)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    const r = e.currentTarget.getBoundingClientRect();
    setTarget(e.clientY < r.top + r.height / 2 ? i : i + 1);
  };
  const drop = (e: DragEvent<HTMLDivElement>) => {
    const path = e.dataTransfer.getData(DRAG_TYPE);
    if (!path || target === null) return end();
    e.preventDefault();
    const from = pinned.findIndex((n) => n.path === path);
    // The index among the other pinned notes, which is what moving counts.
    void movePinned(client, qc, path, from !== -1 && from < target ? target - 1 : target);
    end();
  };

  return (
    <div className="flex flex-col">
      <SectionHeader open={open} onClick={() => toggle("section:pinned", !open)}>
        Pinned
      </SectionHeader>
      {open
        ? pinned.map((n, i) => (
            <div
              key={n.path}
              draggable={!coarse}
              onDragStart={(e) => {
                e.dataTransfer.setData(DRAG_TYPE, n.path);
                e.dataTransfer.effectAllowed = "move";
                setDragging(n.path);
              }}
              onDragOver={(e) => over(e, i)}
              onDragLeave={(e) => {
                if (!e.currentTarget.parentElement?.contains(e.relatedTarget as Node))
                  setTarget(null);
              }}
              onDrop={drop}
              onDragEnd={end}
              className="relative"
            >
              {target === i || (target === pinned.length && i === pinned.length - 1) ? (
                <span
                  aria-hidden
                  className={cn(
                    "pointer-events-none absolute inset-x-2 z-10 h-0.5 rounded-full bg-ink",
                    target === i ? "-top-px" : "-bottom-px",
                  )}
                />
              ) : null}
              <NoteRow note={n} depth={0} className={cn(dragging === n.path && "opacity-50")} />
            </div>
          ))
        : null}
    </div>
  );
}
