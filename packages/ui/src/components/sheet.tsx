import { Dialog as DialogPrimitive } from "radix-ui";
import * as React from "react";
import { cn } from "../lib/utils.ts";

// A bottom sheet for phones: menus, small forms and confirmations rise
// from the bottom edge, where a thumb is, instead of floating next to a
// trigger. It is a Radix dialog underneath (focus trap, Escape, aria),
// slides with the iOS drawer curve, and follows a finger down: a short
// flick or a drag past a third of its height closes it.

export const Sheet = (props: React.ComponentProps<typeof DialogPrimitive.Root>) => (
  <DialogPrimitive.Root data-slot="sheet" {...props} />
);
export const SheetTrigger = (props: React.ComponentProps<typeof DialogPrimitive.Trigger>) => (
  <DialogPrimitive.Trigger data-slot="sheet-trigger" {...props} />
);
export const SheetClose = (props: React.ComponentProps<typeof DialogPrimitive.Close>) => (
  <DialogPrimitive.Close data-slot="sheet-close" {...props} />
);

/** Flick speed (px/ms) that closes the sheet whatever the distance. */
const FLICK = 0.11;

/** Drag-to-dismiss on the sheet's grab area; returns pointer handlers. */
function useDragToClose(ref: React.RefObject<HTMLElement | null>, close: () => void) {
  const start = React.useRef<{ y: number; t: number; id: number } | null>(null);
  const reset = (el: HTMLElement) => {
    el.style.transition = "";
    el.style.transform = "";
  };
  return {
    onPointerDown(e: React.PointerEvent) {
      // One finger only: a second touch mid-drag would make it jump.
      if (start.current || e.button !== 0) return;
      start.current = { y: e.clientY, t: performance.now(), id: e.pointerId };
      (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    },
    onPointerMove(e: React.PointerEvent) {
      const s = start.current;
      const el = ref.current;
      if (!s || !el || e.pointerId !== s.id) return;
      const dy = e.clientY - s.y;
      // Upwards it gives a little, with growing friction, instead of a wall.
      const y = dy > 0 ? dy : -Math.sqrt(-dy) * 2;
      el.style.transition = "none";
      el.style.transform = `translateY(${y}px)`;
    },
    onPointerUp(e: React.PointerEvent) {
      const s = start.current;
      const el = ref.current;
      if (!s || e.pointerId !== s.id) return;
      start.current = null;
      if (!el) return;
      const dy = e.clientY - s.y;
      const v = dy / Math.max(1, performance.now() - s.t);
      if (dy > 0 && (dy > el.offsetHeight / 3 || v > FLICK)) {
        // Carry on from where the finger let go; the exit animation would
        // restart from the top, so it is skipped (data-dragged).
        let done = false;
        const finish = () => {
          if (done) return;
          done = true;
          el.dataset.dragged = "";
          close();
        };
        el.style.transition = "transform 200ms var(--ease-out)";
        el.style.transform = "translateY(100%)";
        el.addEventListener("transitionend", finish, { once: true });
        setTimeout(finish, 240);
      } else {
        el.style.transition = "transform 300ms var(--ease-drawer)";
        el.style.transform = "";
        el.addEventListener("transitionend", () => reset(el), { once: true });
      }
    },
    onPointerCancel() {
      start.current = null;
      if (ref.current) reset(ref.current);
    },
  };
}

/**
 * The sheet itself: full width, pinned to the bottom of the visible
 * viewport (above the keyboard), clear of the home indicator. `title`
 * is shown as the sheet's heading; pass `hideTitle` to keep it for
 * screen readers only.
 */
export function SheetContent({
  className,
  children,
  title,
  description,
  hideTitle = false,
  ...props
}: Omit<React.ComponentProps<typeof DialogPrimitive.Content>, "title"> & {
  title: React.ReactNode;
  description?: React.ReactNode;
  hideTitle?: boolean;
}) {
  const ref = React.useRef<HTMLDivElement>(null);
  const closeRef = React.useRef<HTMLButtonElement>(null);
  const drag = useDragToClose(ref, () => closeRef.current?.click());
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay
        data-slot="sheet-overlay"
        className="fixed inset-0 z-50 bg-overlay data-[state=closed]:animate-fade-out data-[state=open]:animate-fade-in"
      />
      <DialogPrimitive.Content
        ref={ref}
        data-slot="sheet-content"
        className={cn(
          "fixed inset-x-0 bottom-(--keyboard-inset,0px) z-50 flex max-h-[calc(var(--visual-height,100dvh)-var(--safe-top)-12px)] flex-col rounded-t-xl bg-surface pb-[max(var(--safe-bottom),8px)] text-foreground shadow-pop outline-none",
          "pr-(--safe-right) pl-(--safe-left)",
          "data-[state=closed]:animate-sheet-out data-[state=open]:animate-sheet-in data-dragged:animate-none",
          className,
        )}
        {...props}
      >
        <div
          {...drag}
          data-slot="sheet-grab"
          className="shrink-0 cursor-grab touch-none px-5 pt-2 pb-1 select-none"
        >
          <div aria-hidden className="mx-auto h-[5px] w-9 rounded-full bg-faint/40" />
          <DialogPrimitive.Title
            className={cn(
              "mt-3 truncate text-center text-[13px] font-semibold text-muted-foreground",
              hideTitle && "sr-only",
            )}
          >
            {title}
          </DialogPrimitive.Title>
          {description ? (
            <DialogPrimitive.Description className="mt-0.5 text-center text-small text-faint">
              {description}
            </DialogPrimitive.Description>
          ) : (
            <DialogPrimitive.Description className="sr-only">
              Swipe down or press Escape to close.
            </DialogPrimitive.Description>
          )}
        </div>
        <div className="min-h-0 overflow-y-auto overscroll-contain px-2 pt-1">{children}</div>
        <DialogPrimitive.Close ref={closeRef} className="sr-only" tabIndex={-1}>
          Close
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

/** A full-width 48px row in a sheet: a menu item a thumb can't miss. */
export function SheetItem({
  className,
  variant = "default",
  ...props
}: React.ComponentProps<"button"> & { variant?: "default" | "destructive" }) {
  return (
    <button
      type="button"
      data-slot="sheet-item"
      className={cn(
        "flex h-12 w-full items-center gap-3 rounded-lg px-3 text-left text-[16px] text-foreground outline-none select-none",
        "active:bg-hover focus-visible:bg-hover [&_svg]:size-5 [&_svg]:shrink-0 [&_svg]:text-muted-foreground",
        variant === "destructive" && "text-danger [&_svg]:text-danger",
        "disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export function SheetSeparator({ className }: { className?: string }) {
  return <div role="separator" className={cn("mx-3 my-1 h-px bg-divider", className)} />;
}
