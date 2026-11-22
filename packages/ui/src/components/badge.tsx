import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";
import type * as React from "react";
import { cn } from "../lib/utils.ts";

export const badgeVariants = cva(
  "inline-flex h-[22px] w-fit shrink-0 items-center gap-1 overflow-hidden rounded-sm px-2 text-small whitespace-nowrap [&>svg]:pointer-events-none [&>svg]:size-3",
  {
    variants: {
      variant: {
        default: "bg-chip text-chip-foreground",
        ink: "bg-ink text-ink-foreground font-semibold rounded-full px-[7px] h-[18px] text-micro tabular-nums",
        soft: "bg-ink-soft text-ink-strong",
        outline: "shadow-card text-muted-foreground",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

export function Badge({
  className,
  variant,
  asChild = false,
  ...props
}: React.ComponentProps<"span"> & VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span";
  return (
    <Comp data-slot="badge" className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}
