import { Button } from "@nota/ui/button";
import { NotaError } from "@nota/client";
import { FileQuestion } from "lucide-react";
import { relativeTime } from "../lib/format.ts";
import { useNote } from "../queries.ts";
import { useContributions } from "../registry.ts";
import { useNoteMode, useUI } from "../state.ts";
import { EmptyState } from "./EmptyState.tsx";
import { NoteEditor } from "./NoteEditor.tsx";
import { ModeSwitch } from "./ModeSwitch.tsx";
import { NoteMenu } from "./NoteRow.tsx";
import { OpenedNoteStrip } from "./OpenBanner.tsx";
import { PropertiesSection } from "./PropertiesSection.tsx";
import { TopBar } from "./TopBar.tsx";

/** One note: breadcrumbs, title, properties, the editor and the panels. */
export function NotePage({ path }: { path: string }) {
  const note = useNote(path);
  const panels = useContributions("panel");
  const navigate = useUI((s) => s.navigate);
  const toggle = useUI((s) => s.toggle);
  const mode = useNoteMode();
  const opened = useUI((s) => s.route.kind === "note" && s.route.via === "open");

  if (note.isError) {
    const missing =
      note.error instanceof NotaError
        ? note.error.code === "not_found"
        : /not found|no such|not exist/i.test(String(note.error));
    return (
      <>
        <TopBar crumbs={[{ label: "Missing note" }]} />
        <EmptyState
          icon={FileQuestion}
          title={missing ? "This note isn't here anymore" : "Couldn't open the note"}
          body={missing ? "It was moved or deleted, maybe on another device." : String(note.error)}
          action={
            <Button variant="secondary" onClick={() => navigate({ kind: "home" })}>
              Back to notes
            </Button>
          }
        />
      </>
    );
  }
  if (!note.data) return <TopBar crumbs={[]} />;
  const n = note.data;
  const folders = n.folder ? n.folder.split("/") : [];

  return (
    <>
      <TopBar
        crumbs={[
          ...folders.map((name, i) => {
            const p = folders.slice(0, i + 1).join("/");
            return {
              label: name,
              onClick: () => {
                toggle(`folder:${p}`, true);
                navigate({ kind: "list", list: { type: "folder", path: p } });
              },
            };
          }),
          { label: n.title || "Untitled" },
        ]}
        right={
          <>
            {mode === "reading" ? (
              <span className="mr-1 rounded-sm bg-ink-soft px-1.5 py-0.5 text-[11.5px] font-medium text-ink-strong">
                Reading
              </span>
            ) : null}
            <span className="mr-1 whitespace-nowrap max-[720px]:hidden">
              Edited {relativeTime(n.modified).replace(/^[A-Z]/, (c) => c.toLowerCase())}
            </span>
            <ModeSwitch />
            <div className="relative order-last size-7 pointer-coarse:size-11">
              <NoteMenu path={n.path} active title={n.title || "Untitled"} />
            </div>
          </>
        }
      />
      {opened ? <OpenedNoteStrip /> : null}
      <article className="mx-auto w-full max-w-[740px] px-14 pt-10 pb-[140px] max-[720px]:px-5 max-[720px]:pt-5">
        <NoteEditor key={n.path} note={n} between={<PropertiesSection note={n} />} />
        {panels
          .filter((p) => !p.when || p.when(n))
          .map((p) => (
            <p.component key={p.id} note={n} />
          ))}
      </article>
    </>
  );
}
