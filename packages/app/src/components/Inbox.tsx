import type { NoteInfo, Tree } from "@nota/client";
import { Button } from "@nota/ui/button";
import { toast } from "@nota/ui/sonner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@nota/ui/tooltip";
import { useQueryClient } from "@tanstack/react-query";
import { Folder, Inbox as InboxIcon } from "lucide-react";
import { useState } from "react";
import { create } from "zustand";
import { persist } from "zustand/middleware";
import { fail, moveNote } from "../actions.ts";
import { useClient } from "../context.ts";
import { relativeTime } from "../lib/format.ts";
import { folderLabel, splitTitle } from "../lib/notes.ts";
import { keys, useInbox, useNote, useTree } from "../queries.ts";
import { type Suggestion, useContributions } from "../registry.ts";
import { useUI } from "../state.ts";
import { EmptyState } from "./EmptyState.tsx";
import { TopBar } from "./TopBar.tsx";

// Inbox triage (the "Inkorg" prototype): capture a thought, then give
// each note a home in one click.

/** Notes the user chose to keep in the inbox: no suggestion for them. */
const useKept = create<{ kept: Record<string, true>; keep: (p: string) => void }>()(
  persist((set) => ({ kept: {}, keep: (p) => set((s) => ({ kept: { ...s.kept, [p]: true } })) }), {
    name: "nota:inbox-kept",
  }),
);

export function CaptureField({
  autoFocus,
  onSaved,
  compact,
}: {
  autoFocus?: boolean;
  onSaved?: () => void;
  compact?: boolean;
}) {
  const client = useClient();
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const save = async () => {
    const body = text.trim();
    if (!body || busy) return;
    setBusy(true);
    try {
      await client.create({ body });
      setText("");
      void qc.invalidateQueries({ queryKey: keys.inbox });
      void qc.invalidateQueries({ queryKey: keys.tree });
      onSaved?.();
    } catch (e) {
      fail("Couldn't save to the inbox", e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className={compact ? "" : "rounded-lg bg-surface px-3.5 pt-3 pb-2.5 shadow-card"}>
      <input
        autoFocus={autoFocus}
        aria-label="Capture a thought"
        placeholder="Capture a thought…"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.nativeEvent.isComposing) {
            e.preventDefault();
            void save();
          }
        }}
        className="w-full bg-transparent text-[15px] text-foreground outline-none placeholder:text-faint"
      />
      <div className="mt-2 flex items-center justify-between gap-3 text-micro text-faint">
        <span>Enter saves to the inbox</span>
        {client.kind === "desktop" ? (
          <span className="max-[520px]:hidden">
            or{" "}
            <code className="rounded-xs bg-code px-[5px] py-px font-mono text-[11px] text-muted-foreground">
              nota add "…"
            </code>
          </span>
        ) : null}
      </div>
    </div>
  );
}

function useSuggestions(note: NoteInfo, tree: Tree | undefined): Suggestion | undefined {
  const suggesters = useContributions("inboxSuggester");
  if (!tree) return undefined;
  return suggesters.flatMap((s) => s.suggest(note, { tree })).sort((a, b) => b.score - a.score)[0];
}

function InboxItem({ note, tree }: { note: NoteInfo; tree: Tree | undefined }) {
  const client = useClient();
  const qc = useQueryClient();
  const navigate = useUI((s) => s.navigate);
  const openPalette = useUI((s) => s.openPalette);
  const kept = useKept((s) => !!s.kept[note.path]);
  const keep = useKept((s) => s.keep);
  const full = useNote(note.path);
  const suggestion = useSuggestions(note, tree);
  const preview = full.data ? splitTitle(full.data.body).rest.replace(/\s+/g, " ").trim() : "";

  const move = async (folder: string) => {
    const r = await moveNote(client, qc, note.path, folder, { quiet: true });
    if (!r) return;
    toast.success(`Moved to ${folderLabel(folder)}`, {
      action: {
        label: "Undo",
        onClick: () =>
          void client.move(r.path, tree?.inbox ?? "Inbox").then(() => void qc.invalidateQueries()),
      },
    });
  };

  return (
    <li className="border-t border-divider px-5 py-3.5 first:border-t-0 max-[720px]:px-4">
      <button
        type="button"
        onClick={() => navigate({ kind: "note", path: note.path })}
        className="block w-full text-left outline-none focus-visible:underline"
      >
        <h3 className="flex items-baseline gap-2 text-[14px] font-semibold text-foreground">
          <span className="truncate">{note.title}</span>
          <span className="ml-auto shrink-0 text-small font-normal text-faint">
            {relativeTime(note.created ?? note.modified)}
          </span>
        </h3>
        {preview ? (
          <p className="mt-[3px] line-clamp-2 text-[12.5px] leading-[1.45] text-muted-foreground">
            {preview}
          </p>
        ) : null}
      </button>
      <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
        {suggestion && !kept ? (
          <>
            <span className="mr-0.5 text-small text-faint">Suggested</span>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="soft"
                  size="sm"
                  onClick={() => void move(suggestion.folder)}
                  className="pointer-coarse:h-11"
                >
                  <Folder className="size-3.5" strokeWidth={2} />
                  Move to {folderLabel(suggestion.folder)}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{suggestion.reason}</TooltipContent>
            </Tooltip>
          </>
        ) : null}
        <Button
          variant="ghost"
          size="sm"
          onClick={() => openPalette("move", note.path)}
          className="pointer-coarse:h-11"
        >
          Other folder…
        </Button>
        {!kept ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => keep(note.path)}
            className="pointer-coarse:h-11"
          >
            Keep in inbox
          </Button>
        ) : null}
      </div>
    </li>
  );
}

export function InboxView() {
  const inbox = useInbox();
  const tree = useTree();
  const notes = inbox.data ?? [];
  return (
    <div className="h-full overflow-y-auto">
      <TopBar crumbs={[{ label: "Inbox" }]} />
      <div className="mx-auto w-full max-w-[680px] px-6 pt-5 pb-24 max-[720px]:px-3">
        <CaptureField autoFocus={notes.length === 0} />
        <div className="flex items-baseline gap-2 px-5 pt-6 pb-2 max-[720px]:px-4">
          <h1 className="text-[18px] font-[650] tracking-[-0.01em] text-foreground">Inbox</h1>
          {notes.length ? <span className="text-ui text-faint">{notes.length} to sort</span> : null}
        </div>
        {inbox.data && notes.length === 0 ? (
          <EmptyState
            icon={InboxIcon}
            title="Inbox zero"
            body="Everything has a place. New thoughts land here."
          />
        ) : (
          <ul>
            {notes.map((n) => (
              <InboxItem key={n.path} note={n} tree={tree.data} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
