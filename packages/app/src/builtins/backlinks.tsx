import type { Note } from "@nota/client";
import { Link2 } from "lucide-react";
import { folderLabel } from "../lib/notes.ts";
import { useTree } from "../queries.ts";
import { useUI } from "../state.ts";

/** "Linked from": the notes that link here (a built-in panel). */
export function BacklinksPanel({ note }: { note: Note }) {
  const tree = useTree();
  const navigate = useUI((s) => s.navigate);
  const from = [...new Set(note.backlinks.map((l) => l.from))].filter((p) => p !== note.path);
  const byPath = new Map((tree.data?.notes ?? []).map((n) => [n.path, n]));
  return (
    <section aria-labelledby="backlinks-title" className="mt-11 border-t border-divider pt-[18px]">
      <h2 id="backlinks-title" className="mb-2 text-[12.5px] font-semibold text-muted-foreground">
        Linked from
      </h2>
      {from.length === 0 ? (
        <p className="text-ui text-faint">
          No notes link here yet. Link one with [[{note.title}]].
        </p>
      ) : (
        <ul className="flex flex-col">
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
      )}
    </section>
  );
}
