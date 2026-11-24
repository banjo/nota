import { useQueryClient } from "@tanstack/react-query";
import { FileText, X } from "lucide-react";
import { useClient } from "../context.ts";
import { closePreview, useOpened } from "../external.ts";
import { displayPath } from "../lib/opened.ts";
import { useUI } from "../state.ts";
import { Row, SectionHeader } from "./Row.tsx";

/** "Opened files": the previews of outside files, while there are any. */
export function OpenedSection() {
  const client = useClient();
  const qc = useQueryClient();
  const previews = useOpened((s) => s.previews);
  const drafts = useOpened((s) => s.drafts);
  const route = useUI((s) => s.route);
  const navigate = useUI((s) => s.navigate);
  if (!previews.length) return null;
  return (
    <div className="flex flex-col">
      <SectionHeader>Opened files</SectionHeader>
      {previews.map((p) => (
        <div key={p.id} className="group/opened relative">
          <Row
            icon={<FileText strokeWidth={1.75} />}
            label={p.name}
            title={displayPath(p)}
            active={route.kind === "external" && route.id === p.id}
            className="pr-8 pointer-coarse:pr-12"
            onClick={() => navigate({ kind: "external", id: p.id })}
            trailing={
              drafts[p.id] !== undefined ? (
                <span aria-label="Draft" className="size-1.5 shrink-0 rounded-full bg-ink" />
              ) : null
            }
          />
          <button
            type="button"
            aria-label={`Close ${p.name}`}
            onClick={() => closePreview(client, qc, p.id)}
            className="absolute top-1/2 right-1 grid size-6 -translate-y-1/2 place-items-center rounded-sm text-faint opacity-0 outline-none group-hover/opened:opacity-100 hover:bg-hover hover:text-foreground focus-visible:opacity-100 pointer-coarse:size-11 pointer-coarse:opacity-100"
          >
            <X className="size-3.5" strokeWidth={2} />
          </button>
        </div>
      ))}
    </div>
  );
}
