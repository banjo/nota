import type { Opened } from "@nota/client";

/** At most this many previews stay open; the oldest without a draft goes first. */
export const MAX_PREVIEWS = 10;

/**
 * Adds a preview to the list (or moves it to the end when it is open
 * already) and returns the new list and the ids that dropped off it.
 * Previews with a draft (`kept`) never drop.
 */
export function addPreview(
  list: Opened[],
  o: Opened,
  kept: (id: string) => boolean = () => false,
  max = MAX_PREVIEWS,
): { list: Opened[]; dropped: string[] } {
  const next = [...list.filter((x) => x.id !== o.id), o];
  const dropped: string[] = [];
  for (let i = 0; next.length > max && i < next.length - 1;) {
    const x = next[i]!;
    if (kept(x.id)) {
      i++;
      continue;
    }
    next.splice(i, 1);
    dropped.push(x.id);
  }
  return { list: next, dropped };
}

/** Where to go after closing a preview: the one opened before it, else nowhere. */
export function nextPreview(list: Opened[], closing: string): string | undefined {
  const i = list.findIndex((x) => x.id === closing);
  const rest = list.filter((x) => x.id !== closing);
  if (!rest.length) return undefined;
  return rest[Math.max(0, i - 1)]!.id;
}

/** The full path of a preview for display: its folder and name. */
export const displayPath = (o: Pick<Opened, "dir" | "name">) =>
  o.dir ? `${o.dir.replace(/[/\\]$/, "")}${o.dir.includes("\\") ? "\\" : "/"}${o.name}` : o.name;

/** Links a preview may follow: web and mail go to the browser, relative .md files open too. */
export function linkKind(href: string): "web" | "file" | "blocked" {
  const h = href.trim();
  if (/^(https?:|mailto:)/i.test(h) || /^www\./i.test(h)) return "web";
  if (/^[a-z][a-z0-9+.-]*:/i.test(h) || h.startsWith("//")) return "blocked";
  const path = h.split(/[?#]/)[0]!;
  if (!path || path.startsWith("/") || path.startsWith("\\")) return "blocked";
  return /\.(md|markdown)$/i.test(path) ? "file" : "blocked";
}
