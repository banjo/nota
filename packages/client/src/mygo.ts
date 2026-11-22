import type { NotesClient } from "./client.ts";
import type {
  AccountState,
  ChangeEvent,
  CreateInput,
  Device,
  HistoryEntry,
  ListQuery,
  MoveResult,
  Note,
  NoteInfo,
  PropertyPatch,
  SyncStatus,
  TagCount,
  Tree,
  VaultInfo,
  ViewInfo,
  WriteResult,
} from "./types.ts";

/** A MyGo event: `events.name.on(listener)` returns an unsubscribe. */
export interface MygoEvent<T> {
  on(listener: (payload: T) => void): () => void;
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
    rename(path: string, to: string): Promise<MoveResult>;
    delete(path: string): Promise<string>;
    search(q: string, filter: ListQuery): Promise<NoteInfo[] | null>;
    tags(): Promise<TagCount[] | null>;
    views(): Promise<ViewInfo[] | null>;
    history(path: string): Promise<HistoryEntry[] | null>;
    restore(path: string, version: number): Promise<void>;
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
  };
  events: {
    changed: MygoEvent<{ paths: string[] | null; seq: number }>;
    syncStatus: MygoEvent<SyncStatus>;
    account: MygoEvent<AccountState>;
    command: MygoEvent<{ id: string }>;
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
    rename: (path, to) => v.rename(path, to),
    remove: async (path) => {
      await v.delete(path);
    },
    search: async (q, filter = {}) => (q.trim() ? list(await v.search(q, filter)) : []),
    tags: async () => list(await v.tags()),
    views: async () => list(await v.views()),
    history: async (path) => list(await v.history(path)),
    restore: (path, version) => v.restore(path, version),
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
      onCommand: (listener) => b.events.command.on((c) => listener(c.id)),
    },
  };
}
