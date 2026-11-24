import { Button } from "@nota/ui/button";
import { Kbd } from "@nota/ui/kbd";
import { cn } from "@nota/ui/utils";
import { BookOpen } from "lucide-react";
import type { ReactNode } from "react";
import { label } from "../lib/shortcuts.ts";
import { useUI } from "../state.ts";

/**
 * The strip under the top bar for files opened from outside Nota
 * (variant A of the plan): what this page is, and what can be done.
 */
export function OpenStrip({
  icon,
  tone = "ink",
  children,
  actions,
}: {
  icon: ReactNode;
  tone?: "ink" | "warn";
  children: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div
      role="status"
      data-open-strip=""
      className={cn(
        "mx-[18px] mt-1 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg px-3 py-2 text-[12.5px] max-[720px]:mx-3 pointer-coarse:text-[14px]",
        tone === "ink" ? "bg-ink-soft text-ink-strong" : "bg-chip text-foreground",
        "[&_svg]:size-4 [&_svg]:shrink-0",
      )}
    >
      <div className="flex min-w-0 flex-1 basis-[280px] items-start gap-2">
        {icon}
        <div className="min-w-0 leading-[1.35]">{children}</div>
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-1.5">{actions}</div> : null}
    </div>
  );
}

/** A note of the vault opened from the file manager: it reads first. */
export function OpenedNoteStrip() {
  const toggle = useUI((s) => s.toggleNoteMode);
  return (
    <OpenStrip
      icon={<BookOpen strokeWidth={1.75} />}
      actions={
        <Button size="sm" variant="default" onClick={toggle}>
          Edit
          <Kbd className="bg-transparent text-ink-foreground/80 pointer-coarse:hidden">
            {label("mod+e")}
          </Kbd>
        </Button>
      }
    >
      <span className="font-medium">Opened in Reading mode.</span>{" "}
      <span className="opacity-80">This note is in your vault; your usual mode is unchanged.</span>
    </OpenStrip>
  );
}
