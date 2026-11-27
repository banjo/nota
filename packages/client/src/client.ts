import type {
  AttachmentList,
  AccountApi,
  AuthApi,
  Base,
  ChangeEvent,
  CreateInput,
  HistoryEntry,
  TrashItem,
  ListQuery,
  MoveResult,
  ReplaceNote,
  ReplacePick,
  ReplaceQuery,
  ReplaceResult,
  Note,
  NoteInfo,
  OpenerApi,
  PluginInfo,
  PropertyPatch,
  ServerVaultApi,
  SettingsApi,
  SharesApi,
  ExportApi,
  ImportApi,
  ShellApi,
  SyncStatus,
  TagCount,
  Tree,
  VaultInfo,
  ViewInfo,
  WriteResult,
  IconColor,
  IconLook,
  IconMatch,
  IconSet,
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
  /**
   * Renames a folder (or moves it) to a new vault path: every note in it
   * moves with it. Rejects when the target exists or the name is not
   * allowed.
   */
  renameFolder(path: string, to: string): Promise<MoveResult>;
  /** Gives a note a new path. */
  rename(path: string, to: string): Promise<MoveResult>;
  /**
   * Gives a folder an icon and a colour (.nota/folders.yaml); both absent
   * removes them. Resolves to what the folder has now. Rejects for an
   * icon or colour Nota doesn't have, and a folder that isn't there.
   * Absent for files outside a vault.
   */
  setFolderIcon?(path: string, icon?: string, color?: IconColor): Promise<IconLook>;
  /** The same for a saved view: its file's `icon` and `color`. */
  setViewIcon?(id: string, icon?: string, color?: IconColor): Promise<IconLook>;
  /**
   * Icons whose name has every word of query, best first, from every set
   * or one. An empty query (and no set) gives a selection of common ones.
   */
  searchIcons?(query: string, set?: IconSet): Promise<IconMatch[]>;
  /** Moves a note to the trash; resolves to its id there, for restoreTrash. */
  remove(path: string): Promise<string>;
  /** The deleted notes, most recently deleted first. */
  trash(): Promise<TrashItem[]>;
  /** A deleted note's content. */
  trashNote(id: string): Promise<string>;
  /**
   * Puts a deleted note back where it was, or into `folder` ("/" is the
   * root), and resolves to its path. Rejects when a note has taken it.
   */
  restoreTrash(id: string, folder?: string): Promise<string>;
  /** Deletes a note in the trash for good. Absent where the server keeps every version (web). */
  purgeTrash?(id: string): Promise<void>;
  /** Deletes everything in the trash for good, resolving to how many. Absent on the web. */
  emptyTrash?(): Promise<number>;
  /** Full-text search, best match first, with snippets. */
  search(query: string, filter?: ListQuery): Promise<NoteInfo[]>;
  /** The matches of a find and replace across notes, note by note. Absent for files outside a vault. */
  findReplace?(query: ReplaceQuery): Promise<ReplaceNote[]>;
  /** Replaces the picked matches of findReplace; a note changed since is left alone (stale). */
  replace?(query: ReplaceQuery, picks: ReplacePick[]): Promise<ReplaceResult>;
  tags(): Promise<TagCount[]>;
  views(): Promise<ViewInfo[]>;
  /** The vault's plugins (.nota/plugins), broken ones with their error. */
  plugins(): Promise<PluginInfo[]>;
  /** The versions of a note on the sync server, newest first, through renames. */
  history(path: string): Promise<HistoryEntry[]>;
  /** The content of one version; null when that version is a delete. */
  historyVersion(path: string, version: number): Promise<string | null>;
  /** Makes an old version the newest one (a new version: history is kept). */
  restore(path: string, version: number): Promise<void>;
  /**
   * The URL an <img> loads a vault file from. With `from` (a note's
   * path), `path` is an embed's target (![[name]]), found from that note
   * as Obsidian does. Absent where the vault's files can't be shown.
   */
  fileUrl?(path: string, from?: string): string;
  /**
   * Saves an image pasted or dropped into a note to the vault's
   * Attachments folder, under a free name, and resolves to its path.
   */
  attach?(name: string, data: Blob): Promise<string>;
  /** The files in the attachments folder, with the notes that use each one. */
  attachments?(): Promise<AttachmentList>;
  /**
   * Shared notes, on the sync server: the web app's vault, a synced
   * vault on the desktop (which rejects while sync is off).
   */
  shares?: SharesApi;
  /** Exporting notes as HTML, PDF, Markdown or a zip. */
  export?: ExportApi;
  /** Importing notes from a folder, a zip or Apple Notes. */
  import?: ImportApi;
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
  /** The user's settings from settings.toml (desktop) and the account. */
  settings?: SettingsApi;
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
