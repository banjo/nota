import { cn } from "@nota/ui/utils";
import { BookOpen, Code2, type LucideIcon, PenLine } from "lucide-react";
import { label } from "../lib/shortcuts.ts";
import type { NoteMode } from "../lib/noteMode.ts";
import { useUI } from "../state.ts";

export const MODES: { id: NoteMode; name: string; icon: LucideIcon; hint: string }[] = [
  { id: "live", name: "Live", icon: PenLine, hint: "Edit with formatting" },
  { id: "reading", name: "Reading", icon: BookOpen, hint: "Read-only, as a finished page" },
  { id: "source", name: "Source", icon: Code2, hint: "The raw markdown" },
];

/**
 * Live / Reading / Source for the note on screen. On a phone it lives in
 * the note's "…" sheet instead (RowSheet), so the bar keeps its room.
 */
export function ModeSwitch() {
  const mode = useUI((s) => s.noteMode);
  const setMode = useUI((s) => s.setNoteMode);
  return (
    <div className="flex items-center gap-2 max-[720px]:hidden">
      <ModeGroup mode={mode} setMode={setMode} />
      <kbd
        title="Toggle between editing and reading"
        className="rounded border border-border px-1 font-sans text-[11px] leading-4 text-faint"
      >
        {label("mod+e")}
      </kbd>
    </div>
  );
}

function ModeGroup({ mode, setMode }: { mode: NoteMode; setMode: (m: NoteMode) => void }) {
  return (
    <div
      role="group"
      aria-label="View mode"
      className="flex items-center gap-0.5 rounded-md bg-hover p-0.5 text-[12px] max-[720px]:hidden"
    >
      {MODES.map((m) => {
        const on = mode === m.id;
        return (
          <button
            key={m.id}
            type="button"
            aria-pressed={on}
            aria-label={`${m.name} mode`}
            title={`${m.name}: ${m.hint}${m.id === "reading" ? ` (${label("mod+e")})` : ""}`}
            onClick={() => setMode(m.id)}
            className={cn(
              "flex h-6 items-center gap-1 rounded-[5px] px-1.5 outline-none focus-visible:ring-2 focus-visible:ring-ink/40 pointer-coarse:h-10 pointer-coarse:px-2.5",
              on ? "bg-background text-foreground shadow-xs" : "text-faint hover:text-foreground",
            )}
          >
            <m.icon className="size-3.5" strokeWidth={1.75} aria-hidden />
            <span>{m.name}</span>
          </button>
        );
      })}
    </div>
  );
}
