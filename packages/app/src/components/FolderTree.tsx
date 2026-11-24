import { useVirtualizer } from "@tanstack/react-virtual";
import { Folder, FolderOpen } from "lucide-react";
import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useLongPress } from "../lib/longPress.ts";
import { withDraft } from "../lib/newFolder.ts";
import { buildTree, flattenTree } from "../lib/notes.ts";
import { useTree } from "../queries.ts";
import { useContributions } from "../registry.ts";
import { type Route, useUI } from "../state.ts";
import { FolderActions, NewFolderRow, useFolderDraft } from "./NewFolder.tsx";
import { NoteRow } from "./NoteRow.tsx";
import { Row } from "./Row.tsx";
import { openRowSheet } from "./RowSheet.tsx";

/** A folder row, its hover buttons, and a long press for its sheet under a finger. */
function FolderRow({
  path,
  name,
  open,
  depth,
  count,
  onToggle,
}: {
  path: string;
  name: string;
  open: boolean;
  depth: number;
  count?: ReactNode;
  onToggle: () => void;
}) {
  const longPress = useLongPress(() => openRowSheet({ kind: "folder", path, name }));
  return (
    <>
      <Row
        chevron={open}
        icon={open ? <FolderOpen strokeWidth={1.75} /> : <Folder strokeWidth={1.75} />}
        label={name}
        // Room for the row's "…", which is always shown under a finger.
        className="pointer-coarse:pr-12"
        depth={depth}
        onClick={onToggle}
        count={count}
        {...longPress}
      />
      <FolderActions path={path} name={name} />
    </>
  );
}

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

  // A note opened from the file manager: once its folders are open,
  // scroll its row into view (once per open).
  const route = useUI((s) => s.route);
  const scrolled = useRef<Route | null>(null);
  useEffect(() => {
    if (!scrollEl || route.kind !== "note" || route.via !== "open") return;
    if (scrolled.current === route) return;
    const i = rows.findIndex((r) => r.kind === "note" && r.note.path === route.path);
    if (i < 0) return;
    // Rows have a fixed height, so the offset is known without measuring;
    // after a frame, once the expanded folders have laid out.
    const frame = requestAnimationFrame(() => {
      scrolled.current = route;
      const top = margin + i * rowHeight;
      const view = scrollEl.clientHeight;
      if (top < scrollEl.scrollTop || top + rowHeight > scrollEl.scrollTop + view)
        scrollEl.scrollTo({ top: Math.max(0, top - view / 2 + rowHeight / 2) });
    });
    return () => cancelAnimationFrame(frame);
  }, [route, rows, scrollEl, margin, rowHeight]);

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
              // The draft row's error hangs over the rows below it.
              zIndex: row.kind === "draft" ? 1 : undefined,
            }}
          >
            {row.kind === "draft" ? (
              <NewFolderRow parent={row.parent} depth={row.depth} />
            ) : row.kind === "folder" ? (
              <FolderRow
                path={row.node.path}
                name={row.node.name}
                open={row.open}
                depth={row.depth}
                onToggle={() => toggle(`folder:${row.node.path}`)}
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
