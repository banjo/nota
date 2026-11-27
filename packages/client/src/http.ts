import { NotaError, type NotesClient } from "./client.ts";
import { zip, type ZipEntry } from "./zip.ts";
import type {
  IconLook,
  IconMatch,
  AttachmentList,
  AuthApi,
  Base,
  ChangeEvent,
  CreateInput,
  ExportRequest,
  ImportReport,
  ShareTheme,
  HistoryEntry,
  TrashItem,
  ListQuery,
  MoveResult,
  Note,
  NoteInfo,
  PluginInfo,
  PropertyPatch,
  ReplaceNote,
  ReplacePick,
  ReplaceQuery,
  ReplaceResult,
  SettingsApi,
  SettingsLayers,
  Share,
  ShareRequest,
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

/** What a server that predates link updates answers for a move. */
const noLinks = { count: 0, notes: [] };

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
      /** Sent as is (a file), instead of body as JSON. */
      raw?: Blob;
      headers?: Record<string, string>;
      /** The path is under the server, not the vault (the account's routes). */
      account?: boolean;
      /** Resolves to the response itself, for a body that isn't JSON. */
      response?: boolean;
    } = {},
  ): Promise<T> {
    const url = new URL((init.account ? root : vaultRoot) + path);
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
        body: init.raw ?? (init.body === undefined ? undefined : JSON.stringify(init.body)),
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
    if (init.response) return res as T;
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  /** An export as a file: its content and the name the server gives it. */
  async function exported(req: ExportRequest): Promise<{ blob: Blob; name: string }> {
    const res = await request<Response>("POST", "/export", { body: req, response: true });
    return {
      blob: await res.blob(),
      name: dispositionName(res.headers.get("Content-Disposition")) ?? `export.${req.format}`,
    };
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

  // The web app has no settings.toml: only the account layer. The
  // revision tells the stream's nudges apart from what is known.
  type AccountSettings = { rev: number; settings: Record<string, string> | null };
  let rev = -1;
  const layers = (s: AccountSettings): SettingsLayers => {
    rev = s.rev;
    return { file: {}, account: s.settings ?? {}, signedIn: true, path: "", warnings: [] };
  };
  const settings: SettingsApi = {
    get: async () =>
      layers(await request<AccountSettings>("GET", "/v1/account/settings", { account: true })),
    update: async (patch) =>
      layers(
        await request<AccountSettings>("PATCH", "/v1/account/settings", {
          account: true,
          body: patch,
        }),
      ),
    subscribe(listener) {
      const es = openEvents(`${root}/v1/account/events?protocol=${PROTOCOL}`);
      es.addEventListener("settings", (e) => {
        let next: number | undefined;
        try {
          next = (JSON.parse(e.data) as { rev?: number }).rev;
        } catch {
          next = undefined;
        }
        if (next === rev) return;
        void settings.get().then(listener, () => {});
      });
      return () => es.close();
    },
  };

  return {
    kind: "web",
    settings,
    ...(options.auth ? { auth: options.auth } : {}),
    ...(options.openVault ? { serverVault: { name: options.vault, open: options.openVault } } : {}),
    async info(): Promise<VaultInfo> {
      const tree = await request<Tree>("GET", "/tree");
      return { name: options.vault, dir: `${root}/${options.vault}`, inbox: tree.inbox };
    },
    tree: () => request<Tree>("GET", "/tree"),
    fileUrl: (path, from) => {
      const q = new URLSearchParams({ protocol: PROTOCOL });
      if (from) q.set("from", from);
      return `${vaultRoot}/files/${escapePath(path)}?${q}`;
    },
    attach: async (name, data) =>
      (
        await request<{ path: string }>("POST", "/attachments", {
          query: { name },
          raw: data,
          headers: { "Content-Type": data.type || "application/octet-stream" },
        })
      ).path,
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
      const r = await request<MoveResult>("POST", "/move", {
        body: { path, folder: folder || "/" },
      });
      return { from: r.from, path: r.path, links: r.links ?? noLinks };
    },
    async createFolder(path: string): Promise<string> {
      return (await request<{ path: string }>("POST", "/folders", { body: { path } })).path;
    },
    async renameFolder(path: string, to: string): Promise<MoveResult> {
      const r = await request<MoveResult>("POST", "/folders/rename", {
        body: { path, to },
      });
      return { from: r.from, path: r.path, links: r.links ?? noLinks };
    },
    setFolderIcon: (path, icon, color) =>
      request<IconLook>("PUT", "/folders/icon", { body: { path, icon, color } }),
    setViewIcon: (id, icon, color) =>
      request<IconLook>("PUT", `/views/${encodeURIComponent(id)}/icon`, { body: { icon, color } }),
    searchIcons: async (q, set) =>
      (await request<{ icons: IconMatch[] }>("GET", "/icons", { query: { q, set } })).icons,
    async rename(path: string, to: string): Promise<MoveResult> {
      const r = await request<MoveResult>("POST", "/move", {
        body: { path, to },
      });
      return { from: r.from, path: r.path, links: r.links ?? noLinks };
    },
    async remove(path: string) {
      await request("DELETE", `/notes/${escapePath(path)}`);
      // The server keeps one deleted note per path: its path is its id in the trash.
      return path;
    },
    trash: async () => (await request<{ items: TrashItem[] }>("GET", "/trash")).items,
    attachments: () => request<AttachmentList>("GET", "/attachments"),
    trashNote: async (id: string) =>
      (await request<{ content: string }>("GET", `/trash/${escapePath(id)}`)).content,
    restoreTrash: async (id: string, folder?: string) =>
      (await request<{ path: string }>("POST", "/trash/restore", { body: { id, folder } })).path,
    async search(query: string, filter?: ListQuery) {
      if (!query.trim()) return [];
      return (
        await request<{ notes: NoteInfo[] }>("GET", "/search", {
          query: { q: query, ...listParams(filter) },
        })
      ).notes;
    },
    findReplace: async (q: ReplaceQuery) =>
      (await request<{ notes: ReplaceNote[] }>("POST", "/replace/preview", { body: q })).notes,
    async replace(q: ReplaceQuery, picks: ReplacePick[]): Promise<ReplaceResult> {
      const r = await request<{
        notes: { path: string; count: number; before: string; hash: string; version: number }[];
        count: number;
        stale: string[];
      }>("POST", "/replace", { body: { ...q, picks } });
      return {
        notes: r.notes.map(({ hash, version, ...n }) => ({ ...n, base: { version, hash } })),
        count: r.count,
        stale: r.stale,
      };
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
    async historyVersion(path: string, version: number) {
      const v = await request<{ content?: string | null }>("GET", `/history/${escapePath(path)}`, {
        query: { version: String(version) },
      });
      return v.content ?? null;
    },
    async restore(path: string, version: number) {
      await request("POST", `/history/${escapePath(path)}/restore`, { body: { version } });
    },
    shares: {
      list: () => request<Share[]>("GET", "/shares"),
      put: (path: string, req: ShareRequest) =>
        request<Share>("PUT", `/shares/${escapePath(path)}`, { body: req }),
      remove: async (path: string) => {
        await request("DELETE", `/shares/${escapePath(path)}`);
      },
    },
    export: {
      async save(req: ExportRequest) {
        const { blob, name } = await exported(req);
        download(blob, name);
        return name;
      },
      async print(path: string, theme?: ShareTheme) {
        const { blob } = await exported({ path, format: "html", theme });
        await printPage(await blob.text());
      },
    },
    import: {
      async upload(files: File[]) {
        const one = files.length === 1 && /\.zip$/i.test(files[0]!.name) ? files[0]! : null;
        const body = one ?? (await zip(files.filter((f) => !hidden(relPath(f))).map(entry)));
        return request<ImportReport>("POST", "/import", {
          query: { name: one?.name },
          raw: body,
          headers: { "Content-Type": "application/zip" },
        });
      },
    },
    subscribe(listener: (event: ChangeEvent) => void) {
      const es = openEvents(`${vaultRoot}/events?protocol=${PROTOCOL}`);
      es.addEventListener("change", () => listener({ paths: [] }));
      return () => es.close();
    },
  };
}

/** The file name of a Content-Disposition header, as RFC 6266 writes it. */
export function dispositionName(header: string | null): string | undefined {
  if (!header) return undefined;
  const star = /filename\*\s*=\s*([^']*)'[^']*'([^;]+)/i.exec(header);
  if (star) {
    try {
      return decodeURIComponent(star[2]!.trim());
    } catch {
      /* fall through to the plain name */
    }
  }
  const plain = /filename\s*=\s*("((?:[^"\\]|\\.)*)"|[^;]+)/i.exec(header);
  if (!plain) return undefined;
  return plain[2] !== undefined ? plain[2].replace(/\\(.)/g, "$1") : plain[1]!.trim();
}

/** Hands a file to the browser as a download. */
/** A picked file's path: in its folder when a folder was picked. */
const relPath = (f: File) =>
  (f as File & { webkitRelativePath?: string }).webkitRelativePath || f.name;

/** .obsidian/, .DS_Store and the like, which an import leaves out. */
const hidden = (p: string) => p.split("/").some((s) => s.startsWith("."));

const entry = (f: File): ZipEntry => ({
  name: relPath(f),
  data: f,
  modified: new Date(f.lastModified),
});

function download(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.style.display = "none";
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

/** Prints a page of HTML through a hidden frame and the browser's print dialog. */
function printPage(html: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const frame = document.createElement("iframe");
    frame.setAttribute("aria-hidden", "true");
    frame.style.cssText = "position:fixed;width:0;height:0;border:0;right:0;bottom:0";
    frame.onload = () => {
      const win = frame.contentWindow;
      if (!win) {
        frame.remove();
        reject(new Error("couldn't open the page to print"));
        return;
      }
      win.addEventListener("afterprint", () => setTimeout(() => frame.remove(), 1000));
      win.focus();
      win.print();
      resolve();
    };
    frame.srcdoc = html;
    document.body.append(frame);
  });
}
