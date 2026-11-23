import { useVirtualizer } from "@tanstack/react-virtual";
import { Folder, FolderOpen } from "lucide-react";
import { createContext, useContext, useLayoutEffect, useMemo, useRef, useState } from "react";
import { withDraft } from "../lib/newFolder.ts";
import { buildTree, flattenTree } from "../lib/notes.ts";
import { useTree } from "../queries.ts";
import { useContributions } from "../registry.ts";
import { useUI } from "../state.ts";
import { FolderActions, NewFolderRow, useFolderDraft } from "./NewFolder.tsx";
import { NoteRow } from "./NoteRow.tsx";
import { Row } from "./Row.tsx";

/** The sidebar's scrolling element, which the tree virtualizes against. */
export const SidebarScroll = createContext<HTMLElement | null>(null);

const coarse = () => typeof matchMedia !== "undefined" && matchMedia("(pointer: coarse)").matches;

/** Folders with their notes nested, virtualized: 10,000 notes stay smooth. */
export function FolderTree() {
  const tree = useTree();
  const expanded = useUI((s) => s.expanded);
  const toggle = useUI((s) => s.toggle);
  const decorators = useContributions("treeDecorator");
  const scrollEl = useContext(SidebarScroll);
  const listRef = useRef<HTMLDivElement>(null);
  const [margin, setMargin] = useState(0);
  const rowHeight = coarse() ? 44 : 28;

  const root = useMemo(() => (tree.data ? buildTree(tree.data) : null), [tree.data]);
  const draft = useFolderDraft((s) => s.draft);
  const rows = useMemo(
    () =>
      withDraft(
        root ? flattenTree(root, (p) => !!expanded[`folder:${p}`], tree.data?.inbox) : [],
        draft,
      ),
    [root, expanded, tree.data?.inbox, draft],
  );

  // Where the list starts inside the scroller (sections above it vary).
  useLayoutEffect(() => {
    if (!scrollEl || !listRef.current) return;
    const top =
      listRef.current.getBoundingClientRect().top -
      scrollEl.getBoundingClientRect().top +
      scrollEl.scrollTop;
    if (Math.abs(top - margin) > 0.5) setMargin(top);
  });

  const v = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollEl,
    estimateSize: () => rowHeight,
    overscan: 16,
    scrollMargin: margin,
    getItemKey: (i) => {
      const r = rows[i]!;
      if (r.kind === "draft") return `d:${r.parent}`;
      return r.kind === "folder" ? `f:${r.node.path}` : `n:${r.note.path}`;
    },
  });

  if (!root) return null;
  if (rows.length === 0) return <p className="px-2 py-1 text-small text-faint">No folders yet</p>;

  return (
    <div
      ref={listRef}
      role="tree"
      aria-label="Folders"
      style={{ height: v.getTotalSize(), position: "relative" }}
    >
      {v.getVirtualItems().map((item) => {
        const row = rows[item.index]!;
        return (
          <div
            key={item.key}
            role="treeitem"
            aria-level={row.depth + 1}
            aria-expanded={row.kind === "folder" ? row.open : undefined}
            aria-label={row.kind === "draft" ? "New folder" : undefined}
            className="group/item"
            style={{
              position: "absolute",
              top: 0,
              left: 0,
              right: 0,
              height: rowHeight,
              transform: `translateY(${item.start - margin}px)`,
            }}
          >
            {row.kind === "draft" ? (
              <NewFolderRow parent={row.parent} depth={row.depth} />
            ) : row.kind === "folder" ? (
              <>
                <Row
                  chevron={row.open}
                  icon={
                    row.open ? <FolderOpen strokeWidth={1.75} /> : <Folder strokeWidth={1.75} />
                  }
                  label={row.node.name}
                  // Room for the row's buttons, which are always shown under a finger.
                  className="pointer-coarse:pr-[88px]"
                  depth={row.depth}
                  onClick={() => toggle(`folder:${row.node.path}`)}
                  count={
                    decorators
                      .map((d) =>
                        d.decorate({
                          kind: "folder",
                          path: row.node.path,
                          name: row.node.name,
                          notes: row.node.count,
                        }),
                      )
                      .find((d) => d?.badge !== undefined)?.badge
                  }
                />
                <FolderActions path={row.node.path} name={row.node.name} />
              </>
            ) : (
              // Notes line up under their folder's name, past the chevron.
              <NoteRow
                note={row.note}
                depth={row.depth + (row.depth > 0 || row.note.folder !== "" ? 1 : 0)}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}
