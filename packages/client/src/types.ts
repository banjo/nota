// Shapes shared by every NotesClient. They mirror the web API
// (docs/api.md) and the desktop app's Go bindings (apps/desktop), so
// both backends hand the UI the same data.

/** A frontmatter value as the index decodes it (YAML → JSON). */
export type PropertyValue =
  | string
  | number
  | boolean
  | null
  | PropertyValue[]
  | { [key: string]: PropertyValue };

/** Frontmatter as decoded: values are PropertyValues, typed loosely. */
export type Properties = Record<string, unknown>;

/** A note in lists, the tree and search results: metadata, no content. */
export interface NoteInfo {
  /** Vault-relative path, e.g. "Projekt/Nota/Sync-protokoll.md". */
  path: string;
  /** The folder ("" for the vault root). */
  folder: string;
  title: string;
  tags: string[];
  properties: Properties;
  /** RFC 3339; from frontmatter, else the file's modification time. */
  created?: string;
  updated?: string;
  modified: string;
  size: number;
  /** The matching text, for search results. */
  snippet?: string;
}

/** A link between two notes; `to` is "" when it does not resolve. */
export interface Link {
  from: string;
  to: string;
  target: string;
  heading?: string;
  alias?: string;
}

/** One note with its content, version and links. */
export interface Note extends NoteInfo {
  /** The server version (web); 0 on the desktop. */
  version: number;
  /** SHA-256 of the whole file. */
  hash: string;
  /** The markdown after the frontmatter. */
  body: string;
  /** The whole file. */
  content: string;
  backlinks: Link[];
  links: Link[];
}

/** What a save or a frontmatter edit is checked against. */
export interface Base {
  version: number;
  hash: string;
}

export interface TreeFolder {
  path: string;
  name: string;
  /** Notes in the folder, subfolders included. */
  notes: number;
}

export interface Tree {
  /** The inbox folder, e.g. "Inbox". */
  inbox: string;
  folders: TreeFolder[];
  notes: NoteInfo[];
  cursor: number;
}

export interface ListQuery {
  folder?: string;
  tag?: string;
  /** "key=value" or "key!=value". */
  where?: string[];
  /** A saved view's id. */
  view?: string;
  /** "path|title|created|updated|modified [asc|desc]". */
  sort?: string;
  limit?: number;
}

export interface TagCount {
  tag: string;
  count: number;
}

/** A saved view from .nota/views/*.yaml (a virtual folder). */
export interface ViewInfo {
  id: string;
  name: string;
  icon?: string;
  where?: Properties;
  tag?: string;
  folder?: string;
  sort?: string;
  file: string;
  count: number;
}

export interface CreateInput {
  title?: string;
  body?: string;
  /** Defaults to the inbox; "/" is the vault root. */
  folder?: string;
  tags?: string[];
  properties?: Properties;
}

/** A frontmatter edit: only the touched lines change. */
export interface PropertyPatch {
  set?: Properties;
  unset?: string[];
  tags?: { add?: string[]; remove?: string[] };
}

/**
 * The result of a checked write. On a conflict nothing was written and
 * `current` is the note as it is now (null when it was deleted).
 */
export type WriteResult =
  | { conflict: false; note: Note }
  | { conflict: true; current: Note | null };

export interface MoveResult {
  from: string;
  path: string;
}

export interface HistoryEntry {
  path: string;
  version: number;
  hash: string;
  size: number;
  deleted: boolean;
  device: string;
  modifiedAt: string;
  movedFrom?: string;
}

/** What changed; the UI refetches what it shows. */
export interface ChangeEvent {
  /** Changed paths when known (desktop); empty means "anything". */
  paths: string[];
}

export interface SyncStatus {
  /** "off", "starting", "synced" or "error". */
  state: string;
  remote?: string;
  vault?: string;
  cursor: number;
  changed: boolean;
  conflicts: string[];
  error?: string;
  time: string;
}

export interface VaultInfo {
  name: string;
  /** Where the vault lives: a folder on the desktop, a server vault on the web. */
  dir: string;
  inbox: string;
}

export interface AccountState {
  /** "signed-out", "pending", "choose-vault", "signed-in" or "error". */
  state: string;
  server: string;
  email?: string;
  device: string;
  vault?: string;
  vaults: { name: string; cursor: number; created: string }[];
  suggested?: string;
  connectUrl?: string;
  /** Where the browser signs in (the web app's origin). */
  web?: string;
  error?: string;
}

export interface Device {
  id: number;
  device: string;
  createdAt: string;
  lastUsedAt?: string;
  current: boolean;
}

/** Signing in to sync, on clients that have it (the desktop app). */
export interface AccountApi {
  state(): Promise<AccountState>;
  /** Opens the sign-in page in the browser; resolves to its URL. */
  signIn(server?: string): Promise<string>;
  cancelSignIn(): Promise<void>;
  /** Completes sign-in from a nota://auth link (also arrives by itself). */
  completeSignIn(link: string): Promise<void>;
  chooseVault(name: string, create: boolean): Promise<void>;
  devices(): Promise<Device[]>;
  revokeDevice(id: number): Promise<void>;
  signOut(): Promise<void>;
  subscribe(listener: (state: AccountState) => void): () => void;
}

/** What only a desktop shell can do. */
export interface ShellApi {
  platform: string;
  revealVault(): Promise<void>;
  hideCapture(): Promise<void>;
  showMain(): Promise<void>;
  /** Menu items and global shortcuts, as command ids. */
  onCommand(listener: (id: string) => void): () => void;
  /** The windows' native appearance and the colours behind the page. */
  setAppearance?(appearance: ShellAppearance): Promise<void>;
  /** Choosing the vault: first-run setup, switching and creating (desktop). */
  setup?: SetupApi;
}

/** What InspectFolder found in a folder. */
export interface FolderInfo {
  /** The cleaned absolute path. */
  path: string;
  exists: boolean;
  isDir: boolean;
  /** Nothing in it at all. */
  empty: boolean;
  /** `.md` files in it (subfolders included, hidden folders left out), and other files. */
  notes: number;
  files: number;
  /** The count stopped early: the folder is huge. */
  truncated: boolean;
  /** Nota can create its `.nota` folder in it. */
  writable: boolean;
}

/** The vault the setup made or chose. The app relaunches to open it. */
export interface VaultResult {
  path: string;
  name: string;
}

/**
 * Choosing where the notes live. The desktop app asks on first run and
 * from Settings (Switch vault…, New vault…); the web app has none of it
 * (the server is the vault), so its client has no `shell.setup`.
 */
export interface SetupApi {
  /** The app started without a vault: show the setup instead of the notes. */
  needsOnboarding(): Promise<boolean>;
  /** Where a new vault goes by default (`~/Documents`, else the home folder). */
  defaultLocation(): Promise<string>;
  /** What chose the open vault: "config", "env" (`NOTA_VAULT`) or "default". */
  vaultSource(): Promise<string>;
  /** The system's folder chooser; "" when cancelled. Rejects when there is none. */
  pickFolder(title: string, start?: string): Promise<string>;
  inspectFolder(path: string): Promise<FolderInfo>;
  /** Why a folder can't hold a new vault; "" when it can. */
  checkLocation(location: string): Promise<string>;
  /** Makes `<location>/<name>` and opens it (the app relaunches). */
  createVault(
    location: string,
    name: string,
    inbox: string,
    welcome: boolean,
  ): Promise<VaultResult>;
  /** Opens an existing folder as it is (the app relaunches). */
  useVault(path: string, inbox: string, welcome: boolean): Promise<VaultResult>;
}

/** What the native windows need of the theme. */
export interface ShellAppearance {
  /** The choice: "system", "light" or "dark". */
  mode: string;
  /** The mode in effect. */
  resolved: "light" | "dark";
  /** The main window's page colour and the capture panel's, as #rrggbb. */
  background: string;
  captureBackground: string;
}

// The web app's account (docs/api.md, "Accounts"): passkeys, recovery
// codes, devices and the approvals behind /app/connect.

export interface SessionUser {
  id: number;
  email: string;
  createdAt: string;
}

/** The signed-in browser session. */
export interface AuthSession {
  user: SessionUser;
  /** Signed in with a recovery code: a new passkey is required first. */
  needsPasskey: boolean;
  passkeys: number;
  recoveryCodesLeft: number;
}

export interface AuthConfig {
  signup: "invite" | "open" | "closed";
  rpId: string;
  passkeys: boolean;
}

export interface PasskeyInfo {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
}

/** A pending CLI sign-in, by its user code. */
export interface PendingCode {
  id: number;
  device: string;
  createdAt: string;
  expiresAt: string;
}

/**
 * The web app's open vault. Listing, creating, renaming and deleting
 * the account's vaults is on AuthApi; this is which one is open.
 */
export interface ServerVaultApi {
  /** The vault's name as this page opened it. */
  name: string;
  /** Opens another vault (the page reloads); null forgets the choice and asks. */
  open(name: string | null): void;
}

export interface VaultSummary {
  name: string;
  cursor: number;
  createdAt: string;
}

/**
 * The web app's account API. It lives on the HTTP client only: the
 * desktop app signs in through browser + PKCE (AccountApi) and the CLI
 * through device codes.
 */
export interface AuthApi {
  config(): Promise<AuthConfig>;
  /** null when there is no session. Throws `unreachable` when offline. */
  session(): Promise<AuthSession | null>;
  register(input: {
    email: string;
    invite?: string;
    /** The passkey's name, e.g. "MacBook Air". */
    name: string;
  }): Promise<{ user: SessionUser; recoveryCodes: string[] }>;
  login(email?: string): Promise<AuthSession>;
  recovery(email: string, code: string): Promise<AuthSession>;
  addPasskey(name: string): Promise<PasskeyInfo[]>;
  passkeys(): Promise<PasskeyInfo[]>;
  removePasskey(id: string): Promise<void>;
  logout(): Promise<void>;
  devices(): Promise<Device[]>;
  revokeDevice(id: number): Promise<void>;
  /** A pending `nota login` by its user code. */
  lookupCode(code: string): Promise<PendingCode>;
  approveCode(code: string, approve: boolean): Promise<void>;
  /** Approves a desktop sign-in and gets the nota://auth redirect. */
  approveDesktop(input: {
    challenge: string;
    device: string;
    redirect: string;
    state?: string;
  }): Promise<{ code: string; redirect: string; expiresIn: number }>;
  vaults(): Promise<VaultSummary[]>;
  createVault(name: string): Promise<VaultSummary>;
  /**
   * Gives a vault a new name (browser session only). The old name keeps
   * leading to it, so devices that sync with it keep working.
   */
  renameVault(name: string, to: string): Promise<VaultSummary>;
  /**
   * Deletes a vault with its whole history (browser session only).
   * `confirm` is the vault's name as the user typed it; the server
   * refuses anything else.
   */
  deleteVault(name: string, confirm: string): Promise<void>;
  /** Where the web app lists this account's devices. */
  devicesUrl(): string;
  /** Fires when the session changes or expires; refetch `session()`. */
  subscribe(listener: () => void): () => void;
  /** Treats the session as gone (a 401 anywhere, or a sign-out). */
  invalidate(): void;
}

/**
 * A file the OS opened with the app (desktop): a note of the vault, an
 * outside file to preview (known by `id`), or one that can't open.
 */
export interface Opened {
  /** Names an outside file for the OpenerApi calls; "" for notes and errors. */
  id: string;
  kind: "note" | "external" | "error";
  /** The vault path, for kind "note". */
  notePath?: string;
  /** The file name, and its folder for display ("~" is the home folder). */
  name: string;
  dir: string;
  /** Why it can't open, for kind "error". */
  error?: string;
}

/** The content of an outside file, as it is on disk now. */
export interface ExternalDoc {
  id: string;
  name: string;
  dir: string;
  title: string;
  /** The whole file, and what follows its frontmatter. */
  content: string;
  body: string;
  properties: Record<string, unknown>;
  modified: string;
  size: number;
  /** SHA-256 of the file as read; Save and overwrite check it. */
  hash: string;
  /** false: not UTF-8, shown with replacement characters, can't be saved. */
  utf8: boolean;
}

/** Saving an outside file into the vault. */
export interface SaveExternal {
  /** The vault folder; "" for the inbox, "/" for the root. */
  folder: string;
  /**
   * A draft's body, replacing the file's body (its frontmatter stays
   * byte for byte). Without it the file's bytes are copied exactly.
   */
  body?: string;
  /** The hash the preview showed. */
  hash: string;
  /** Send the original to the Trash after writing the note. */
  move?: boolean;
}

export interface SaveExternalResult {
  /** The file changed since `hash`: nothing was written. */
  conflict: boolean;
  /** The new note's vault path. */
  path?: string;
  /** The note was written, but the original couldn't go to the Trash. */
  trashError?: string;
}

/**
 * Files opened with the app from the file manager, the Dock, the command
 * line or File ▸ Open… (desktop). Nothing is written to an outside file
 * except through `overwriteOriginal`.
 */
export interface OpenerApi {
  /** The files opened since the last call, oldest first. */
  take(): Promise<Opened[]>;
  /** The outside files open now (a page that loads again finds them). */
  previews(): Promise<Opened[]>;
  read(id: string): Promise<ExternalDoc>;
  /** Copies the file (or the draft) into the vault as a new note. */
  save(id: string, req: SaveExternal): Promise<SaveExternalResult>;
  /** Writes a draft's body over the original file, if it still has `hash`. */
  overwriteOriginal(id: string, body: string, hash: string): Promise<SaveExternalResult>;
  /** Shows the original in the file manager. */
  reveal(id: string): Promise<void>;
  dismiss(id: string): Promise<void>;
  /** Opens the Markdown file a relative link in the preview names. */
  follow(id: string, href: string): Promise<Opened>;
  /** The system's file chooser (File ▸ Open…); chosen files arrive through take. */
  pick(): Promise<void>;
  /** Called when files were opened: call take. */
  onOpened(listener: () => void): () => void;
}
