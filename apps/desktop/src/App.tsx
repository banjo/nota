import { useEffect, useState } from "react";
import { Editor } from "./Editor.tsx";
import { Vault, type Note } from "./mygo.ts";

export function App() {
  const [notes, setNotes] = useState<Note[]>([]);
  const [current, setCurrent] = useState<string>();
  const [error, setError] = useState<string>();
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<string[] | null>(null);

  useEffect(() => {
    Vault.list()
      .then((list) => {
        setNotes(list ?? []);
        setCurrent(list?.[0]?.path);
      })
      .catch((e: unknown) => setError(String(e)));
  }, []);

  const note = notes.find((n) => n.path === current);
  const visible = hits ? notes.filter((n) => hits.includes(n.path)) : notes;
  const folders = groupByFolder(visible);

  return (
    <div className="shell">
      <aside className="side">
        <div className="ws">Antons anteckningar</div>
        <input
          className="search"
          placeholder="Sök"
          value={query}
          onChange={(e) => {
            const q = e.target.value;
            setQuery(q);
            if (!q.trim()) return setHits(null);
            Vault.search(`${q.trim()}*`)
              .then((r) => setHits(r ?? []))
              .catch(() => setHits([]));
          }}
        />
        {error && <p className="error">{error}</p>}
        {folders.map(([folder, items]) => (
          <section key={folder}>
            <div className="folder">{folder || "Inkorg"}</div>
            {items.map((n) => (
              <button
                key={n.path}
                className={n.path === current ? "row on" : "row"}
                onClick={() => setCurrent(n.path)}
              >
                {n.title}
              </button>
            ))}
          </section>
        ))}
      </aside>
      <main className="main">
        {note ? (
          <>
            <div className="bar">{note.path}</div>
            <div className="page">
              <div className="props">
                {(note.tags ?? []).map((t) => (
                  <span key={t} className="chip">
                    #{t}
                  </span>
                ))}
              </div>
              <Editor
                key={note.path}
                doc={note.body}
                onSave={(body) => void Vault.save(note.path, body)}
              />
            </div>
          </>
        ) : (
          <p className="empty">Inga anteckningar i vaulten.</p>
        )}
      </main>
    </div>
  );
}

function groupByFolder(notes: Note[]): [string, Note[]][] {
  const groups = new Map<string, Note[]>();
  for (const n of notes) {
    const i = n.path.lastIndexOf("/");
    const folder = i < 0 ? "" : n.path.slice(0, i);
    groups.set(folder, [...(groups.get(folder) ?? []), n]);
  }
  return [...groups.entries()];
}
