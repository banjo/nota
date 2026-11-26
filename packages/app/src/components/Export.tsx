import type { ExportFormat, NotesClient } from "@nota/client";
import { Button } from "@nota/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@nota/ui/dialog";
import { cn } from "@nota/ui/utils";
import { toast } from "@nota/ui/sonner";
import { useMutation } from "@tanstack/react-query";
import {
  Archive,
  Code2,
  FileDown,
  FileText,
  Globe,
  LoaderCircle,
  type LucideIcon,
  Printer,
} from "lucide-react";
import { useState } from "react";
import { create } from "zustand";
import { fail } from "../actions.ts";
import { useClient } from "../context.ts";
import { currentShareTheme, printTheme } from "../lib/share.ts";
import { useNote } from "../queries.ts";

// Exporting a note, a folder or the vault: a web page, a PDF, Markdown
// for other apps, or the files as they are. The note's and the folder's
// menus and ⌘K open it; ⌘P prints the open note.

export type ExportTarget = { kind: "note" | "folder"; path: string } | { kind: "vault" };

export const useExportDialog = create<{ target: ExportTarget | null }>(() => ({ target: null }));
export const openExport = (target: ExportTarget) => useExportDialog.setState({ target });
const close = () => useExportDialog.setState({ target: null });

type Choice = ExportFormat | "pdf";

interface Option {
  id: Choice;
  icon: LucideIcon;
  label: string;
  detail: string;
}

export function ExportDialog() {
  const target = useExportDialog((s) => s.target);
  // Keep the content while the dialog closes.
  const [last, setLast] = useState(target);
  if (target && target !== last) setLast(target);
  return (
    <Dialog open={!!target} onOpenChange={(o) => !o && close()}>
      <DialogContent showClose className="max-w-[460px]">
        {last ? (
          <ExportBody
            key={last.kind === "vault" ? "" : `${last.kind}:${last.path}`}
            target={last}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function ExportBody({ target }: { target: ExportTarget }) {
  const client = useClient();
  const path = target.kind === "vault" ? "" : target.path;
  const note = useNote(target.kind === "note" ? target.path : undefined);
  const base = path.split("/").pop() ?? "";
  const name =
    target.kind === "vault"
      ? "your notes"
      : target.kind === "note"
        ? note.data?.title || base.replace(/\.md$/, "")
        : base;
  const options = exportOptions(target.kind === "note", !!client.export?.pdf);
  const [choice, setChoice] = useState<Choice>(options[0]!.id);
  const prints = choice === "pdf" && !client.export?.pdf;

  const run = useMutation({
    mutationFn: async (c: Choice) => {
      const api = client.export!;
      if (c === "pdf") {
        if (api.pdf) return api.pdf(path, printTheme());
        await api.print(path, printTheme());
        return null;
      }
      return api.save({
        path,
        format: c,
        theme: c === "html" ? currentShareTheme() : undefined,
      });
    },
    onSuccess: (where, c) => {
      if (c === "pdf" && !client.export?.pdf) {
        close();
        return;
      }
      if (!where) return; // the save dialog was cancelled
      close();
      toast("Exported", { description: where.split(/[\\/]/).pop() });
    },
    onError: (e) => fail("Couldn't export", e),
  });

  return (
    <>
      <DialogHeader className="pr-12">
        <DialogTitle className="flex items-center gap-2">
          <FileDown className="size-4 text-faint" strokeWidth={1.75} aria-hidden />
          <span className="truncate">Export {name}</span>
        </DialogTitle>
        <DialogDescription>
          {target.kind === "note"
            ? "Save a copy of this note to send or keep elsewhere."
            : target.kind === "folder"
              ? "Save the notes in this folder, with their images, as a zip."
              : "Save every note in the vault, with their images, as a zip."}
        </DialogDescription>
      </DialogHeader>
      <form
        className="flex flex-col gap-4 px-5 pt-4 pb-5"
        onSubmit={(e) => {
          e.preventDefault();
          run.mutate(choice);
        }}
      >
        <div role="radiogroup" aria-label="Format" className="flex flex-col gap-1">
          {options.map((o) => {
            const on = o.id === choice;
            return (
              <button
                key={o.id}
                type="button"
                role="radio"
                aria-checked={on}
                onClick={() => setChoice(o.id)}
                onDoubleClick={() => run.mutate(o.id)}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-2.5 py-2 text-left outline-none transition-colors duration-150 focus-visible:ring-2 focus-visible:ring-ink/40",
                  on ? "bg-selected" : "hover:bg-hover",
                )}
              >
                <span
                  className={cn(
                    "grid size-8 shrink-0 place-items-center rounded-md transition-colors duration-150",
                    on ? "bg-ink text-ink-foreground" : "bg-chip text-muted-foreground",
                  )}
                >
                  <o.icon className="size-4" strokeWidth={1.75} aria-hidden />
                </span>
                <span className="flex min-w-0 flex-col">
                  <span className="text-ui text-foreground">{o.label}</span>
                  <span className="text-small text-muted-foreground">{o.detail}</span>
                </span>
              </button>
            );
          })}
        </div>
        <p className="text-small text-muted-foreground">
          Links to notes that aren't part of the export become plain text.
        </p>
        <div className="flex justify-end">
          <Button type="submit" disabled={run.isPending}>
            {run.isPending ? (
              <LoaderCircle className="animate-spin" />
            ) : prints ? (
              <Printer />
            ) : (
              <FileDown />
            )}
            {prints ? "Print…" : "Export…"}
          </Button>
        </div>
      </form>
    </>
  );
}

function exportOptions(note: boolean, pdfFile: boolean): Option[] {
  if (!note)
    return [
      {
        id: "html",
        icon: Globe,
        label: "Web pages",
        detail: "A page per note that opens in any browser, linked to each other.",
      },
      {
        id: "md",
        icon: Code2,
        label: "Markdown",
        detail: "For other apps: [[links]] become regular links.",
      },
      {
        id: "zip",
        icon: Archive,
        label: "Original files",
        detail: "The notes and images exactly as they are in the vault.",
      },
    ];
  return [
    {
      id: "pdf",
      icon: pdfFile ? FileText : Printer,
      label: "PDF",
      detail: pdfFile
        ? "A document to send or print."
        : "Opens the print dialog, where you can save it as a PDF.",
    },
    {
      id: "html",
      icon: Globe,
      label: "Web page",
      detail: "One file with the images inside, in your theme.",
    },
    {
      id: "md",
      icon: Code2,
      label: "Markdown",
      detail: "For other apps: [[links]] become regular links. With images, a zip.",
    },
    {
      id: "zip",
      icon: Archive,
      label: "Original files",
      detail: "The note and its images exactly as they are, in a zip.",
    },
  ];
}

/** Prints a note with the system's print dialog (⌘P). */
export async function printNote(client: NotesClient, path: string) {
  try {
    await client.export?.print(path, printTheme());
  } catch (e) {
    fail("Couldn't print the note", e);
  }
}
