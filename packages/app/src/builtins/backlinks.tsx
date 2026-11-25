import type { Note } from "@nota/client";
import { Link2 } from "lucide-react";
import { SectionChip } from "../components/SectionChip.tsx";
import { backlinkSources } from "../lib/backlinksPref.ts";
import { folderLabel } from "../lib/notes.ts";
import { useTree } from "../queries.ts";
import { useUI } from "../state.ts";

/**
 * "Linked from": the notes that link here (a built-in panel). A chip with
 * the count, like Properties, and the list only when the device preference
 * says show. Nothing at all when no note links here.
 */
export function BacklinksPanel({ note }: { note: Note }) {
  const tree = useTree();
  const navigate = useUI((s) => s.navigate);
  const shown = useUI((s) => s.backlinks === "show");
  const toggle = useUI((s) => s.toggleBacklinks);
  const from = backlinkSources(note.backlinks, note.path);
  if (from.length === 0) return null;
  const byPath = new Map((tree.data?.notes ?? []).map((n) => [n.path, n]));
  return (
    <section aria-label="Linked from" className="mt-11 border-t border-divider pt-3">
      <SectionChip
        label="Linked from"
        icon={Link2}
        shown={shown}
        count={from.length}
        onToggle={toggle}
        controls="backlinks-list"
      />
      {shown ? (
        <ul id="backlinks-list" className="mt-1 flex flex-col">
          {from.map((p) => {
            const n = byPath.get(p);
            return (
              <li key={p}>
                <button
                  type="button"
                  onClick={() => navigate({ kind: "note", path: p })}
                  className="group flex min-h-8 w-full items-center gap-2 rounded-sm py-1.5 text-left text-[14px] text-text-soft outline-none hover:text-ink focus-visible:text-ink pointer-coarse:min-h-11"
                >
                  <Link2
                    className="size-3.5 shrink-0 text-faint group-hover:text-ink"
                    strokeWidth={1.75}
                  />
                  <span className="truncate">{n?.title ?? p}</span>
                  <small className="ml-1.5 shrink-0 text-small text-faint">
                    {folderLabel(n?.folder ?? "")}
                  </small>
                </button>
              </li>
            );
          })}
        </ul>
      ) : null}
    </section>
  );
}
