// Dates as people say them: "Today 10:42", "Yesterday", "18 Nov".

const time = new Intl.DateTimeFormat("en-GB", { hour: "2-digit", minute: "2-digit" });
const dayMonth = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short" });
const full = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric" });

const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();

export function relativeTime(iso: string | undefined, now = new Date()): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const days = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000);
  if (days === 0) return `Today ${time.format(d)}`;
  if (days === 1) return `Yesterday ${time.format(d)}`;
  if (d.getFullYear() === now.getFullYear()) return dayMonth.format(d);
  return full.format(d);
}

/** "18 Nov 2026" for a date or a date-time; the text itself when it is neither. */
export function formatDate(value: string): string {
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? value : full.format(d);
}
