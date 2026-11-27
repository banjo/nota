import type { Attachment } from "@nota/client";
import { Button } from "@nota/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@nota/ui/dialog";
import { toast } from "@nota/ui/sonner";
import { cn } from "@nota/ui/utils";
import { useQuery } from "@tanstack/react-query";
import { Copy, FileText, LoaderCircle, Paperclip } from "lucide-react";
import { useState } from "react";
import { useClient } from "../context.ts";
import { relativeTime } from "../lib/format.ts";
import { keys, useTreeValue } from "../queries.ts";
import { useUI } from "../state.ts";
import { EmptyState } from "./EmptyState.tsx";
import { TopBar } from "./TopBar.tsx";

// The attachments folder as what it is: the images (and other files)
// notes show, each with the notes that use it, rather than a folder of
// notes that looks empty.

const title = (path: string) => path.split("/").pop()!.replace(/\.md$/i, "");

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

export function AttachmentsView() {
  const client = useClient();
  const folder = useTreeValue((t) => t.attachments ?? "") || "Attachments";
  const list = useQuery({
    queryKey: keys.attachments,
    queryFn: () => client.attachments!(),
    enabled: !!client.attachments,
  });
  const [unused, setUnused] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const files = list.data?.files ?? [];
  const unusedCount = files.filter((f) => f.usedBy.length === 0).length;
  const shown = unused ? files.filter((f) => f.usedBy.length === 0) : files;
  const picked = files.find((f) => f.path === open) ?? null;

  return (
    <div className="h-full overflow-y-auto overscroll-contain">
      <TopBar
        crumbs={[{ label: "Attachments" }]}
        right={list.data ? `${files.length} ${files.length === 1 ? "file" : "files"}` : undefined}
      />
      <div className="mx-auto w-full max-w-[960px] px-14 pt-8 pb-24 max-[720px]:px-3 max-[720px]:pt-3">
        <div className="mb-5 flex flex-wrap items-end justify-between gap-3 px-1">
          <div className="min-w-0">
            <h1 className="text-[26px] font-bold tracking-[-0.015em] text-foreground max-[720px]:hidden">
              Attachments
            </h1>
            <p className="text-ui text-muted-foreground">
              Images you paste or drop into a note, in{" "}
              <span className="text-foreground">{folder}/</span>.
            </p>
          </div>
          {unusedCount > 0 ? (
            <div
              role="radiogroup"
              aria-label="Show"
              className="flex gap-1 rounded-lg bg-chip p-0.5"
            >
              {[
                { on: false, label: "All", n: files.length },
                { on: true, label: "Not used", n: unusedCount },
              ].map((o) => (
                <button
                  key={o.label}
                  type="button"
                  role="radio"
                  aria-checked={unused === o.on}
                  onClick={() => setUnused(o.on)}
                  className={cn(
                    "rounded-md px-2.5 py-1 text-small outline-none transition-colors duration-150 focus-visible:ring-2 focus-visible:ring-ink/40 pointer-coarse:py-2",
                    unused === o.on
                      ? "bg-surface text-foreground shadow-card"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {o.label} <span className="text-faint tabular-nums">{o.n}</span>
                </button>
              ))}
            </div>
          ) : null}
        </div>

        {list.isPending ? (
          <div className="grid place-items-center py-24 text-faint">
            <LoaderCircle className="size-5 animate-spin" aria-label="Loading" />
          </div>
        ) : list.isError ? (
          <EmptyState
            icon={Paperclip}
            title="Couldn't load the attachments"
            body={String(list.error)}
          />
        ) : files.length === 0 ? (
          <EmptyState
            icon={Paperclip}
            title="No attachments yet"
            body="Paste or drop an image into a note and it's saved here."
          />
        ) : (
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(168px,1fr))] gap-3 max-[720px]:grid-cols-2">
            {shown.map((f) => (
              <li key={f.path}>
                <Card file={f} onOpen={() => setOpen(f.path)} />
              </li>
            ))}
          </ul>
        )}
      </div>
      <Dialog open={!!picked} onOpenChange={(o) => !o && setOpen(null)}>
        <DialogContent showClose className="max-w-[640px]">
          {picked ? <Details file={picked} onDone={() => setOpen(null)} /> : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Preview({ file, className }: { file: Attachment; className?: string }) {
  const client = useClient();
  const [broken, setBroken] = useState(false);
  if (file.image && client.fileUrl && !broken)
    return (
      <img
        src={client.fileUrl(file.path)}
        alt={file.name}
        loading="lazy"
        decoding="async"
        onError={() => setBroken(true)}
        className={className}
      />
    );
  const ext = file.name.includes(".") ? file.name.split(".").pop()!.toUpperCase() : "";
  return (
    <span className="flex flex-col items-center gap-1.5 text-faint">
      <FileText className="size-7" strokeWidth={1.5} aria-hidden />
      {ext ? <span className="text-[11px] font-medium tracking-wide">{ext}</span> : null}
    </span>
  );
}

function Card({ file, onOpen }: { file: Attachment; onOpen: () => void }) {
  const used = file.usedBy.length;
  return (
    <button
      type="button"
      onClick={onOpen}
      className="group flex w-full flex-col overflow-hidden rounded-lg bg-surface text-left shadow-card outline-none transition-shadow duration-150 hover:shadow-[0_0_0_1px_var(--hairline),0_2px_8px_rgb(0_0_0/0.06)] focus-visible:shadow-[0_0_0_1px_var(--ink),0_0_0_4px_var(--ink-soft)]"
    >
      <span className="grid aspect-[4/3] place-items-center overflow-hidden bg-chip/60">
        <Preview file={file} className="size-full object-cover" />
      </span>
      <span className="flex min-w-0 flex-col gap-0.5 px-2.5 py-2">
        <span className="truncate text-ui text-foreground">{file.name}</span>
        <span className="truncate text-small text-faint">
          {formatBytes(file.size)} ·{" "}
          {used ? (
            `${used} ${used === 1 ? "note" : "notes"}`
          ) : (
            <span className="text-warning">Not used</span>
          )}
        </span>
      </span>
    </button>
  );
}

function Details({ file, onDone }: { file: Attachment; onDone: () => void }) {
  const navigate = useUI((s) => s.navigate);
  const embed = `![[${file.name}]]`;
  const copy = async () => {
    await navigator.clipboard.writeText(embed);
    toast.success("Copied", { description: embed });
  };
  return (
    <>
      <DialogHeader className="pr-12">
        <DialogTitle className="truncate">{file.name}</DialogTitle>
        <DialogDescription>
          {formatBytes(file.size)} · {relativeTime(file.modified)} · {file.path}
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-4 px-5 pt-4 pb-5">
        <div className="grid max-h-[52vh] min-h-32 place-items-center overflow-hidden rounded-lg bg-chip/60">
          <Preview file={file} className="max-h-[52vh] w-auto max-w-full object-contain" />
        </div>
        <section>
          <h3 className="mb-1 text-small font-medium text-foreground">
            {file.usedBy.length ? "Used in" : "Not used in any note"}
          </h3>
          {file.usedBy.length ? (
            <ul className="flex max-h-40 flex-col overflow-y-auto">
              {file.usedBy.map((p) => (
                <li key={p}>
                  <button
                    type="button"
                    onClick={() => {
                      onDone();
                      navigate({ kind: "note", path: p });
                    }}
                    className="flex w-full items-baseline gap-2 rounded-sm px-2 py-1.5 text-left outline-none hover:bg-hover focus-visible:bg-hover"
                  >
                    <span className="truncate text-ui text-foreground">{title(p)}</span>
                    <span className="truncate text-small text-faint">{p}</span>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-small text-muted-foreground">
              No note embeds or links to it. Embed it with {embed}.
            </p>
          )}
        </section>
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => void copy()}>
            <Copy /> Copy embed
          </Button>
        </div>
      </div>
    </>
  );
}
