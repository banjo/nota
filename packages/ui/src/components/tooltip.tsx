import { Tooltip as TooltipPrimitive } from "radix-ui";
import type * as React from "react";
import { cn } from "../lib/utils.ts";

/** Wrap the app once: after the first tooltip, neighbours open at once. */
export function TooltipProvider({
  delayDuration = 500,
  skipDelayDuration = 300,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Provider>) {
  return (
    <TooltipPrimitive.Provider
      data-slot="tooltip-provider"
      delayDuration={delayDuration}
      skipDelayDuration={skipDelayDuration}
      {...props}
    />
  );
}

export const Tooltip = (props: React.ComponentProps<typeof TooltipPrimitive.Root>) => (
  <TooltipPrimitive.Root data-slot="tooltip" {...props} />
);
export const TooltipTrigger = (props: React.ComponentProps<typeof TooltipPrimitive.Trigger>) => (
  <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />
);

export function TooltipContent({
  className,
  sideOffset = 6,
  children,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        data-slot="tooltip-content"
        sideOffset={sideOffset}
        className={cn(
          "z-50 origin-(--radix-tooltip-content-transform-origin) rounded-sm bg-foreground px-2 py-1 text-small text-background data-[state=delayed-open]:animate-pop-in",
          className,
        )}
        {...props}
      >
        {children}
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  );
}
