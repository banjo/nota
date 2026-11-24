import type { ExternalDoc, NotesClient } from "@nota/client";
import { createEditor, type EditorView, replaceDoc, setEditorMode } from "@nota/editor";
import { Button } from "@nota/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@nota/ui/dialog";
import { toast } from "@nota/ui/sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, FileQuestion, FileText, PenLine } from "lucide-react";
import { useEffect, useLayoutEffect, useRef } from "react";
import { openExternal } from "../actions.ts";
import { useClient } from "../context.ts";
import {
  closePreview,
  discardDraft,
  editPreview,
  externalKey,
  revertDraft,
  useOpened,
} from "../external.ts";
import { displayPath, linkKind } from "../lib/opened.ts";
import { resolveLink } from "../lib/notes.ts";
import { keys } from "../queries.ts";
import { useUI } from "../state.ts";
import { EmptyState } from "./EmptyState.tsx";
import { showOpened } from "./OpenedFiles.tsx";
import { OpenStrip } from "./OpenBanner.tsx";
import { TopBar } from "./TopBar.tsx";

/** "Reveal in Finder" where there is a Finder. */
export const revealLabel = (client: NotesClient) =>
  client.shell?.platform === "darwin"
    ? "Reveal in Finder"
    : client.shell?.platform === "windows"
      ? "Show in Explorer"
      : "Show in folder";

/** A preview's question (close with changes, move, overwrite); in the shell. */
export function ConfirmDialog() {
  const confirm = useOpened((s) => s.confirm);
  const close = () => useOpened.setState({ confirm: null });
  return (
    <Dialog open={!!confirm} onOpenChange={(o) => !o && close()}>
      <DialogContent className="max-w-[440px]">
        <DialogHeader>
          <DialogTitle>{confirm?.title}</DialogTitle>
          <DialogDescription>{confirm?.body}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={close}>
            Cancel
          </Button>
          <Button
            variant={confirm?.destructive ? "destructive" : "default"}
            onClick={() => {
              const run = confirm?.run;
              close();
              void run?.();
            }}
          >
            {confirm?.action}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** The strip under the top bar: what this is, and the actions (variant A). */
export function PreviewBanner({
  doc,
  editing,
  dirty,
  gone = false,
}: {
  doc: ExternalDoc;
  editing: boolean;
  dirty: boolean;
  /** The file went away (or can't be read) since it was shown. */
  gone?: boolean;
}) {
  const client = useClient();
  const qc = useQueryClient();
  const openPalette = useUI((s) => s.openPalette);
  const where = displayPath(doc);
  const save = (
    <Button size="sm" variant="default" onClick={() => openPalette("saveExternal", doc.id)}>
      Save to vault…
    </Button>
  );
  const reveal = (
    <Button size="sm" variant="ghost" onClick={() => void client.opener?.reveal(doc.id)}>
      {revealLabel(client)}
    </Button>
  );
  if (gone)
    return (
      <OpenStrip
        tone="warn"
        icon={<FileQuestion strokeWidth={1.75} />}
        actions={
          editing ? (
            <>
              {save}
              <Button size="sm" variant="ghost" onClick={() => discardDraft(qc, doc.id)}>
                Discard
              </Button>
            </>
          ) : (
            <Button size="sm" variant="ghost" onClick={() => closePreview(client, qc, doc.id)}>
              Close
            </Button>
          )
        }
      >
        <span className="font-medium">{where} is gone.</span>
        <span className="opacity-80">
          {" "}
          {editing
            ? "It was moved or deleted. Your draft is still here: save it to the vault to keep it."
            : "It was moved or deleted. This is the text it had."}
        </span>
      </OpenStrip>
    );
  if (!doc.utf8)
    return (
      <OpenStrip
        tone="warn"
        icon={<AlertTriangle strokeWidth={1.75} />}
        actions={
          <>
            {reveal}
            <Button size="sm" variant="ghost" onClick={() => closePreview(client, qc, doc.id)}>
              Close
            </Button>
          </>
        }
      >
        <span className="font-medium">Preview of {where}.</span>{" "}
        <span className="opacity-80">
          It isn't UTF-8 text, so it's shown with replacement characters and can't be edited or
          saved to the vault.
        </span>
      </OpenStrip>
    );
  if (editing)
    return (
      <OpenStrip
        icon={<PenLine strokeWidth={1.75} />}
        actions={
          <>
            {save}
            <Button
              size="sm"
              variant="soft"
              disabled={!dirty}
              onClick={() => revertDraft(qc, doc.id)}
            >
              Revert
            </Button>
            <Button size="sm" variant="ghost" onClick={() => discardDraft(qc, doc.id)}>
              Discard
            </Button>
          </>
        }
      >
        <span className="font-medium">
          {dirty ? "Draft · unsaved changes" : "Draft · no changes yet"}
        </span>
        <span className="opacity-80">
          {" "}
          — nothing is written to {where} or to your vault until you save it to the vault.
        </span>
      </OpenStrip>
    );
  return (
    <OpenStrip
      icon={<FileText strokeWidth={1.75} />}
      actions={
        <>
          <Button size="sm" variant="soft" onClick={() => editPreview(qc, doc.id)}>
            <PenLine strokeWidth={1.75} />
            Edit
          </Button>
          {save}
          {reveal}
          <Button size="sm" variant="ghost" onClick={() => closePreview(client, qc, doc.id)}>
            Close
          </Button>
        </>
      }
    >
      <span className="font-medium">Preview of {where}</span>
      <span className="opacity-80">
        {" "}
        — it isn't in your vault. Nothing is copied or changed unless you choose to.
      </span>
    </OpenStrip>
  );
}

/**
 * The editor for a preview: read-only Reading mode, or Live while the
 * draft is open. Links never create notes or open local files.
 */
function PreviewEditor({ doc, draft }: { doc: ExternalDoc; draft: string | undefined }) {
  const client = useClient();
  const qc = useQueryClient();
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const editing = draft !== undefined;
  const shown = useRef(draft ?? doc.body);
  // Where a click in the preview asked to start editing.
  const editAt = useRef<number | null>(null);

  useLayoutEffect(() => {
    const id = doc.id;
    const v = createEditor(host.current!, {
      doc: shown.current,
      mode: editing ? "live" : "reading",
      placeholder: "Empty file",
      onChange: (text) => {
        shown.current = text;
        useOpened.setState((s) => ({ drafts: { ...s.drafts, [id]: text } }));
      },
      onWikilink: (target) => {
        void (async () => {
          const tree = await qc.ensureQueryData({
            queryKey: keys.tree,
            queryFn: () => client.tree(),
          });
          const hit = resolveLink(target, tree.notes);
          if (hit) useUI.getState().navigate({ kind: "note", path: hit.path });
          else toast(`No note named “${target}” in your vault`);
        })();
      },
      onTag: () => toast("Tags belong to notes in your vault. Save the file to the vault first."),
      onLink: (href) => {
        const k = linkKind(href);
        if (k === "web") return openExternal(href);
        if (k === "blocked") return void toast("This link can't be opened from a preview.");
        client
          .opener!.follow(id, href)
          .then((o) => showOpened([o], client))
          .catch((e: unknown) =>
            toast.error("Couldn't open the link", {
              description: e instanceof Error ? e.message : String(e),
            }),
          );
      },
    });
    view.current = v;
    return () => {
      v.destroy();
      view.current = null;
    };
    // One editor per preview; later changes arrive below.
  }, [doc.id]);

  // Edit, Discard, Revert, or the file changed on disk while previewing.
  useEffect(() => {
    const v = view.current;
    if (!v) return;
    const want = draft ?? doc.body;
    if (want !== shown.current) {
      shown.current = want;
      replaceDoc(v, want);
    }
    setEditorMode(v, editing ? "live" : "reading");
    if (editing && !v.hasFocus) v.focus();
    const at = editAt.current;
    editAt.current = null;
    if (editing && at !== null)
      v.dispatch({ selection: { anchor: Math.min(at, v.state.doc.length) }, scrollIntoView: true });
  }, [draft, doc.body, editing]);

  // A click in the preview starts a draft there, unless it opened a
  // link, ticked a task or selected text.
  useEffect(() => {
    const v = view.current;
    const el = host.current;
    if (editing || !v || !el) return;
    const onClick = (e: MouseEvent) => {
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      const t = e.target as HTMLElement | null;
      if (t?.closest(".cm-task-checkbox, [data-wikilink], [data-tag], [data-href], a")) return;
      if (!(window.getSelection()?.isCollapsed ?? true)) return;
      editAt.current = v.posAtCoords({ x: e.clientX, y: e.clientY }, false);
      editPreview(qc, doc.id);
      // Not editable (not UTF-8): don't carry the position to a later Edit.
      if (useOpened.getState().drafts[doc.id] === undefined) editAt.current = null;
    };
    el.addEventListener("click", onClick);
    return () => el.removeEventListener("click", onClick);
  }, [editing, doc.id]);

  return <div ref={host} className="nota-editor min-h-[40vh] text-editor" />;
}

const propText = (v: unknown): string =>
  Array.isArray(v)
    ? v.map(propText).join(", ")
    : typeof v === "string" || typeof v === "number" || typeof v === "boolean"
      ? String(v)
      : JSON.stringify(v);

/** Frontmatter, read-only: the file isn't a note of the vault. */
function Properties({ props }: { props: Record<string, unknown> }) {
  const entries = Object.entries(props);
  if (!entries.length) return null;
  return (
    <dl className="mb-6 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-6 gap-y-1 border-b border-divider pb-4 text-ui">
      {entries.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-faint">{k}</dt>
          <dd className="truncate text-text-soft">{propText(v)}</dd>
        </div>
      ))}
    </dl>
  );
}

/** A Markdown file from outside the vault: preview first, a draft on Edit. */
export function ExternalPage({ id }: { id: string }) {
  const client = useClient();
  const qc = useQueryClient();
  const draft = useOpened((s) => s.drafts[id]);
  const doc = useQuery({
    queryKey: externalKey(id),
    queryFn: () => client.opener!.read(id),
    enabled: !!client.opener,
    refetchOnWindowFocus: true,
    staleTime: 0,
    retry: false,
  });
  const editing = draft !== undefined;
  const dirty = editing && !!doc.data && draft !== doc.data.body;

  // A failed re-read keeps the text it had: the file went away.
  if ((doc.isError && !doc.data) || !client.opener)
    return (
      <>
        <TopBar crumbs={[{ label: "Outside vault" }]} />
        <EmptyState
          icon={FileQuestion}
          title="Couldn't show this file"
          body={
            doc.error instanceof Error ? doc.error.message : "It may have moved or been deleted."
          }
          action={
            <Button variant="secondary" onClick={() => closePreview(client, qc, id)}>
              Close preview
            </Button>
          }
        />
      </>
    );
  if (!doc.data) return <TopBar crumbs={[{ label: "Outside vault" }]} />;
  const d = doc.data;
  return (
    <>
      <TopBar
        crumbs={[{ label: "Outside vault" }, { label: d.title || d.name }]}
        right={
          <span className="mr-1 rounded-sm bg-ink-soft px-1.5 py-0.5 text-[11.5px] font-medium text-ink-strong">
            {editing ? "Draft" : "Preview"}
          </span>
        }
      />
      <PreviewBanner doc={d} editing={editing} dirty={dirty} gone={doc.isError} />
      <article className="mx-auto w-full max-w-[740px] px-14 pt-10 pb-[140px] max-[720px]:px-5 max-[720px]:pt-5">
        <Properties props={d.properties} />
        <PreviewEditor key={d.id} doc={d} draft={draft} />
      </article>
    </>
  );
}
