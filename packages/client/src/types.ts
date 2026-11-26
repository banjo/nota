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

/** A find and replace across notes: what, and in which notes. */
export interface ReplaceQuery {
  find: string;
  replace: string;
  /** `find` is a regular expression (RE2); `$1` in `replace` is its first group. */
  regex?: boolean;
  /** Match case; ignored by default. */
  case?: boolean;
  /** Whole words only. */
  word?: boolean;
  /** Also in code blocks and inline code, which are skipped by default. */
  code?: boolean;
  folder?: string;
  tag?: string;
}

/** One place a replace would change, with the rest of its line around it. */
export interface ReplaceMatch {
  line: number;
  text: string;
  with: string;
  before: string;
  after: string;
}

/** A note with matches; `hash` is the content they were found in. */
export interface ReplaceNote {
  path: string;
  title: string;
  hash: string;
  matches: ReplaceMatch[];
}

/** The matches to replace in a note, by index into its list; all when absent. */
export interface ReplacePick {
  path: string;
  hash: string;
  matches?: number[];
}

/** A note a replace wrote: saving `before` on `base` undoes it. */
export interface ReplacedNote {
  path: string;
  count: number;
  before: string;
  base: Base;
}

export interface ReplaceResult {
  notes: ReplacedNote[];
  count: number;
  /** Notes that changed since their matches were found; left alone. */
  stale: string[];
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

/** The colour tokens of one mode of a theme, without the leading "--". */
export type ThemeTokens = Record<string, string>;

/** A colour theme a plugin contributes (docs/themes.md). */
export interface PluginTheme {
  id: string;
  name: string;
  light?: ThemeTokens;
  dark?: ThemeTokens;
}

/** A plugin from .nota/plugins/<folder>/manifest.json. */
export interface PluginInfo {
  id: string;
  name: string;
  version?: string;
  description?: string;
  author?: string;
  contributes: { themes: PluginTheme[] };
  /** The plugin's folder in the vault. */
  dir: string;
  /** Why the plugin did not load; nothing else is set then. */
  error?: string;
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

/** The links rewritten so they keep pointing at a moved note. */
export interface LinksUpdated {
  count: number;
  notes: { path: string; links: number }[];
}

export interface MoveResult {
  from: string;
  path: string;
  links: LinksUpdated;
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

/**
 * The look of a shared note: the sharer's theme as its colour tokens in
 * one mode. No tokens is Nota's own theme; no mode follows the reader.
 */
export interface ShareTheme {
  /** "light", "dark", or "" to follow the reader. */
  mode?: string;
  tokens?: Record<string, string>;
}

/** A note shared as a web page of the sync server. */
export interface Share {
  id: string;
  path: string;
  /** The link to give out. */
  url: string;
  /** Whether the link asks for a password. */
  password: boolean;
  theme?: ShareTheme;
  created: string;
  updated: string;
}

/**
 * Shares a note or changes its share: a password sets one, "" removes
 * it, and leaving it out keeps it; likewise the theme ({} is Nota's own).
 */
export interface ShareRequest {
  password?: string;
  theme?: ShareTheme;
}

/** Shared notes: pages of the sync server, one link per note. */
export interface SharesApi {
  list(): Promise<Share[]>;
  /** Shares a note (or changes its share) and resolves to its link. */
  put(path: string, req: ShareRequest): Promise<Share>;
  /** Stops sharing a note: its link leads nowhere from then on. */
  remove(path: string): Promise<void>;
}

/**
 * What an export is made of: a web page in Reading mode's look, Markdown
 * for apps that don't read Nota's syntax, or the files as they are.
 */
export type ExportFormat = "html" | "md" | "zip";

/** What to export, and how. */
export interface ExportRequest {
  /** A note's path, a folder's, or "" for the whole vault. */
  path: string;
  format: ExportFormat;
  /** The look of HTML; absent is Nota's own, in the reader's mode. */
  theme?: ShareTheme;
}

/**
 * Exports (docs/cli.md, Export): a note as one file where it can be one, a
 * folder or the vault as a zip.
 */
export interface ExportApi {
  /**
   * Exports to a file: a save dialog (desktop) or a download (web).
   * Resolves to where it went, or null when the save dialog was cancelled.
   */
  save(req: ExportRequest): Promise<string | null>;
  /**
   * A note as a PDF file (desktop). Where it is absent, print() does it:
   * the print dialog saves PDFs.
   */
  pdf?(path: string, theme?: ShareTheme): Promise<string | null>;
  /** Prints a note's page with the system's print dialog. */
  print(path: string, theme?: ShareTheme): Promise<void>;
}

/** A deleted note. */
export interface TrashItem {
  /** What the other trash calls take: its path in .trash/ (desktop) or its path (web). */
  id: string;
  /** Where it was deleted from. */
  path: string;
  deletedAt: string;
  size: number;
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

/** One layer's settings by key (internal/settings), e.g. `{ theme: "dark" }`. */
export type SettingValues = Record<string, string>;

/**
 * The layers of the user's settings that live outside the page. The
 * device layer is the page's own localStorage; the app merges them:
 * file over account over device over the defaults.
 */
export interface SettingsLayers {
  /** What settings.toml sets (desktop). These keys are locked. */
  file: SettingValues;
  /** What the account sets; null when signed out or not loaded (offline). */
  account: SettingValues | null;
  /** Whether changes go to the account. */
  signedIn: boolean;
  /** Where settings.toml is (desktop); "" where there is none. */
  path: string;
  /** Problems with the file or the account, to show in Settings. */
  warnings: string[];
}

/** Changes the account's settings key by key; the last write of a key wins. */
export interface SettingsPatch {
  set?: SettingValues;
  unset?: string[];
}

/** The user's settings outside this device: settings.toml and the account. */
export interface SettingsApi {
  get(): Promise<SettingsLayers>;
  /** Changes the account's settings. Rejects when signed out. */
  update(patch: SettingsPatch): Promise<SettingsLayers>;
  /** Calls listener whenever a layer changes (the file, another device). */
  subscribe(listener: (layers: SettingsLayers) => void): () => void;
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
  /** Called with a preview's id when its file changed on disk: read it again. */
  onChanged(listener: (id: string) => void): () => void;
}
