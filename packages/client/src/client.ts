import type {
  AccountApi,
  AuthApi,
  Base,
  ChangeEvent,
  CreateInput,
  HistoryEntry,
  ListQuery,
  MoveResult,
  Note,
  NoteInfo,
  OpenerApi,
  PropertyPatch,
  ServerVaultApi,
  ShellApi,
  SyncStatus,
  TagCount,
  Tree,
  VaultInfo,
  ViewInfo,
  WriteResult,
} from "./types.ts";

/**
 * Everything the UI needs from a vault. Components only ever see this
 * interface: the desktop app implements it over its Go bindings
 * (createMygoClient), the web app over the HTTP API (createHttpClient).
 */
export interface NotesClient {
  /** "desktop" or "web", for the few places that differ (copy, shortcuts). */
  readonly kind: "desktop" | "web";
  info(): Promise<VaultInfo>;
  tree(): Promise<Tree>;
  list(query?: ListQuery): Promise<NoteInfo[]>;
  /** The inbox folder, newest first. */
  inbox(): Promise<NoteInfo[]>;
  get(path: string): Promise<Note>;
  /**
   * Writes a note's body back, keeping its frontmatter. `base` is what
   * the editor started from; when the note changed since, nothing is
   * written and the result is a conflict. Without a base it overwrites.
   */
  save(path: string, body: string, base?: Base): Promise<WriteResult>;
  /** Edits frontmatter (properties and tags), checked like save. */
  patch(path: string, patch: PropertyPatch, base?: Base): Promise<WriteResult>;
  create(input: CreateInput): Promise<Note>;
  /** Moves a note into a folder ("/" is the root), keeping its name. */
  move(path: string, folder: string): Promise<MoveResult>;
  /**
   * Makes an empty folder (nested paths like "a/b" make the parents too)
   * and resolves to its path. Rejects when it exists or the name is not
   * allowed.
   */
  createFolder(path: string): Promise<string>;
  /** Gives a note a new path. */
  rename(path: string, to: string): Promise<MoveResult>;
  /** Moves a note to the trash. */
  remove(path: string): Promise<void>;
  /** Full-text search, best match first, with snippets. */
  search(query: string, filter?: ListQuery): Promise<NoteInfo[]>;
  tags(): Promise<TagCount[]>;
  views(): Promise<ViewInfo[]>;
  history(path: string): Promise<HistoryEntry[]>;
  restore(path: string, version: number): Promise<void>;
  /** Calls listener whenever notes change, from anywhere. */
  subscribe(listener: (event: ChangeEvent) => void): () => void;
  /** Background sync (desktop); absent where the server is the vault. */
  sync?: {
    status(): Promise<SyncStatus>;
    subscribe(listener: (status: SyncStatus) => void): () => void;
  };
  /** Sign in to sync (desktop). The web app's session is phase 4b. */
  account?: AccountApi;
  /** The web app's session: passkeys, devices, approvals, the account's vaults. */
  auth?: AuthApi;
  /** The server vault the web app has open, and opening another (web). */
  serverVault?: ServerVaultApi;
  /** Windows, the file manager and menu commands (desktop). */
  shell?: ShellApi;
  /** Files opened with the app from the file manager or the Dock (desktop). */
  opener?: OpenerApi;
}

/** An error from a backend, with the API's stable code when there is one. */
export class NotaError extends Error {
  readonly code: string;
  readonly status: number;
  readonly body: unknown;
  constructor(message: string, code: string, status = 0, body?: unknown) {
    super(message);
    this.name = "NotaError";
    this.code = code;
    this.status = status;
    this.body = body;
  }
}
