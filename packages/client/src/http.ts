import { NotaError, type NotesClient } from "./client.ts";
import type {
  AuthApi,
  Base,
  ChangeEvent,
  CreateInput,
  HistoryEntry,
  ListQuery,
  MoveResult,
  Note,
  NoteInfo,
  PluginInfo,
  PropertyPatch,
  TagCount,
  Tree,
  VaultInfo,
  ViewInfo,
  WriteResult,
} from "./types.ts";

/** The part of EventSource the client uses, so tests can fake it. */
export interface EventSourceLike {
  addEventListener(type: string, listener: (event: { data: string }) => void): void;
  close(): void;
}

export interface HttpClientOptions {
  /** The API server, e.g. https://nota.app.banjoanton.com. */
  baseUrl: string;
  /** The vault name on the server. */
  vault: string;
  /** Defaults to globalThis.fetch. */
  fetch?: typeof fetch;
  /** Defaults to `new EventSource(url, { withCredentials: true })`. */
  eventSource?: (url: string) => EventSourceLike;
  /** The web app's account API, exposed as `client.auth`. */
  auth?: AuthApi;
  /**
   * Opens another of the account's vaults (the web app reloads into it);
   * null forgets the choice and asks. Exposed as `client.serverVault`.
   */
  openVault?: (name: string | null) => void;
  /** Called on a 401, so the web app can show sign-in again. */
  onUnauthorized?: () => void;
  /** Called when the server answers, or when it cannot be reached. */
  onStatus?: (up: boolean) => void;
}

/** The protocol version the client speaks (docs/sync-protocol.md). */
export const PROTOCOL = "1";

/** Escapes a vault path segment by segment, as protocol.EscapePath does. */
export function escapePath(p: string): string {
  return p
    .split("/")
    .map((s) => encodeURIComponent(s.normalize("NFC")))
    .join("/");
}

interface ServerEntry {
  path: string;
  version: number;
  hash: string;
  size: number;
  deleted: boolean;
  device_id: string;
  modified_at: string;
  moved_from?: string;
}

/**
 * A NotesClient over the server's web API (docs/api.md): fetch with the
 * session cookie (credentials: "include"), `Nota-Client: web` for CSRF,
 * and SSE for change nudges.
 */
export function createHttpClient(options: HttpClientOptions): NotesClient {
  const doFetch = options.fetch ?? globalThis.fetch.bind(globalThis);
  const root = options.baseUrl.replace(/\/+$/, "");
  const vaultRoot = `${root}/v1/api/vaults/${encodeURIComponent(options.vault)}`;
  const openEvents =
    options.eventSource ??
    ((url: string) => new EventSource(url, { withCredentials: true }) as EventSourceLike);

  async function request<T>(
    method: string,
    path: string,
    init: {
      query?: Record<string, string | number | string[] | undefined>;
      body?: unknown;
      headers?: Record<string, string>;
    } = {},
  ): Promise<T> {
    const url = new URL(vaultRoot + path);
    for (const [k, v] of Object.entries(init.query ?? {})) {
      if (v === undefined || v === "") continue;
      for (const item of Array.isArray(v) ? v : [v]) url.searchParams.append(k, String(item));
    }
    const headers: Record<string, string> = {
      "Nota-Protocol": PROTOCOL,
      "Nota-Client": "web",
      Accept: "application/json",
      ...init.headers,
    };
    if (init.body !== undefined) headers["Content-Type"] = "application/json";
    let res: Response;
    try {
      res = await doFetch(url.toString(), {
        method,
        headers,
        credentials: "include",
        body: init.body === undefined ? undefined : JSON.stringify(init.body),
      });
    } catch (e) {
      options.onStatus?.(false);
      throw new NotaError(
        e instanceof Error && e.message ? e.message : "the server did not answer",
        "unreachable",
      );
    }
    options.onStatus?.(true);
    if (!res.ok) {
      let body: { code?: string; message?: string } | undefined;
      try {
        body = (await res.json()) as typeof body;
      } catch {
        body = undefined;
      }
      if (res.status === 401) options.onUnauthorized?.();
      throw new NotaError(
        body?.message ?? `${res.status} ${res.statusText}`,
        body?.code ?? `http_${res.status}`,
        res.status,
        body,
      );
    }
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  const listParams = (q: ListQuery = {}) => ({
    folder: q.folder,
    tag: q.tag,
    where: q.where,
    view: q.view,
    sort: q.sort,
    limit: q.limit,
  });

  const get = (path: string) => request<Note>("GET", `/notes/${escapePath(path)}`);

  /** A 409 becomes a conflict with the note as it is now. */
  async function checked(write: () => Promise<Note>, path: string): Promise<WriteResult> {
    try {
      return { conflict: false, note: await write() };
    } catch (e) {
      if (e instanceof NotaError && e.status === 409 && e.code === "version_mismatch") {
        const current = await get(path).catch((err: unknown) => {
          if (err instanceof NotaError && err.status === 404) return null;
          throw err;
        });
        return { conflict: true, current };
      }
      throw e;
    }
  }

  return {
    kind: "web",
    ...(options.auth ? { auth: options.auth } : {}),
    ...(options.openVault ? { serverVault: { name: options.vault, open: options.openVault } } : {}),
    async info(): Promise<VaultInfo> {
      const tree = await request<Tree>("GET", "/tree");
      return { name: options.vault, dir: `${root}/${options.vault}`, inbox: tree.inbox };
    },
    tree: () => request<Tree>("GET", "/tree"),
    list: async (q) =>
      (await request<{ notes: NoteInfo[] }>("GET", "/notes", { query: listParams(q) })).notes,
    inbox: async () => (await request<{ notes: NoteInfo[] }>("GET", "/inbox")).notes,
    get,
    save: (path: string, body: string, base?: Base) =>
      checked(async () => {
        // The web API always checks: without a base, save over what is there now.
        const version = base?.version ?? (await get(path)).version;
        return request<Note>("PUT", `/notes/${escapePath(path)}`, {
          body: { body },
          headers: { "Base-Version": String(version) },
        });
      }, path),
    patch: (path: string, patch: PropertyPatch, base?: Base) =>
      checked(
        () =>
          request<Note>("PATCH", `/notes/${escapePath(path)}`, {
            body: patch,
            headers: base ? { "Base-Version": String(base.version) } : {},
          }),
        path,
      ),
    create: (input: CreateInput) => request<Note>("POST", "/notes", { body: input }),
    async move(path: string, folder: string): Promise<MoveResult> {
      const r = await request<{ from: string; path: string }>("POST", "/move", {
        body: { path, folder: folder || "/" },
      });
      return { from: r.from, path: r.path };
    },
    async createFolder(path: string): Promise<string> {
      return (await request<{ path: string }>("POST", "/folders", { body: { path } })).path;
    },
    async renameFolder(path: string, to: string): Promise<MoveResult> {
      const r = await request<{ from: string; path: string }>("POST", "/folders/rename", {
        body: { path, to },
      });
      return { from: r.from, path: r.path };
    },
    async rename(path: string, to: string): Promise<MoveResult> {
      const r = await request<{ from: string; path: string }>("POST", "/move", {
        body: { path, to },
      });
      return { from: r.from, path: r.path };
    },
    async remove(path: string) {
      await request("DELETE", `/notes/${escapePath(path)}`);
    },
    async search(query: string, filter?: ListQuery) {
      if (!query.trim()) return [];
      return (
        await request<{ notes: NoteInfo[] }>("GET", "/search", {
          query: { q: query, ...listParams(filter) },
        })
      ).notes;
    },
    tags: async () => (await request<{ tags: TagCount[] }>("GET", "/tags")).tags,
    views: async () => (await request<{ views: ViewInfo[] }>("GET", "/views")).views,
    plugins: async () => (await request<{ plugins: PluginInfo[] }>("GET", "/plugins")).plugins,
    async history(path: string): Promise<HistoryEntry[]> {
      const h = await request<{ versions: ServerEntry[] }>("GET", `/history/${escapePath(path)}`);
      return h.versions.map((e) => ({
        path: e.path,
        version: e.version,
        hash: e.hash,
        size: e.size,
        deleted: e.deleted,
        device: e.device_id,
        modifiedAt: e.modified_at,
        movedFrom: e.moved_from,
      }));
    },
    async restore(path: string, version: number) {
      await request("POST", `/history/${escapePath(path)}/restore`, { body: { version } });
    },
    subscribe(listener: (event: ChangeEvent) => void) {
      const es = openEvents(`${vaultRoot}/events?protocol=${PROTOCOL}`);
      es.addEventListener("change", () => listener({ paths: [] }));
      return () => es.close();
    },
  };
}
