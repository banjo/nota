// Keyboard shortcuts as "mod+shift+i": mod is ⌘ on macOS, Ctrl elsewhere.

export const isMac = () =>
  typeof navigator !== "undefined" &&
  /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

interface Parsed {
  mod: boolean;
  shift: boolean;
  alt: boolean;
  key: string;
}

function parse(shortcut: string): Parsed {
  const parts = shortcut.toLowerCase().split("+");
  const key = parts.pop() ?? "";
  return {
    mod: parts.includes("mod"),
    shift: parts.includes("shift"),
    alt: parts.includes("alt"),
    key: key === "" ? "+" : key,
  };
}

const CODES: Record<string, string> = {
  "\\": "Backslash",
  ",": "Comma",
  "/": "Slash",
  ".": "Period",
};

export function matches(
  e: Pick<KeyboardEvent, "key" | "code" | "metaKey" | "ctrlKey" | "shiftKey" | "altKey">,
  shortcut: string,
  mac = isMac(),
): boolean {
  const p = parse(shortcut);
  const mod = mac ? e.metaKey : e.ctrlKey;
  if (p.mod !== mod || p.shift !== e.shiftKey || p.alt !== e.altKey) return false;
  if (mac ? e.ctrlKey : e.metaKey) return false;
  const code = CODES[p.key];
  if (code && e.code === code) return true;
  if (p.key.length === 1 && /[a-z]/.test(p.key) && e.code === `Key${p.key.toUpperCase()}`)
    return true;
  return e.key.toLowerCase() === p.key;
}

const MAC: Record<string, string> = { mod: "⌘", shift: "⇧", alt: "⌥", enter: "↵", escape: "esc" };
const OTHER: Record<string, string> = {
  mod: "Ctrl",
  shift: "Shift",
  alt: "Alt",
  enter: "Enter",
  escape: "Esc",
};

/** "⌘⇧I" on macOS, "Ctrl+Shift+I" elsewhere. */
export function label(shortcut: string, mac = isMac()): string {
  const names = mac ? MAC : OTHER;
  const parts = shortcut.toLowerCase().split("+");
  const key = parts.pop() ?? "";
  const out = [
    ...parts.map((p) => names[p] ?? p),
    names[key] ?? (key.length === 1 ? key.toUpperCase() : key),
  ];
  return mac ? out.join("") : out.join("+");
}
