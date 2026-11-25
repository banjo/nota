import type { Note } from "@nota/client";
import { cn } from "@nota/ui/utils";
import { ListTree } from "lucide-react";
import { propertyCount } from "../lib/propertiesPref.ts";
import { useUI } from "../state.ts";
import { PropertiesPanel } from "./PropertiesPanel.tsx";
import { SectionChip } from "./SectionChip.tsx";

/**
 * A note's properties under its title: a quiet "Properties" chip with
 * the count, and the panel only when the device preference says show.
 * The chip is the same toggle as Settings and ⌘K "Toggle properties".
 */
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
    <SectionChip
      label="Properties"
      icon={ListTree}
      shown={shown}
      count={count}
      onToggle={onToggle}
    />
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
