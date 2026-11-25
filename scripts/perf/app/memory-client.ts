// An in-memory NotesClient for the render-count harness (docs/perf.md).
// It behaves like the desktop client: writes resolve after a short
// delay and the "watcher" then emits the changed paths, so the app's
// change subscription and query invalidation run as they do for real.

import type {
  ChangeEvent,
  CreateInput,
  ListQuery,
  MoveResult,
  Note,
  NoteInfo,
  NotesClient,
  PropertyPatch,
  Properties,
  Tree,
  ViewInfo,
  WriteResult,
} from "@nota/client";
import { NotaError } from "@nota/client";

export interface SeedNote {
  path: string;
  body: string;
  properties?: Properties;
}

const LATENCY = 2; // ms, like a local IPC round trip
const EVENT_DELAY = 30; // ms, the file watcher's debounce

const wait = <T>(v: T, ms = LATENCY) => new Promise<T>((r) => setTimeout(() => r(v), ms));
const folderOf = (p: string) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "");
const scalar = (v: unknown) => (typeof v === "string" ? v : v == null ? "" : JSON.stringify(v));
const stem = (p: string) => p.slice(p.lastIndexOf("/") + 1).replace(/\.md$/, "");

function hashOf(s: string): string {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return (h >>> 0).toString(16).padStart(8, "0");
}

function titleOf(path: string, body: string): string {
  const first = body.split("\n", 1)[0] ?? "";
  const t = first.replace(/^#+\s*/, "").trim();
  return t || stem(path);
}

function bodyTags(body: string): string[] {
  const out = new Set<string>();
  for (const m of body.matchAll(/(?:^|\s)#([A-Za-z][\w/-]*)/g)) out.add(m[1]!);
  return [...out];
}

function frontmatter(props: Properties): string {
  const keys = Object.keys(props);
  if (!keys.length) return "";
  const lines = keys.map((k) => {
    const v = props[k];
    if (Array.isArray(v)) return `${k}: [${v.map(String).join(", ")}]`;
    return `${k}: ${String(v)}`;
  });
  return `---\n${lines.join("\n")}\n---\n`;
}

interface Stored {
  path: string;
  body: string;
  properties: Properties;
  version: number;
  modified: string;
}

export function createMemoryClient(
  seed: SeedNote[],
  opts: { inbox?: string; views?: ViewInfo[]; folders?: string[] } = {},
): NotesClient {
  const inbox = opts.inbox ?? "Inbox";
  const notes = new Map<string, Stored>();
  const extraFolders = new Set<string>(opts.folders ?? []);
  const listeners = new Set<(e: ChangeEvent) => void>();
  let clock = Date.parse("2026-11-20T09:00:00Z");
  const now = () => new Date((clock += 1000)).toISOString();

  for (const s of seed)
    notes.set(s.path, {
      path: s.path,
      body: s.body,
      properties: { ...s.properties },
      version: 1,
      modified: (s.properties?.updated as string | undefined) ?? now(),
    });

  const info = (n: Stored): NoteInfo => {
    const fmTags = Array.isArray(n.properties.tags) ? n.properties.tags.map(String) : [];
    return {
      path: n.path,
      folder: folderOf(n.path),
      title: titleOf(n.path, n.body),
      tags: [...new Set([...fmTags, ...bodyTags(n.body)])],
      properties: n.properties,
      created: n.properties.created as string | undefined,
      updated: n.properties.updated as string | undefined,
      modified: n.modified,
      size: n.body.length,
    };
  };

  const full = (n: Stored): Note => {
    const content = frontmatter(n.properties) + n.body;
    const links: Note["links"] = [];
    for (const m of n.body.matchAll(/\[\[([^\]|#]+)/g)) {
      const target = m[1]!.trim();
      const to = [...notes.values()].find((o) => stem(o.path) === target)?.path ?? "";
      links.push({ from: n.path, to, target });
    }
    const backlinks: Note["backlinks"] = [];
    const me = stem(n.path);
    for (const o of notes.values())
      if (o.path !== n.path && o.body.includes(`[[${me}`))
        backlinks.push({ from: o.path, to: n.path, target: me });
    return {
      ...info(n),
      version: n.version,
      hash: hashOf(content),
      body: n.body,
      content,
      links,
      backlinks,
    };
  };

  const emit = (paths: string[]) =>
    setTimeout(() => {
      for (const l of listeners) l({ paths });
    }, EVENT_DELAY);

  const need = (path: string) => {
    const n = notes.get(path);
    if (!n) throw new NotaError(`note not found: ${path}`, "not_found", 404);
    return n;
  };

  const allFolders = () => {
    const set = new Set<string>(extraFolders);
    set.add(inbox);
    for (const n of notes.values()) {
      let f = folderOf(n.path);
      while (f) {
        set.add(f);
        f = folderOf(f);
      }
    }
    return [...set].sort();
  };

  const sortNotes = (list: NoteInfo[], sort?: string) => {
    const [field = "path", dir = "asc"] = (sort ?? "path").split(" ");
    const key = (n: NoteInfo) =>
      JSON.stringify((n as unknown as Record<string, unknown>)[field] ?? n.modified ?? "");
    list.sort((a, b) => key(a).localeCompare(key(b)));
    if (dir === "desc") list.reverse();
    return list;
  };

  const filter = (q: ListQuery = {}) => {
    let list = [...notes.values()].map(info);
    if (q.folder !== undefined)
      list = list.filter((n) => n.folder === q.folder || n.folder.startsWith(`${q.folder}/`));
    if (q.tag) list = list.filter((n) => n.tags.includes(q.tag!));
    if (q.view) {
      const v = opts.views?.find((x) => x.id === q.view);
      if (v?.tag) list = list.filter((n) => n.tags.includes(v.tag!));
    }
    for (const w of q.where ?? []) {
      const neg = w.includes("!=");
      const [k, val] = w.split(neg ? "!=" : "=");
      list = list.filter((n) => (scalar(n.properties[k!]) === val) !== neg);
    }
    sortNotes(list, q.sort);
    return q.limit ? list.slice(0, q.limit) : list;
  };

  const write = (n: Stored) => {
    n.version++;
    n.modified = now();
    if (n.properties.updated !== undefined) n.properties = { ...n.properties, updated: n.modified };
    emit([n.path]);
    return full(n);
  };

  const client: NotesClient = {
    kind: "web",
    info: () => wait({ name: "Notes", dir: "memory", inbox }),
    tree: () =>
      wait<Tree>({
        inbox,
        folders: allFolders().map((path) => ({
          path,
          name: path.slice(path.lastIndexOf("/") + 1),
          notes: [...notes.values()].filter(
            (n) => folderOf(n.path) === path || folderOf(n.path).startsWith(`${path}/`),
          ).length,
        })),
        notes: sortNotes([...notes.values()].map(info), "path"),
        cursor: 1,
      }),
    list: (q) => wait(filter(q)),
    inbox: () => wait(filter({ folder: inbox, sort: "modified desc" })),
    get: async (path) => wait(full(need(path))),
    save: async (path, body, base) => {
      const n = need(path);
      if (base && base.hash !== full(n).hash)
        return wait<WriteResult>({ conflict: true, current: full(n) });
      n.body = body;
      return wait<WriteResult>({ conflict: false, note: write(n) });
    },
    patch: async (path, patch: PropertyPatch, base) => {
      const n = need(path);
      if (base && base.hash !== full(n).hash)
        return wait<WriteResult>({ conflict: true, current: full(n) });
      const props: Properties = { ...n.properties, ...patch.set };
      for (const k of patch.unset ?? []) delete props[k];
      if (patch.tags) {
        let tags = Array.isArray(props.tags) ? props.tags.map(String) : [];
        tags = tags.filter((t) => !patch.tags!.remove?.includes(t));
        for (const t of patch.tags.add ?? []) if (!tags.includes(t)) tags.push(t);
        props.tags = tags;
      }
      n.properties = props;
      return wait<WriteResult>({ conflict: false, note: write(n) });
    },
    create: async (input: CreateInput) => {
      const folder = input.folder === "/" ? "" : (input.folder ?? inbox);
      const base = input.title?.trim() || "Untitled";
      let path = `${folder ? `${folder}/` : ""}${base}.md`;
      for (let i = 2; notes.has(path); i++) path = `${folder ? `${folder}/` : ""}${base} ${i}.md`;
      const n: Stored = {
        path,
        body: `# ${base}\n${input.body ?? ""}`,
        properties: { ...input.properties, ...(input.tags ? { tags: input.tags } : {}) },
        version: 0,
        modified: now(),
      };
      notes.set(path, n);
      return wait(write(n));
    },
    move: async (path, folder) => {
      const n = need(path);
      const to = `${folder === "/" ? "" : `${folder}/`}${path.slice(path.lastIndexOf("/") + 1)}`;
      notes.delete(path);
      n.path = to;
      notes.set(to, n);
      emit([path, to]);
      return wait<MoveResult>({ from: path, path: to, links: { count: 0, notes: [] } });
    },
    createFolder: async (path) => {
      if (allFolders().includes(path)) throw new NotaError("exists", "exists", 409);
      extraFolders.add(path);
      emit([path]);
      return wait(path);
    },
    renameFolder: async (path, to) => {
      for (const n of Array.from(notes.values()))
        // the map changes below
        if (n.path.startsWith(`${path}/`)) {
          notes.delete(n.path);
          n.path = to + n.path.slice(path.length);
          notes.set(n.path, n);
        }
      emit([path, to]);
      return wait<MoveResult>({ from: path, path: to, links: { count: 0, notes: [] } });
    },
    rename: async (path, to) => {
      const n = need(path);
      if (notes.has(to)) throw new NotaError("exists", "exists", 409);
      notes.delete(path);
      n.path = to;
      notes.set(to, n);
      emit([path, to]);
      return wait<MoveResult>({ from: path, path: to, links: { count: 0, notes: [] } });
    },
    remove: async (path) => {
      notes.delete(path);
      emit([path]);
      return wait(undefined);
    },
    search: async (query, f) => {
      const q = query.toLowerCase();
      const hits = filter({ ...f, limit: undefined })
        .filter((n) => notes.get(n.path)!.body.toLowerCase().includes(q))
        .map((n) => ({ ...n, snippet: n.title }));
      return wait(f?.limit ? hits.slice(0, f.limit) : hits);
    },
    tags: async () => {
      const counts = new Map<string, number>();
      for (const n of notes.values())
        for (const t of info(n).tags) counts.set(t, (counts.get(t) ?? 0) + 1);
      return wait([...counts].map(([tag, count]) => ({ tag, count })));
    },
    views: () =>
      wait(
        (opts.views ?? []).map((v) => ({
          ...v,
          count: filter({ view: v.id }).length,
        })),
      ),
    plugins: () => wait([]),
    history: () => wait([]),
    historyVersion: () => wait(null),
    restore: () => wait(undefined),
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
  return client;
}
