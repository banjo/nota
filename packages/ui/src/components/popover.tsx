import { Popover as PopoverPrimitive } from "radix-ui";
import type * as React from "react";
import { cn } from "../lib/utils.ts";

export const Popover = (props: React.ComponentProps<typeof PopoverPrimitive.Root>) => (
  <PopoverPrimitive.Root data-slot="popover" {...props} />
);
export const PopoverTrigger = (props: React.ComponentProps<typeof PopoverPrimitive.Trigger>) => (
  <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />
);
export const PopoverAnchor = (props: React.ComponentProps<typeof PopoverPrimitive.Anchor>) => (
  <PopoverPrimitive.Anchor data-slot="popover-anchor" {...props} />
);

export function PopoverContent({
  className,
  align = "start",
  sideOffset = 6,
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Content>) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        data-slot="popover-content"
        align={align}
        sideOffset={sideOffset}
        className={cn(
          "z-50 w-64 origin-(--radix-popover-content-transform-origin) rounded-lg bg-surface p-1 text-foreground shadow-pop outline-none data-[state=open]:animate-pop-in",
          className,
        )}
        {...props}
      />
    </PopoverPrimitive.Portal>
  );
}
