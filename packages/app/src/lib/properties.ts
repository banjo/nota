// Frontmatter in the properties panel: which keys get their own rows,
// and the typed editors of the rest.

/** Keys with dedicated rows (or none): never in the generic list. */
export const SPECIAL_KEYS = new Set([
  "tags",
  "tag",
  "status",
  "created",
  "updated",
  "aliases",
  "alias",
  // The pinned list's order (lib/pinned.ts): the sidebar shows it.
  "pinned",
]);

export type PropType = "text" | "number" | "bool" | "date" | "list";

const DATE = /^\d{4}-\d{2}-\d{2}(?:[T ][\d:.]+(?:Z|[+-]\d{2}:?\d{2})?)?$/;

export function propType(value: unknown): PropType {
  if (typeof value === "boolean") return "bool";
  if (typeof value === "number") return "number";
  if (Array.isArray(value)) return "list";
  if (typeof value === "string" && DATE.test(value)) return "date";
  return "text";
}

/** The value a new property of a type starts with. */
export function emptyValue(type: PropType): unknown {
  switch (type) {
    case "bool":
      return false;
    case "number":
      return 0;
    case "list":
      return [];
    case "date":
      return new Date().toISOString().slice(0, 10);
    default:
      return "";
  }
}

/** Reads an editor's text back into a typed value; undefined when invalid. */
export function parseValue(type: PropType, raw: string): unknown {
  switch (type) {
    case "number": {
      const n = Number(raw.replace(",", "."));
      return raw.trim() === "" || Number.isNaN(n) ? undefined : n;
    }
    case "bool":
      return raw === "true";
    case "list":
      return raw
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
    default:
      return raw;
  }
}

export function display(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (Array.isArray(value)) return value.map(display).join(", ");
  if (typeof value === "object") return JSON.stringify(value);
  return String(value as string | number | boolean);
}

/** Statuses offered in the picker; any other text works too. */
export const STATUSES = ["draft", "in progress", "done"] as const;

export function statusColor(status: string): string {
  const s = status.toLowerCase();
  if (/done|klar|finished|complete/.test(s)) return "var(--success)";
  if (/progress|pågår|doing|active/.test(s)) return "var(--ink)";
  if (/draft|utkast|idea|idé/.test(s)) return "var(--warning)";
  return "var(--faint)";
}

export const statusLabel = (status: string) => status.charAt(0).toUpperCase() + status.slice(1);

/** A valid property name: YAML-safe, not empty. */
export const validKey = (k: string) => /^[\p{L}\p{N}_][\p{L}\p{N}_ -]*$/u.test(k.trim());
