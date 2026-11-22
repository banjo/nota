import { useQueryClient } from "@tanstack/react-query";
import { CornerDownLeft } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { fail } from "../actions.ts";
import { useClient } from "../context.ts";
import { keys } from "../queries.ts";

/**
 * The quick-capture panel (⌥⌘N): one field, Enter saves to the inbox,
 * Escape hides. It grows with the text so a thought fits.
 */
export function CaptureApp() {
  const client = useClient();
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const [saved, setSaved] = useState(false);
  const ref = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    ref.current?.focus();
  }, []);
  // Reopening the panel should start empty, even if the window stayed hidden.
  useEffect(() => client.shell?.onCommand(() => setText("")), [client.shell]);

  const save = async () => {
    const body = text.trim();
    if (!body) return;
    setText("");
    setSaved(true);
    setTimeout(() => setSaved(false), 1400);
    try {
      await client.create({ body });
      void qc.invalidateQueries({ queryKey: keys.inbox });
      void qc.invalidateQueries({ queryKey: keys.tree });
      // Let the panel show what happened, then get out of the way.
      setTimeout(() => void client.shell?.hideCapture(), 700);
    } catch (e) {
      setText(body);
      fail("Couldn't save to the inbox", e);
    }
  };

  return (
    <div className="flex h-full flex-col rounded-xl border border-hairline bg-surface px-4 pt-4 shadow-pop">
      <div className="flex items-start gap-2.5">
        <span className="mt-[3px] size-2 shrink-0 rounded-full bg-ink" aria-hidden />
        <textarea
          ref={ref}
          aria-label="Capture a thought"
          rows={1}
          value={text}
          placeholder="Capture a thought…"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              void save();
            }
            if (e.key === "Escape") void client.shell?.hideCapture();
          }}
          className="w-full flex-1 resize-none bg-transparent text-[15px] leading-[1.5] text-foreground outline-none placeholder:text-faint"
        />
      </div>
      <div className="-mx-4 mt-1 flex items-center gap-3 border-t border-divider px-4 py-[7px] text-micro text-faint">
        <span className="flex items-center gap-1">
          <kbd className="rounded-xs bg-chip px-1 py-px font-sans text-[10.5px] text-chip-foreground">
            ↵
          </kbd>{" "}
          saves to Inbox
        </span>
        <span className="hidden items-center gap-1 [@media(min-width:420px)]:flex">
          <kbd className="rounded-xs bg-chip px-1 py-px font-sans text-[10.5px] text-chip-foreground">
            ⇧↵
          </kbd>{" "}
          new line
        </span>
        <span className="ml-auto flex items-center gap-1.5">
          {saved ? (
            <>
              <CornerDownLeft className="size-3" /> Saved
            </>
          ) : (
            <>
              <kbd className="rounded-xs bg-chip px-1 py-px font-sans text-[10.5px] text-chip-foreground">
                esc
              </kbd>{" "}
              closes
            </>
          )}
        </span>
      </div>
    </div>
  );
}
