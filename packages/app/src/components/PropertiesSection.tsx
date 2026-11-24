import type { Note } from "@nota/client";
import { cn } from "@nota/ui/utils";
import { ChevronRight, ListTree } from "lucide-react";
import { propertyCount } from "../lib/propertiesPref.ts";
import { useUI } from "../state.ts";
import { PropertiesPanel } from "./PropertiesPanel.tsx";

/**
 * A note's properties under its title: a quiet "Properties" chip with
 * the count, and the panel only when the device preference says show.
 * The chip is the same toggle as Settings and ⌘K "Toggle properties",
 * in the look of the mode switch, and big enough for a thumb.
 */
/** The chip: Properties, a chevron and the count when it is closed. */
export function PropertiesChip({
  shown,
  count,
  onToggle,
}: {
  shown: boolean;
  count: number;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      aria-expanded={shown}
      aria-label={shown ? "Hide properties" : `Show properties (${count})`}
      title={shown ? "Hide properties" : "Show properties"}
      onClick={onToggle}
      className="-ml-1.5 flex h-6 items-center gap-1 rounded-md px-1.5 text-[12px] text-faint outline-none select-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ink/40 pointer-coarse:h-10 pointer-coarse:px-2.5"
    >
      <ChevronRight
        className={cn("size-3 transition-transform duration-150 ease-out", shown && "rotate-90")}
        strokeWidth={2}
        aria-hidden
      />
      <ListTree className="size-3.5" strokeWidth={1.75} aria-hidden />
      <span>Properties</span>
      {!shown && count > 0 ? (
        <span className="tabular-nums text-faint" aria-hidden>
          {count}
        </span>
      ) : null}
    </button>
  );
}

export function PropertiesSection({ note }: { note: Note }) {
  const shown = useUI((s) => s.properties === "show");
  const toggle = useUI((s) => s.toggleProperties);
  const count = propertyCount(note.properties);
  return (
    <div className={cn(shown ? "mb-7" : "mb-4")}>
      <PropertiesChip shown={shown} count={count} onToggle={toggle} />
      {shown ? <PropertiesPanel note={note} /> : null}
    </div>
  );
}
