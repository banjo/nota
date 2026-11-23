import type { FolderInfo } from "@nota/client";

// The logic of the setup wizard (first run, Switch vault…, New vault…),
// kept out of the components so it can be tested: the step machine, the
// checks on names and paths, and what a folder's state means for the
// next step. The checks mirror internal/vault/setup.go and give the same
// messages; packages/app/tests/setup.test.ts and the Go tests run the
// same cases (internal/vault/testdata/names.json).

export type Step = "welcome" | "vault" | "inbox" | "sync" | "done";

/**
 * "setup" is the first run: welcome, vault, inbox, optional sign-in,
 * done. "switch" is Settings → Switch vault… / New vault…: the same
 * vault, inbox and done screens, without the welcome and sign-in (the
 * account belongs to the vault that is open).
 */
export type Flow = "setup" | "switch";

export const STEP_LABEL: Record<Step, string> = {
  welcome: "Welcome",
  vault: "Vault",
  inbox: "Inbox",
  sync: "Sync",
  done: "Done",
};

export function stepsFor(flow: Flow): Step[] {
  return flow === "setup"
    ? ["welcome", "vault", "inbox", "sync", "done"]
    : ["vault", "inbox", "done"];
}

/** The step after `step`, or the same one at the end. */
export function nextStep(flow: Flow, step: Step): Step {
  const s = stepsFor(flow);
  return s[Math.min(s.indexOf(step) + 1, s.length - 1)] ?? step;
}

/** The step before `step`, or null at the start (the wizard cancels there, in "switch"). */
export function prevStep(flow: Flow, step: Step): Step | null {
  const s = stepsFor(flow);
  const i = s.indexOf(step);
  return i > 0 ? (s[i - 1] ?? null) : null;
}

export type VaultMode = "create" | "open";

/** What the user has typed and chosen so far. */
export interface Draft {
  mode: VaultMode;
  name: string;
  location: string;
  /** The folder chosen in "open" mode. */
  existing: string;
  inbox: string;
  welcome: boolean;
}

export const DEFAULT_NAME = "Notes";
export const DEFAULT_INBOX = "Inbox";

export function initialDraft(
  location: string,
  mode: VaultMode = "create",
  inbox: string = DEFAULT_INBOX,
): Draft {
  return { mode, name: DEFAULT_NAME, location, existing: "", inbox, welcome: true };
}

// ---- Names ----

export type NameKind = "vault" | "inbox";

const MAX_NAME = 100;
const RESERVED = /^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i;

/**
 * Checks a vault or inbox folder name as typed: NFC-normalised and
 * trimmed, no separators, nothing a file system refuses, no leading dot,
 * no trailing dot, nothing Windows reserves, at most 100 characters.
 * `error` is "" when the name is fine.
 */
export function checkName(raw: string, kind: NameKind): { name: string; error: string } {
  const name = raw.normalize("NFC").trim();
  const bad = (error: string) => ({ name: "", error });
  if (name === "") {
    return bad(
      kind === "inbox"
        ? "Give the folder a name, or skip to use “Inbox”."
        : "Give the vault a name.",
    );
  }
  if (/[/\\]/.test(name)) {
    return bad(
      kind === "inbox"
        ? "Use a single folder name, without slashes."
        : "A name can’t contain slashes. Use Location to choose where it goes.",
    );
  }
  const found = [...new Set(name.match(/[:*?"<>|]/g) ?? [])];
  if (found.length > 0) return bad(`A name can’t contain ${found.map((c) => `“${c}”`).join(" ")}.`);
  if (/\p{Cc}/u.test(name)) return bad("A name can’t contain control characters.");
  if (name.startsWith(".")) {
    return bad("A name can’t start with a dot, because the folder would be hidden.");
  }
  if (name.endsWith(".")) return bad("A name can’t end with a dot.");
  if (RESERVED.test(name.split(".")[0] ?? "")) {
    return bad(`“${name}” is reserved by the operating system.`);
  }
  if (Array.from(name).length > MAX_NAME)
    return bad("That name is too long (100 characters at most).");
  return { name, error: "" };
}

// ---- Paths ----

/** True when the path is absolute (or starts with ~) on any platform. */
export function isFullPath(p: string): boolean {
  return /^(~([/\\]|$)|\/|[A-Za-z]:[/\\]|\\\\)/.test(p);
}

/** The separator the platform's paths use. */
export function separator(platform: string): string {
  return platform === "windows" ? "\\" : "/";
}

/** Joins a location and a name the way the platform writes paths. */
export function joinPath(location: string, name: string, platform: string): string {
  const sep = separator(platform);
  const loc = location.trim();
  if (loc === "") return name;
  return loc.endsWith("/") || loc.endsWith("\\") ? loc + name : loc + sep + name;
}

/** The last element of a path. */
export function baseName(p: string): string {
  const parts = p.split(/[/\\]/).filter(Boolean);
  return parts[parts.length - 1] ?? p;
}

/** The location is checked fully by the app; this is what needs no round trip. */
export function locationError(raw: string): string {
  const p = raw.trim();
  if (p === "") return "Choose where the vault should be created.";
  if (!isFullPath(p)) return "Use a full path, starting with ~ or /.";
  return "";
}

// ---- What the folder says ----

/** The state of the vault folder the draft points at. */
export type VaultKind =
  /** Doesn't exist yet: Nota creates it. */
  | "new"
  /** Exists and is empty: Nota uses it as it is. */
  | "empty"
  /** An existing folder opened as a vault. */
  | "existing"
  /** Create mode, but the folder exists and has files in it. */
  | "conflict";

export interface VaultView {
  /** The folder the vault will be, "" until the draft is complete. */
  path: string;
  kind: VaultKind | null;
  /** Nothing is wrong and the user may continue. */
  ok: boolean;
  nameError: string;
  /** From the location field (and the app's check of it). */
  locationError: string;
  /** The folder can't be written to. */
  unwritable: boolean;
  notes: number;
  /** Files that are not notes. */
  files: number;
  /** A starter note makes sense: a brand-new or empty vault. */
  canWelcome: boolean;
  /** The folder to inspect for this draft, "" when there is none yet. */
  target: string;
}

const EMPTY_VIEW: VaultView = {
  path: "",
  kind: null,
  ok: false,
  nameError: "",
  locationError: "",
  unwritable: false,
  notes: 0,
  files: 0,
  canWelcome: false,
  target: "",
};

/**
 * What the vault step shows for a draft: the path it would make, whether
 * it can proceed, and why not. `info` is InspectFolder of `target` (null
 * while it loads), `locationProblem` the app's answer to CheckLocation.
 */
export function deriveVault(
  d: Draft,
  platform: string,
  info: FolderInfo | null,
  locationProblem = "",
): VaultView {
  if (d.mode === "create") {
    const n = checkName(d.name, "vault");
    const locErr = locationError(d.location) || locationProblem;
    const view: VaultView = { ...EMPTY_VIEW, nameError: n.error, locationError: locErr };
    if (n.error || locErr) return view;
    view.path = joinPath(d.location, n.name, platform);
    view.target = view.path;
    return fromInfo(view, info, "create");
  }
  const p = d.existing.trim();
  if (p === "") return EMPTY_VIEW;
  if (!isFullPath(p))
    return { ...EMPTY_VIEW, locationError: "Use a full path, starting with ~ or /." };
  return fromInfo({ ...EMPTY_VIEW, path: p, target: p }, info, "open");
}

function fromInfo(v: VaultView, info: FolderInfo | null, mode: VaultMode): VaultView {
  if (!info) return v; // still looking
  const out = { ...v, path: info.path || v.path, files: info.files };
  if (mode === "create") {
    if (!info.exists) return { ...out, kind: "new", ok: true, canWelcome: true };
    if (!info.isDir) return { ...out, kind: "conflict" };
    if (info.empty)
      return {
        ...out,
        kind: "empty",
        ok: info.writable,
        unwritable: !info.writable,
        canWelcome: true,
      };
    return { ...out, kind: "conflict", notes: info.notes };
  }
  if (!info.exists) return { ...out, locationError: `“${out.path}” doesn’t exist.` };
  if (!info.isDir) return { ...out, locationError: `“${out.path}” is a file, not a folder.` };
  return {
    ...out,
    kind: info.empty ? "empty" : "existing",
    ok: info.writable,
    unwritable: !info.writable,
    notes: info.notes,
    canWelcome: info.empty,
  };
}

/** The line under an opened folder: what Nota found and what it will (not) do. */
export function notesLine(v: VaultView): string {
  if (v.kind === "empty" && v.files === 0) return "The folder is empty. Nota will use it as is.";
  const nothing = "Nothing is moved or rewritten; Nota only adds a .nota folder for its index.";
  if (v.notes > 0) return `Found ${plural(v.notes, "note")} (.md files). ${nothing}`;
  return `No Markdown notes found yet. ${nothing}`;
}

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

/** Whether the "Continue" of a step is enabled. */
export function canAdvance(step: Step, d: Draft, v: VaultView, signingIn = false): boolean {
  switch (step) {
    case "vault":
      return v.ok;
    case "inbox":
      return checkName(d.inbox, "inbox").error === "";
    case "sync":
      return !signingIn;
    default:
      return true;
  }
}

/** The inbox the draft ends up with: blank means the default. */
export function inboxOf(d: Draft): string {
  return d.inbox.trim() === "" ? DEFAULT_INBOX : checkName(d.inbox, "inbox").name;
}
