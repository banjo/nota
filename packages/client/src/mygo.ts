import { NotaError, type NotesClient } from "./client.ts";
import { escapePath } from "./http.ts";
import type {
  AccountState,
  ChangeEvent,
  CreateInput,
  Device,
  ExternalDoc,
  FolderInfo,
  HistoryEntry,
  TrashItem,
  ListQuery,
  MoveResult,
  Note,
  NoteInfo,
  Opened,
  OpenerApi,
  PluginInfo,
  PropertyPatch,
  ReplaceNote,
  ReplacePick,
  ReplaceQuery,
  SettingsApi,
  SettingsLayers,
  Share,
  ShareRequest,
  ShareTheme,
  ExportRequest,
  SyncStatus,
  TagCount,
  Tree,
  VaultInfo,
  VaultResult,
  ViewInfo,
  WriteResult,
} from "./types.ts";

/** A MyGo event: `events.name.on(listener)` returns an unsubscribe. */
export interface MygoEvent<T> {
  on(listener: (payload: T) => void): () => void;
}

interface OpenerSaveOutcome {
  conflict: boolean;
  path?: string;
  trashError?: string;
}

interface SaveResult {
  note?: Note | null;
  conflict: boolean;
  current?: Note | null;
}

/**
 * The generated bindings of apps/desktop (src/mygo.ts, made by `mygo
 * generate` from main.go's services). Passed in, so this package does
 * not depend on the app.
 */
export interface MygoBindings {
  Vault: {
    info(): Promise<VaultInfo>;
    tree(): Promise<Tree>;
    list(q: ListQuery): Promise<NoteInfo[] | null>;
    inbox(): Promise<NoteInfo[] | null>;
    get(path: string): Promise<Note>;
    save(path: string, body: string, baseHash: string): Promise<SaveResult>;
    patch(path: string, req: PropertyPatch, baseHash: string): Promise<SaveResult>;
    create(req: CreateInput): Promise<Note>;
    move(path: string, folder: string): Promise<MoveResult>;
    createFolder(path: string): Promise<string>;
    renameFolder(path: string, to: string): Promise<MoveResult>;
    rename(path: string, to: string): Promise<MoveResult>;
    delete(path: string): Promise<string>;
    search(q: string, filter: ListQuery): Promise<NoteInfo[] | null>;
    findReplace(q: ReplaceQuery): Promise<ReplaceNote[] | null>;
    replace(
      q: ReplaceQuery,
      picks: ReplacePick[],
    ): Promise<{
      notes: { path: string; count: number; before: string; hash: string }[] | null;
      count: number;
      stale: string[] | null;
    }>;
    tags(): Promise<TagCount[] | null>;
    views(): Promise<ViewInfo[] | null>;
    plugins(): Promise<PluginInfo[] | null>;
    history(path: string): Promise<HistoryEntry[] | null>;
    historyVersion(path: string, version: number): Promise<string | null>;
    trash(): Promise<TrashItem[] | null>;
    trashNote(id: string): Promise<string>;
    restoreTrash(id: string, folder: string): Promise<string>;
    purgeTrash(id: string): Promise<void>;
    emptyTrash(): Promise<number>;
    restore(path: string, version: number): Promise<void>;
    shares(): Promise<Share[] | null>;
    share(path: string, req: ShareRequest): Promise<Share>;
    unshare(path: string): Promise<void>;
    export(req: ExportRequest): Promise<string>;
    exportPDF(path: string, theme: ShareTheme | null): Promise<string>;
    print(path: string, theme: ShareTheme | null): Promise<void>;
    syncStatus(): Promise<SyncStatus>;
  };
  Account: {
    state(): Promise<AccountState>;
    signIn(server: string): Promise<string>;
    cancelSignIn(): Promise<void>;
    completeSignIn(link: string): Promise<void>;
    chooseVault(name: string, create: boolean): Promise<void>;
    devices(): Promise<Device[] | null>;
    revokeDevice(id: number): Promise<void>;
    signOut(): Promise<void>;
  };
  Desktop: {
    hideCapture(): Promise<void>;
    showMain(): Promise<void>;
    revealVault(): Promise<void>;
    needsOnboarding(): Promise<boolean>;
    defaultLocation(): Promise<string>;
    vaultSource(): Promise<string>;
    pickFolder(title: string, start: string): Promise<string>;
    inspectFolder(path: string): Promise<FolderInfo>;
    checkLocation(location: string): Promise<string>;
    createVault(
      location: string,
      name: string,
      inbox: string,
      welcome: boolean,
    ): Promise<VaultResult>;
    useVault(path: string, inbox: string, welcome: boolean): Promise<VaultResult>;
    setAppearance(appearance: {
      mode: string;
      resolved?: string;
      background?: string;
      captureBackground?: string;
    }): Promise<void>;
  };
  /** Files opened with the app; absent in builds before it existed. */
  Opener?: {
    take(): Promise<RawOpened[] | null>;
    previews(): Promise<RawOpened[] | null>;
    read(id: string): Promise<ExternalDoc>;
    save(
      id: string,
      req: { folder: string; edited: boolean; body: string; hash: string; move: boolean },
    ): Promise<OpenerSaveOutcome>;
    overwriteOriginal(id: string, body: string, hash: string): Promise<OpenerSaveOutcome>;
    reveal(id: string): Promise<void>;
    dismiss(id: string): Promise<void>;
    follow(id: string, href: string): Promise<RawOpened>;
    pickFile(): Promise<void>;
  };
  /** settings.toml and the account's settings; absent in builds before they existed. */
  Settings?: {
    state(): Promise<RawSettings>;
    update(p: { set: Record<string, string>; unset: string[] }): Promise<RawSettings>;
  };
  events: {
    changed: MygoEvent<{ paths: string[] | null; seq: number }>;
    syncStatus: MygoEvent<SyncStatus>;
    account: MygoEvent<AccountState>;
    command: MygoEvent<{ id: string }>;
    opened?: MygoEvent<{ seq: number }>;
    externalChanged?: MygoEvent<{ id: string }>;
    settings?: MygoEvent<RawSettings>;
  };
}

/** SettingsLayers as Go sends them: nil maps and slices come as null. */
interface RawSettings {
  file: Record<string, string> | null;
  account: Record<string, string> | null;
  signedIn: boolean;
  path: string;
  warnings: string[] | null;
}

const layers = (r: RawSettings): SettingsLayers => ({
  file: r.file ?? {},
  account: r.account ?? null,
  signedIn: r.signedIn,
  path: r.path,
  warnings: r.warnings ?? [],
});

function settingsApi(b: MygoBindings): SettingsApi | undefined {
  const s = b.Settings;
  const ev = b.events.settings;
  if (!s || !ev) return undefined;
  return {
    get: async () => layers(await s.state()),
    update: async (p) => layers(await s.update({ set: p.set ?? {}, unset: p.unset ?? [] })),
    subscribe: (listener) => ev.on((r) => listener(layers(r))),
  };
}

/** Opened as Go sends it: kind is a plain string. */
type RawOpened = Omit<Opened, "kind"> & { kind: string };

const opened = (o: RawOpened): Opened => ({
  ...o,
  kind: o.kind === "note" || o.kind === "external" ? o.kind : "error",
});

function openerApi(b: MygoBindings): OpenerApi | undefined {
  const o = b.Opener;
  const ev = b.events.opened;
  if (!o || !ev) return undefined;
  const outcome = (r: OpenerSaveOutcome) => ({
    conflict: r.conflict,
    path: r.path || undefined,
    trashError: r.trashError || undefined,
  });
  return {
    take: async () => list(await o.take()).map(opened),
    previews: async () => list(await o.previews()).map(opened),
    read: (id) => o.read(id),
    save: async (id, req) =>
      outcome(
        await o.save(id, {
          folder: req.folder,
          edited: req.body !== undefined,
          body: req.body ?? "",
          hash: req.hash,
          move: !!req.move,
        }),
      ),
    overwriteOriginal: async (id, body, hash) => outcome(await o.overwriteOriginal(id, body, hash)),
    reveal: (id) => o.reveal(id),
    dismiss: (id) => o.dismiss(id),
    follow: async (id, href) => opened(await o.follow(id, href)),
    pick: () => o.pickFile(),
    onOpened: (listener) => ev.on(() => listener()),
    onChanged: (listener) => b.events.externalChanged?.on((p) => listener(p.id)) ?? (() => {}),
  };
}

const list = <T>(xs: T[] | null | undefined): T[] => xs ?? [];

function result(r: SaveResult): WriteResult {
  if (r.conflict) return { conflict: true, current: r.current ?? null };
  if (!r.note) throw new Error("save returned no note");
  return { conflict: false, note: r.note };
}

function platform(): string {
  const p = typeof navigator === "undefined" ? "" : navigator.platform || navigator.userAgent;
  if (/Mac|iPhone|iPad/i.test(p)) return "darwin";
  if (/Win/i.test(p)) return "windows";
  return "linux";
}

/**
 * The desktop app's notafile:// scheme (apps/desktop/files.go) at host:
 * WebView2 serves custom schemes at http://<host>.<scheme>.localhost.
 */
const fileBase = (host: string) =>
  platform() === "windows" ? `http://${host}.notafile.localhost/` : `notafile://${host}/`;

async function attach(name: string, data: Blob): Promise<string> {
  const res = await fetch(fileBase("attach") + encodeURIComponent(name), {
    method: "POST",
    body: data,
  });
  if (!res.ok) {
    const msg = (await res.text()).trim() || `${res.status} ${res.statusText}`;
    throw new NotaError(msg, res.status === 400 ? "invalid_path" : `http_${res.status}`, res.status);
  }
  return ((await res.json()) as { path: string }).path;
}

/** A NotesClient over the desktop app's Go services. */
export function createMygoClient(b: MygoBindings): NotesClient {
  const v = b.Vault;
  return {
    kind: "desktop",
    info: () => v.info(),
    tree: () => v.tree(),
    list: async (q: ListQuery = {}) => list(await v.list(q)),
    inbox: async () => list(await v.inbox()),
    get: (path) => v.get(path),
    save: async (path, body, base) => result(await v.save(path, body, base?.hash ?? "")),
    patch: async (path, patch, base) => result(await v.patch(path, patch, base?.hash ?? "")),
    create: (input) => v.create(input),
    move: (path, folder) => v.move(path, folder),
    createFolder: (path) => v.createFolder(path),
    renameFolder: (path, to) => v.renameFolder(path, to),
    rename: (path, to) => v.rename(path, to),
    remove: (path) => v.delete(path),
    trash: async () => list(await v.trash()),
    trashNote: (id) => v.trashNote(id),
    restoreTrash: (id, folder = "") => v.restoreTrash(id, folder),
    purgeTrash: (id) => v.purgeTrash(id),
    emptyTrash: () => v.emptyTrash(),
    search: async (q, filter = {}) => (q.trim() ? list(await v.search(q, filter)) : []),
    findReplace: async (q) => list(await v.findReplace(q)),
    async replace(q, picks) {
      const r = await v.replace(q, picks);
      return {
        notes: list(r.notes).map(({ hash, ...n }) => ({ ...n, base: { version: 0, hash } })),
        count: r.count,
        stale: list(r.stale),
      };
    },
    tags: async () => list(await v.tags()),
    views: async () => list(await v.views()),
    plugins: async () => list(await v.plugins()),
    history: async (path) => list(await v.history(path)),
    historyVersion: async (path, version) => (await v.historyVersion(path, version)) ?? null,
    restore: (path, version) => v.restore(path, version),
    shares: {
      list: async () => list(await v.shares()),
      put: (path, req) => v.share(path, req),
      remove: (path) => v.unshare(path),
    },
    export: {
      save: async (req) => (await v.export(req)) || null,
      pdf: async (path, theme) => (await v.exportPDF(path, theme ?? null)) || null,
      print: (path, theme) => v.print(path, theme ?? null),
    },
    fileUrl: (path, from) =>
      fileBase("vault") +
      escapePath(path) +
      (from ? `?${new URLSearchParams({ from })}` : ""),
    attach,
    subscribe: (listener: (e: ChangeEvent) => void) =>
      b.events.changed.on((c) => listener({ paths: list(c.paths) })),
    sync: {
      status: () => v.syncStatus(),
      subscribe: (listener) => b.events.syncStatus.on(listener),
    },
    account: {
      state: () => b.Account.state(),
      signIn: (server = "") => b.Account.signIn(server),
      cancelSignIn: () => b.Account.cancelSignIn(),
      completeSignIn: (link) => b.Account.completeSignIn(link),
      chooseVault: (name, create) => b.Account.chooseVault(name, create),
      devices: async () => list(await b.Account.devices()),
      revokeDevice: (id) => b.Account.revokeDevice(id),
      signOut: () => b.Account.signOut(),
      subscribe: (listener) => b.events.account.on(listener),
    },
    shell: {
      platform: platform(),
      revealVault: () => b.Desktop.revealVault(),
      hideCapture: () => b.Desktop.hideCapture(),
      showMain: () => b.Desktop.showMain(),
      setAppearance: (a) => b.Desktop.setAppearance(a),
      setup: {
        needsOnboarding: () => b.Desktop.needsOnboarding(),
        defaultLocation: () => b.Desktop.defaultLocation(),
        vaultSource: () => b.Desktop.vaultSource(),
        pickFolder: (title, start = "") => b.Desktop.pickFolder(title, start),
        inspectFolder: (path) => b.Desktop.inspectFolder(path),
        checkLocation: (location) => b.Desktop.checkLocation(location),
        createVault: (location, name, inbox, welcome) =>
          b.Desktop.createVault(location, name, inbox, welcome),
        useVault: (path, inbox, welcome) => b.Desktop.useVault(path, inbox, welcome),
      },
      onCommand: (listener) => b.events.command.on((c) => listener(c.id)),
    },
    opener: openerApi(b),
    settings: settingsApi(b),
  };
}
