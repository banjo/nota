import type { HistoryEntry } from "@nota/client";
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
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, Copy, History as HistoryIcon, LoaderCircle, RotateCcw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { create } from "zustand";
import { fail } from "../actions.ts";
import { useClient } from "../context.ts";
import { type DiffLine, diffLines, diffStats } from "../lib/diff.ts";
import { relativeTime } from "../lib/format.ts";
import { useNarrow } from "../lib/viewport.ts";
import { keys, useNote } from "../queries.ts";
import { useUI } from "../state.ts";

// A note's versions on the sync server: pick one to read it or see what
// changed since, then restore it (a new version: nothing is lost) or copy
// its text. ⌘K "Show history", the note's "…" menu and ⌘⇧H open it.

export const useHistoryDialog = create<{ path: string | null }>(() => ({ path: null }));
export const openHistory = (path: string) => useHistoryDialog.setState({ path });
const close = () => useHistoryDialog.setState({ path: null });

/**
 * The versions worth listing: a move is a delete of the old path next to
 * the new path's version, and that delete says nothing new.
 */
export function visibleVersions(vs: HistoryEntry[]): HistoryEntry[] {
  return vs.filter(
    (e, i) =>
      !(
        e.deleted &&
        i > 0 &&
        vs[i - 1]!.movedFrom === e.path &&
        vs[i - 1]!.version === e.version + 1
      ),
  );
}

/** The version to show first: the newest one that differs from the note now. */
export function initialVersion(list: HistoryEntry[], current: string | undefined) {
  return (list.find((e) => !e.deleted && e.hash !== current) ?? list[0])?.version ?? null;
}

const when = new Intl.DateTimeFormat("en-GB", {
  day: "numeric",
  month: "short",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
});

function sizeChange(e: HistoryEntry, older: HistoryEntry | undefined): string {
  if (e.deleted) return "Deleted";
  if (!older || older.deleted) return "Created";
  const d = e.size - older.size;
  if (d === 0) return "Same size";
  return `${d > 0 ? "+" : "−"}${Math.abs(d)} B`;
}

export function HistoryDialog() {
  const path = useHistoryDialog((s) => s.path);
  // Keep the content while the dialog closes.
  const [last, setLast] = useState(path);
  if (path && path !== last) setLast(path);
  return (
    <Dialog open={!!path} onOpenChange={(o) => !o && close()}>
      <DialogContent
        showClose
        className="flex h-[min(640px,calc(100dvh-4rem))] max-w-[860px] flex-col overflow-hidden max-[720px]:h-[calc(var(--visual-height,100dvh)-var(--safe-top)-12px)]"
      >
        {last ? <HistoryBody key={last} path={last} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function HistoryBody({ path }: { path: string }) {
  const client = useClient();
  const qc = useQueryClient();
  const narrow = useNarrow();
  const note = useNote(path);
  const versions = useQuery({
    queryKey: keys.history(path),
    queryFn: () => client.history(path),
    retry: false,
  });
  const list = useMemo(() => visibleVersions(versions.data ?? []), [versions.data]);
  const [picked, setPicked] = useState<number | null>(null);
  const [showList, setShowList] = useState(true);
  const current = note.data?.hash;
  // Start on the newest version that differs from the note as it is.
  useEffect(() => {
    if (picked === null && list.length > 0) setPicked(initialVersion(list, current));
  }, [list, current, picked]);
  const entry = list.find((e) => e.version === picked);
  const title = note.data?.title || path.split("/").pop()?.replace(/\.md$/, "") || path;

  const header = (
    <DialogHeader className="shrink-0 pb-3 pr-12">
      <DialogTitle className="flex items-center gap-2">
        <HistoryIcon className="size-4 text-faint" strokeWidth={1.75} aria-hidden />
        <span className="truncate">History of {title}</span>
      </DialogTitle>
      <DialogDescription>
        The versions on the sync server, newest first. Restoring one makes it the newest version;
        nothing is lost.
      </DialogDescription>
    </DialogHeader>
  );

  if (versions.isError) {
    const err = versions.error;
    const noSync = /needs sync/i.test(String(err));
    return (
      <>
        {header}
        <div className="grid flex-1 place-items-center px-6 pb-8 text-center">
          <div className="max-w-sm">
            <p className="text-ui text-foreground">
              {noSync ? "History comes with sync" : "Couldn't load the history"}
            </p>
            <p className="mt-1 text-small text-muted-foreground">
              {noSync
                ? "Versions are kept on your sync server. Sign in to sync, and every save from then on is kept."
                : err instanceof Error
                  ? err.message
                  : String(err)}
            </p>
            {noSync ? (
              <Button
                variant="secondary"
                className="mt-4"
                onClick={() => {
                  close();
                  useUI.getState().navigate({ kind: "settings" });
                }}
              >
                Open Settings
              </Button>
            ) : null}
          </div>
        </div>
      </>
    );
  }

  if (versions.isPending) {
    return (
      <>
        {header}
        <div className="grid flex-1 place-items-center text-faint">
          <LoaderCircle className="size-5 animate-spin" aria-label="Loading" />
        </div>
      </>
    );
  }

  const listPane = (
    <ol aria-label="Versions" className="min-h-0 overflow-y-auto overscroll-contain p-2">
      {list.map((e, i) => {
        const on = e.version === picked;
        const isCurrent = !e.deleted && e.hash === current;
        return (
          <li key={e.version}>
            <button
              type="button"
              aria-current={on ? "true" : undefined}
              onClick={() => {
                setPicked(e.version);
                setShowList(false);
              }}
              className={cn(
                "flex w-full flex-col gap-0.5 rounded-sm px-2.5 py-2 text-left outline-none focus-visible:bg-hover pointer-coarse:py-3",
                on ? "bg-selected" : "hover:bg-hover",
              )}
            >
              <span className="flex items-center gap-1.5 text-ui text-foreground">
                <span className="truncate">{relativeTime(e.modifiedAt)}</span>
                {isCurrent ? (
                  <span className="rounded-xs bg-ink-soft px-1 text-micro font-medium text-ink-strong">
                    Current
                  </span>
                ) : null}
              </span>
              <span className="truncate text-small text-faint">
                {e.device} · {sizeChange(e, list[i + 1])}
                {e.movedFrom ? ` · moved from ${e.movedFrom}` : ""}
              </span>
            </button>
          </li>
        );
      })}
    </ol>
  );

  return (
    <>
      {header}
      <div
        className={cn(
          "grid min-h-0 flex-1 border-t border-hairline",
          narrow ? "grid-cols-1" : "grid-cols-[240px_minmax(0,1fr)]",
        )}
      >
        {!narrow || showList ? (
          <div className={cn("min-h-0", !narrow && "border-r border-hairline")}>{listPane}</div>
        ) : null}
        {entry && (!narrow || !showList) ? (
          <VersionPane
            key={entry.version}
            path={path}
            entry={entry}
            current={note.data?.content}
            isCurrent={!entry.deleted && entry.hash === current}
            onBack={narrow ? () => setShowList(true) : undefined}
            onRestored={() => {
              void qc.invalidateQueries({ queryKey: keys.history(path) });
              close();
            }}
          />
        ) : null}
      </div>
    </>
  );
}

export function VersionPane({
  path,
  entry,
  current,
  isCurrent,
  onBack,
  onRestored,
}: {
  path: string;
  entry: HistoryEntry;
  current: string | undefined;
  isCurrent: boolean;
  onBack?: () => void;
  onRestored: () => void;
}) {
  const client = useClient();
  const qc = useQueryClient();
  const [view, setView] = useState<"changes" | "text">("changes");
  const [busy, setBusy] = useState(false);
  const content = useQuery({
    // Not under keys.history: a version never changes, so change events leave it be.
    queryKey: ["historyVersion", path, entry.version],
    queryFn: () => client.historyVersion(path, entry.version),
    enabled: !entry.deleted,
    staleTime: Infinity,
    retry: false,
  });
  const diff = useMemo(
    () => (content.data != null && current !== undefined ? diffLines(content.data, current) : null),
    [content.data, current],
  );
  const stats = diff ? diffStats(diff) : null;

  async function restore() {
    setBusy(true);
    try {
      await client.restore(path, entry.version);
      toast.success(`Restored the version from ${when.format(new Date(entry.modifiedAt))}`, {
        description:
          client.kind === "desktop"
            ? "It arrives here with the next sync, in a moment."
            : undefined,
      });
      void qc.invalidateQueries({ queryKey: keys.note(path) });
      onRestored();
    } catch (e) {
      fail("Couldn't restore the version", e);
    } finally {
      setBusy(false);
    }
  }

  async function copy() {
    if (content.data == null) return;
    try {
      await navigator.clipboard.writeText(content.data);
      toast("Copied the version's text");
    } catch (e) {
      fail("Couldn't copy", e);
    }
  }

  return (
    <section aria-label="Version" className="flex min-h-0 min-w-0 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-hairline px-3 py-2">
        {onBack ? (
          <Button variant="ghost" size="icon-sm" aria-label="Back to versions" onClick={onBack}>
            <ChevronLeft className="size-5" />
          </Button>
        ) : null}
        <div className="min-w-0 flex-1">
          <p className="truncate text-ui text-foreground">
            {when.format(new Date(entry.modifiedAt))}
          </p>
          <p className="truncate text-small text-faint">
            Version {entry.version} · {entry.device}
            {stats && !isCurrent && stats.added + stats.removed > 0
              ? ` · since then +${stats.added} −${stats.removed} lines`
              : ""}
          </p>
        </div>
        {!entry.deleted ? (
          <div role="group" aria-label="Show" className="flex rounded-md bg-hover p-0.5">
            {(["changes", "text"] as const).map((v) => (
              <button
                key={v}
                type="button"
                aria-pressed={view === v}
                onClick={() => setView(v)}
                className={cn(
                  "h-6 rounded-[5px] px-2 text-small outline-none focus-visible:ring-2 focus-visible:ring-ink/40 pointer-coarse:h-9 pointer-coarse:px-3",
                  view === v
                    ? "bg-background text-foreground shadow-xs"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {v === "changes" ? "Changes" : "Text"}
              </button>
            ))}
          </div>
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-auto overscroll-contain bg-background">
        {entry.deleted ? (
          <p className="p-4 text-ui text-muted-foreground">
            The note was deleted in this version. Restore the version before it to bring it back.
          </p>
        ) : content.isError ? (
          <p className="p-4 text-ui text-danger">{String(content.error)}</p>
        ) : content.data == null ? (
          <div className="grid h-full place-items-center text-faint">
            <LoaderCircle className="size-5 animate-spin" aria-label="Loading" />
          </div>
        ) : view === "text" || !diff ? (
          <pre className="p-4 font-mono text-[12.5px] leading-relaxed whitespace-pre-wrap text-foreground">
            {content.data}
          </pre>
        ) : isCurrent || (stats && stats.added + stats.removed === 0) ? (
          <p className="p-4 text-ui text-muted-foreground">
            This version is the same as the note now.
          </p>
        ) : (
          <DiffView lines={diff} />
        )}
      </div>
      {!entry.deleted ? (
        <div className="flex shrink-0 justify-end gap-2 border-t border-hairline px-3 py-2.5">
          <Button variant="ghost" onClick={() => void copy()} disabled={content.data == null}>
            <Copy /> Copy text
          </Button>
          <Button
            onClick={() => void restore()}
            disabled={busy || isCurrent || content.data == null}
          >
            {busy ? <LoaderCircle className="animate-spin" /> : <RotateCcw />} Restore this version
          </Button>
        </div>
      ) : null}
    </section>
  );
}

/** The version against the note now: removed lines red, added lines green. */
function DiffView({ lines }: { lines: DiffLine[] }) {
  return (
    <div
      role="table"
      aria-label="Changes from this version to now"
      className="py-2 font-mono text-[12.5px] leading-relaxed"
    >
      {lines.map((l, i) => (
        <div
          // Lines can repeat; their order is their identity.
          key={i}
          role="row"
          className={cn(
            "flex px-2 whitespace-pre-wrap",
            l.kind === "del" && "bg-danger/10 text-foreground",
            l.kind === "add" && "bg-success/12 text-foreground",
            l.kind === "same" && "text-muted-foreground",
          )}
        >
          <span aria-hidden className="w-5 shrink-0 text-faint select-none">
            {l.kind === "del" ? "−" : l.kind === "add" ? "+" : ""}
          </span>
          <span role="cell" className="min-w-0 break-words">
            <span className="sr-only">
              {l.kind === "del"
                ? "Only in this version: "
                : l.kind === "add"
                  ? "Added since: "
                  : ""}
            </span>
            {l.text || " "}
          </span>
        </div>
      ))}
    </div>
  );
}
