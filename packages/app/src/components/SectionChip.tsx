import { cn } from "@nota/ui/utils";
import { ChevronRight, type LucideIcon } from "lucide-react";

/**
 * A quiet toggle for a section of a note (Properties, Linked from): a
 * chevron, the label and the count when it is closed, in the look of the
 * mode switch, and big enough for a thumb.
 */
export function SectionChip({
  label,
  icon: Icon,
  shown,
  count,
  onToggle,
  controls,
}: {
  label: string;
  icon: LucideIcon;
  shown: boolean;
  count: number;
  onToggle: () => void;
  controls?: string;
}) {
  const lower = label.toLowerCase();
  return (
    <button
      type="button"
      aria-expanded={shown}
      aria-controls={shown ? controls : undefined}
      aria-label={shown ? `Hide ${lower}` : `Show ${lower} (${count})`}
      title={shown ? `Hide ${lower}` : `Show ${lower}`}
      onClick={onToggle}
      className="-ml-1.5 flex h-6 items-center gap-1 rounded-md px-1.5 text-[12px] text-faint outline-none select-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ink/40 pointer-coarse:h-10 pointer-coarse:px-2.5"
    >
      <ChevronRight
        className={cn("size-3 transition-transform duration-150 ease-out", shown && "rotate-90")}
        strokeWidth={2}
        aria-hidden
      />
      <Icon className="size-3.5" strokeWidth={1.75} aria-hidden />
      <span>{label}</span>
      {!shown && count > 0 ? (
        <span className="tabular-nums text-faint" aria-hidden>
          {count}
        </span>
      ) : null}
    </button>
  );
}
