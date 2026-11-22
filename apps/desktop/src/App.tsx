import type { Note, NotesClient, Tree } from "@nota/client";
import { useEffect, useState } from "react";
import { Editor } from "./Editor.tsx";

// Interim shell over NotesClient; packages/app replaces it.
export function App({ client }: { client: NotesClient }) {
  const [tree, setTree] = useState<Tree>();
  const [note, setNote] = useState<Note>();
  const [error, setError] = useState<string>();

  useEffect(() => {
    const load = () =>
      client
        .tree()
        .then(setTree)
        .catch((e: unknown) => setError(String(e)));
    void load();
    return client.subscribe(() => void load());
  }, [client]);

  const open = (path: string) =>
    client
      .get(path)
      .then(setNote)
      .catch((e: unknown) => setError(String(e)));

  return (
    <div className="grid h-full grid-cols-[268px_1fr]">
      <aside className="overflow-auto border-r border-hairline bg-sidebar p-2 pt-10">
        {error ? <p className="p-2 text-danger">{error}</p> : null}
        {tree?.notes.map((n) => (
          <button
            key={n.path}
            className="flex h-7 w-full items-center rounded-sm px-2 text-left text-text-soft hover:bg-hover"
            onClick={() => void open(n.path)}
          >
            {n.title}
          </button>
        ))}
      </aside>
      <main className="overflow-auto">
        {note ? (
          <div className="mx-auto max-w-[740px] px-14 py-10">
            <Editor
              key={note.path}
              doc={note.body}
              onSave={(body) =>
                void client.save(note.path, body, note).then((r) => !r.conflict && setNote(r.note))
              }
            />
          </div>
        ) : (
          <p className="p-10 text-muted-foreground">Pick a note.</p>
        )}
      </main>
    </div>
  );
}
