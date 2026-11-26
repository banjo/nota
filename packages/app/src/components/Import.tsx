import type { ImportItem, ImportReport } from "@nota/client";
import { Button } from "@nota/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@nota/ui/dialog";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  FileInput,
  FolderInput,
  FolderOpen,
  LoaderCircle,
  type LucideIcon,
  NotebookPen,
} from "lucide-react";
import { useRef, useState } from "react";
import { create } from "zustand";
import { fail } from "../actions.ts";
import { useClient } from "../context.ts";
import { useUI } from "../state.ts";

// Importing notes from elsewhere: a folder of Markdown (an Obsidian
// vault), a zip (Bear, Notion, Logseq exports) or Apple Notes. Settings
// and ⌘K open it. Everything goes into a new folder, and the dialog
// ends with what happened to each file.

export const useImportDialog = create<{ open: boolean }>(() => ({ open: false }));
export const openImport = () => useImportDialog.setState({ open: true });
const close = () => useImportDialog.setState({ open: false });

type Source = "folder" | "zip" | "apple-notes";

interface Option {
  id: Source;
  icon: LucideIcon;
  label: string;
  detail: string;
}

export function ImportDialog() {
  const open = useImportDialog((s) => s.open);
  // A new body each time it opens: no report from last time.
  const [round, setRound] = useState(0);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setRound((r) => r + 1);
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent showClose className="max-w-[480px]">
        <ImportBody key={round} />
      </DialogContent>
    </Dialog>
  );
}

function ImportBody() {
  const client = useClient();
  const queryClient = useQueryClient();
  const api = client.import;
  const folderInput = useRef<HTMLInputElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const appleNotes = useQuery({
    queryKey: ["import", "apple-notes"],
    queryFn: () => api!.appleNotes!.supported(),
    enabled: !!api?.appleNotes,
    staleTime: Infinity,
  });

  const run = useMutation({
    mutationFn: async (job: { source: Source; files?: File[] }) => {
      if (job.files) return api!.upload!(job.files);
      if (job.source === "apple-notes") return api!.appleNotes!.run();
      return api!.pick!(job.source);
    },
    onSuccess: (r) => {
      if (r) void queryClient.invalidateQueries();
    },
    onError: (e) => fail("Couldn't import", e),
  });

  const options: Option[] = [
    {
      id: "folder",
      icon: FolderInput,
      label: "Folder of Markdown notes",
      detail: "An Obsidian vault, or any folder of .md files with their images.",
    },
    {
      id: "zip",
      icon: api?.upload ? FileInput : Archive,
      label: api?.upload ? "Zip or Markdown files" : "Zip",
      detail: "An export from Bear, Notion, Logseq or another app.",
    },
  ];
  if (appleNotes.data)
    options.push({
      id: "apple-notes",
      icon: NotebookPen,
      label: "Apple Notes",
      detail: "Every note with its folders and images. macOS asks you to allow it.",
    });

  const start = (source: Source) => {
    if (api?.upload) {
      (source === "folder" ? folderInput : fileInput).current?.click();
      return;
    }
    run.mutate({ source });
  };
  const picked = (source: Source, list: FileList | null) => {
    const files = [...(list ?? [])];
    if (files.length) run.mutate({ source, files });
  };

  if (run.data) return <Done report={run.data} />;
  const working = run.isPending;
  return (
    <>
      <DialogHeader className="pr-12">
        <DialogTitle className="flex items-center gap-2">
          <FolderInput className="size-4 text-faint" strokeWidth={1.75} aria-hidden />
          Import notes
        </DialogTitle>
        <DialogDescription>
          Notes go into a new folder. Nothing in your vault is replaced.
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1 px-5 pt-4 pb-5">
        {options.map((o) => (
          <button
            key={o.id}
            type="button"
            disabled={working}
            onClick={() => start(o.id)}
            className="flex items-center gap-3 rounded-lg px-2.5 py-2 text-left outline-none transition-colors duration-150 hover:bg-hover focus-visible:ring-2 focus-visible:ring-ink/40 disabled:pointer-events-none"
          >
            <span className="grid size-8 shrink-0 place-items-center rounded-md bg-chip text-muted-foreground">
              {working && run.variables?.source === o.id ? (
                <LoaderCircle className="size-4 animate-spin" aria-hidden />
              ) : (
                <o.icon className="size-4" strokeWidth={1.75} aria-hidden />
              )}
            </span>
            <span className="flex min-w-0 flex-col">
              <span className="text-ui text-foreground">{o.label}</span>
              <span className="text-small text-muted-foreground">
                {working && run.variables?.source === o.id
                  ? o.id === "apple-notes"
                    ? "Reading Apple Notes…"
                    : "Importing…"
                  : o.detail}
              </span>
            </span>
          </button>
        ))}
        {api?.upload ? (
          <>
            <input
              ref={folderInput}
              type="file"
              hidden
              multiple
              // @ts-expect-error: not in React's types, in every browser
              webkitdirectory=""
              onChange={(e) => picked("folder", e.currentTarget.files)}
            />
            <input
              ref={fileInput}
              type="file"
              hidden
              multiple
              accept=".zip,.md,.markdown,application/zip,text/markdown"
              onChange={(e) => picked("zip", e.currentTarget.files)}
            />
          </>
        ) : null}
      </div>
    </>
  );
}

function Done({ report }: { report: ImportReport }) {
  const navigate = useUI((s) => s.navigate);
  const toggle = useUI((s) => s.toggle);
  const where = report.folder || "your vault";
  const renamed = report.items.filter((i) => i.status === "renamed");
  const skipped = report.items.filter((i) => i.status === "skipped");
  const remarks = report.items.filter((i) => i.detail);
  const what =
    count(report.notes, "note") + (report.files ? ` and ${count(report.files, "file")}` : "");
  const openFolder = () => {
    close();
    const parts = report.folder.split("/");
    parts.forEach((_, i) => toggle(`folder:${parts.slice(0, i + 1).join("/")}`, true));
    navigate({ kind: "list", list: { type: "folder", path: report.folder } });
  };
  return (
    <>
      <DialogHeader className="pr-12">
        <DialogTitle className="flex items-center gap-2">
          <FolderInput className="size-4 text-faint" strokeWidth={1.75} aria-hidden />
          {report.notes + report.files ? "Imported" : "Nothing imported"}
        </DialogTitle>
        <DialogDescription>
          {report.notes + report.files ? (
            <>
              {what} {report.notes + report.files === 1 ? "is" : "are"} in{" "}
              <span className="text-foreground">{where}</span>.
              {report.links
                ? ` ${count(report.links, "link")} ${report.links === 1 ? "was" : "were"} updated to the new names.`
                : ""}
            </>
          ) : (
            "There were no files Nota could import."
          )}
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-4 px-5 pt-4 pb-5">
        {renamed.length + skipped.length + remarks.length ? (
          <div className="flex max-h-64 flex-col gap-3 overflow-y-auto rounded-lg bg-chip/50 px-3 py-2.5">
            <Items title="Renamed" items={renamed} />
            <Items title="Left out" items={skipped} />
            <Items title="Good to know" items={remarks} remark />
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={close}>
            Done
          </Button>
          {report.notes && report.folder ? (
            <Button onClick={openFolder}>
              <FolderOpen /> Open folder
            </Button>
          ) : null}
        </div>
      </div>
    </>
  );
}

function Items({ title, items, remark }: { title: string; items: ImportItem[]; remark?: boolean }) {
  if (!items.length) return null;
  return (
    <section>
      <h3 className="mb-1 text-small font-medium text-foreground">
        {title} <span className="text-faint">{items.length}</span>
      </h3>
      <ul className="flex flex-col gap-1">
        {items.map((i) => (
          <li key={i.source + (i.path ?? "")} className="text-small text-muted-foreground">
            <span className="break-all text-text-soft">
              {remark ? i.path : i.path ? `${i.source} → ${i.path}` : i.source}
            </span>
            <span className="text-faint"> · {remark ? i.detail : i.reason}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

function count(n: number, word: string) {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}
