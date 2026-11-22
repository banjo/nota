import type { ListQuery, NoteInfo } from "@nota/client";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Clock, Folder, Hash, Layers } from "lucide-react";
import { useRef } from "react";
import { relativeTime } from "../lib/format.ts";
import { folderLabel } from "../lib/notes.ts";
import { useList } from "../queries.ts";
import { type ListSpec, useUI } from "../state.ts";
import { EmptyState } from "./EmptyState.tsx";
import { TopBar } from "./TopBar.tsx";

function query(list: ListSpec): ListQuery {
  switch (list.type) {
    case "recent":
      return { sort: "modified desc", limit: 50 };
    case "tag":
      return { tag: list.tag, sort: "modified desc" };
    case "view":
      return { view: list.id };
    case "folder":
      return { folder: list.path, sort: "title" };
  }
}

function heading(list: ListSpec): string {
  switch (list.type) {
    case "recent":
      return "Recent";
    case "tag":
      return `#${list.tag}`;
    case "view":
      return list.name;
    case "folder":
      return list.path.split("/").pop() ?? list.path;
  }
}

const ICONS = { recent: Clock, tag: Hash, view: Layers, folder: Folder };

/** A list of notes: Recent, a tag, a saved view or a folder. Virtualized. */
export function NoteList({ list }: { list: ListSpec }) {
  const notes = useList(query(list));
  const navigate = useUI((s) => s.navigate);
  const scroller = useRef<HTMLDivElement>(null);
  const rows: NoteInfo[] = notes.data ?? [];
  const v = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => 58,
    overscan: 10,
  });
  const title = heading(list);

  return (
    <div ref={scroller} className="h-full overflow-y-auto">
      <TopBar
        crumbs={[{ label: title }]}
        right={notes.data ? `${rows.length} ${rows.length === 1 ? "note" : "notes"}` : undefined}
      />
      <div className="mx-auto w-full max-w-[740px] px-14 pt-8 pb-24 max-[720px]:px-3 max-[720px]:pt-3">
        <h1 className="mb-4 px-3 text-[26px] font-bold tracking-[-0.015em] text-foreground max-[720px]:hidden">
          {title}
        </h1>
        {notes.data && rows.length === 0 ? (
          <EmptyState
            icon={ICONS[list.type]}
            title="Nothing here yet"
            body={
              list.type === "tag"
                ? `Add #${list.tag} to a note and it shows up here.`
                : "Notes you write show up here."
            }
          />
        ) : (
          <ul style={{ height: v.getTotalSize(), position: "relative" }}>
            {v.getVirtualItems().map((item) => {
              const n = rows[item.index]!;
              return (
                <li
                  key={n.path}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    right: 0,
                    transform: `translateY(${item.start}px)`,
                  }}
                  ref={v.measureElement}
                  data-index={item.index}
                >
                  <button
                    type="button"
                    onClick={() => navigate({ kind: "note", path: n.path })}
                    className="flex w-full flex-col gap-0.5 rounded-lg px-3 py-2.5 text-left outline-none hover:bg-hover focus-visible:bg-hover pointer-coarse:py-3"
                  >
                    <span className="truncate text-[14px] font-medium text-foreground">
                      {n.title}
                    </span>
                    <span className="flex gap-2 text-small text-faint">
                      <span className="truncate">{folderLabel(n.folder)}</span>
                      <span className="shrink-0">{relativeTime(n.modified)}</span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
