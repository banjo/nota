import type { TrashItem } from "@nota/client";
import { createEditor } from "@nota/editor";
import { Button } from "@nota/ui/button";
import { cn } from "@nota/ui/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, LoaderCircle, RotateCcw, Trash2 } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { emptyTrash, purgeFromTrash, restoreFromTrash } from "../actions.ts";
import { useClient } from "../context.ts";
import { useOpened } from "../external.ts";
import { relativeTime } from "../lib/format.ts";
import { useNarrow } from "../lib/viewport.ts";
import { keys } from "../queries.ts";
import { EmptyState } from "./EmptyState.tsx";
import { TopBar } from "./TopBar.tsx";

// The deleted notes: read one, put it back, or (on the desktop, where
// .trash/ is a folder of files) delete it for good.

const name = (path: string) => path.split("/").pop()!.replace(/\.md$/i, "");
const folder = (path: string) => (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "");

export function TrashView() {
  const client = useClient();
  const qc = useQueryClient();
  const narrow = useNarrow();
  const trash = useQuery({ queryKey: keys.trash, queryFn: () => client.trash() });
  const items = trash.data ?? [];
  const [picked, setPicked] = useState<string | null>(null);
  const item = items.find((i) => i.id === picked) ?? (narrow ? undefined : items[0]);
  const confirmEmpty = () =>
    useOpened.setState({
      confirm: {
        title: "Empty the trash?",
        body: `The ${items.length === 1 ? "note" : `${items.length} notes`} in the trash will be deleted for good. This can't be undone.`,
        action: "Empty trash",
        destructive: true,
        run: () => emptyTrash(client, qc),
      },
    });

  const bar = (
    <TopBar
      crumbs={[{ label: "Trash" }]}
      right={
        client.emptyTrash && items.length > 0 ? (
          <Button variant="ghost" size="sm" onClick={confirmEmpty}>
            Empty trash
          </Button>
        ) : undefined
      }
    />
  );

  if (trash.isPending)
    return (
      <div className="flex h-full flex-col">
        {bar}
        <div className="grid flex-1 place-items-center text-faint">
          <LoaderCircle className="size-5 animate-spin" aria-label="Loading" />
        </div>
      </div>
    );
  if (items.length === 0)
    return (
      <div className="flex h-full flex-col">
        {bar}
        <EmptyState
          icon={Trash2}
          title={trash.isError ? "Couldn't load the trash" : "The trash is empty"}
          body={
            trash.isError
              ? String(trash.error)
              : client.purgeTrash
                ? "Deleted notes stay here until you empty the trash."
                : "Deleted notes show up here, and can be put back."
          }
        />
      </div>
    );

  const showList = !narrow || !item;
  return (
    <div className="flex h-full flex-col">
      {bar}
      <div
        className={cn(
          "grid min-h-0 flex-1 border-t border-hairline",
          narrow ? "grid-cols-1" : "grid-cols-[280px_minmax(0,1fr)]",
        )}
      >
        {showList ? (
          <ul
            aria-label="Deleted notes"
            className={cn(
              "min-h-0 overflow-y-auto overscroll-contain p-2",
              !narrow && "border-r border-hairline",
            )}
          >
            {items.map((i) => (
              <li key={i.id}>
                <button
                  type="button"
                  aria-current={i.id === item?.id ? "true" : undefined}
                  onClick={() => setPicked(i.id)}
                  className={cn(
                    "flex w-full flex-col gap-0.5 rounded-sm px-2.5 py-2 text-left outline-none focus-visible:bg-hover pointer-coarse:py-3",
                    i.id === item?.id ? "bg-selected" : "hover:bg-hover",
                  )}
                >
                  <span className="truncate text-ui text-foreground">{name(i.path)}</span>
                  <span className="truncate text-small text-faint">
                    {[folder(i.path), relativeTime(i.deletedAt)].filter(Boolean).join(" · ")}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        ) : null}
        {item ? (
          <TrashedNote
            key={item.id}
            item={item}
            onBack={narrow ? () => setPicked(null) : undefined}
            onGone={() => setPicked(null)}
          />
        ) : null}
      </div>
    </div>
  );
}

export function TrashedNote({
  item,
  onBack,
  onGone,
}: {
  item: TrashItem;
  onBack?: () => void;
  onGone: () => void;
}) {
  const client = useClient();
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const content = useQuery({
    queryKey: [...keys.trash, item.id],
    queryFn: () => client.trashNote(item.id),
    staleTime: Infinity,
    retry: false,
  });
  const where = folder(item.path);

  async function restore() {
    setBusy(true);
    const path = await restoreFromTrash(client, qc, item.id);
    setBusy(false);
    if (path) onGone();
  }
  const purge = () =>
    useOpened.setState({
      confirm: {
        title: `Delete “${name(item.path)}” for good?`,
        body: "It will be removed from the trash. This can't be undone.",
        action: "Delete for good",
        destructive: true,
        run: async () => {
          await purgeFromTrash(client, qc, item.id);
          onGone();
        },
      },
    });

  return (
    <section aria-label={name(item.path)} className="flex min-h-0 min-w-0 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-hairline px-3 py-2">
        {onBack ? (
          <Button variant="ghost" size="icon-sm" aria-label="Back to the trash" onClick={onBack}>
            <ChevronLeft className="size-5" />
          </Button>
        ) : null}
        <div className="min-w-0 flex-1">
          <p className="truncate text-ui text-foreground">{name(item.path)}</p>
          <p className="truncate text-small text-faint">
            Deleted {relativeTime(item.deletedAt).replace(/^[A-Z]/, (c) => c.toLowerCase())}
            {where ? ` from ${where}` : ""}
          </p>
        </div>
        {client.purgeTrash ? (
          <Button variant="ghost" onClick={purge}>
            Delete for good
          </Button>
        ) : null}
        <Button onClick={() => void restore()} disabled={busy}>
          {busy ? <LoaderCircle className="animate-spin" /> : <RotateCcw />} Restore
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {content.isError ? (
          <p className="p-4 text-ui text-danger">{String(content.error)}</p>
        ) : content.data === undefined ? (
          <div className="grid h-full place-items-center text-faint">
            <LoaderCircle className="size-5 animate-spin" aria-label="Loading" />
          </div>
        ) : (
          <ReadOnly text={content.data} />
        )}
      </div>
    </section>
  );
}

/** The note in Reading mode: nothing in it can be edited or followed. */
function ReadOnly({ text }: { text: string }) {
  const host = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const v = createEditor(host.current!, { doc: text, mode: "reading" });
    return () => v.destroy();
  }, [text]);
  return (
    <div className="mx-auto max-w-[720px] px-6 py-5">
      <div ref={host} className="nota-editor text-editor" />
    </div>
  );
}
