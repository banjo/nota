import { XIcon } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import type * as React from "react";
import { cn } from "../lib/utils.ts";

export const Dialog = (props: React.ComponentProps<typeof DialogPrimitive.Root>) => (
  <DialogPrimitive.Root data-slot="dialog" {...props} />
);
export const DialogTrigger = (props: React.ComponentProps<typeof DialogPrimitive.Trigger>) => (
  <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />
);
export const DialogClose = (props: React.ComponentProps<typeof DialogPrimitive.Close>) => (
  <DialogPrimitive.Close data-slot="dialog-close" {...props} />
);

export function DialogOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn(
        "fixed inset-0 z-50 bg-overlay data-[state=open]:animate-fade-in max-[720px]:data-[state=closed]:animate-fade-out",
        className,
      )}
      {...props}
    />
  );
}

/**
 * A centered modal; below 720px a bottom sheet above the keyboard, clear
 * of the home indicator. `instant` skips the entrance animation, for
 * things opened by keyboard many times a day (the ⌘K palette).
 */
export function DialogContent({
  className,
  children,
  showClose = false,
  instant = false,
  overlayClassName,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  showClose?: boolean;
  instant?: boolean;
  overlayClassName?: string;
}) {
  return (
    <DialogPrimitive.Portal>
      <DialogOverlay
        className={cn(
          instant && "data-[state=open]:animate-none max-[720px]:data-[state=closed]:animate-none",
          overlayClassName,
        )}
      />
      <DialogPrimitive.Content
        data-slot="dialog-content"
        className={cn(
          "fixed top-1/2 left-1/2 z-50 w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-surface text-foreground shadow-pop outline-none",
          !instant && "data-[state=open]:animate-pop-in",
          "max-[720px]:top-auto max-[720px]:bottom-(--keyboard-inset,0px) max-[720px]:left-0 max-[720px]:max-h-[calc(var(--visual-height,100dvh)-var(--safe-top)-12px)] max-[720px]:w-full max-[720px]:max-w-none max-[720px]:translate-x-0 max-[720px]:translate-y-0 max-[720px]:overflow-y-auto max-[720px]:overscroll-contain max-[720px]:rounded-b-none max-[720px]:pr-(--safe-right) max-[720px]:pb-(--safe-bottom) max-[720px]:pl-(--safe-left)",
          !instant &&
            "max-[720px]:data-[state=closed]:animate-sheet-out max-[720px]:data-[state=open]:animate-sheet-in",
          className,
        )}
        {...props}
      >
        {children}
        {showClose ? (
          <DialogPrimitive.Close className="absolute top-3 right-3 grid size-7 place-items-center rounded-sm text-faint hover:bg-hover hover:text-foreground active:bg-hover pointer-coarse:top-1.5 pointer-coarse:right-1.5 pointer-coarse:size-11">
            <XIcon className="size-4" />
            <span className="sr-only">Close</span>
          </DialogPrimitive.Close>
        ) : null}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex flex-col gap-1 px-5 pt-5", className)}
      {...props}
    />
  );
}
export function DialogFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn("flex justify-end gap-2 px-5 pb-5", className)}
      {...props}
    />
  );
}
export function DialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn("text-[15px] font-semibold", className)}
      {...props}
    />
  );
}
export function DialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn("text-ui text-muted-foreground", className)}
      {...props}
    />
  );
}
