import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";
import type * as React from "react";
import { cn } from "../lib/utils.ts";

export const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium outline-none transition-[background-color,color,transform] duration-150 ease-out select-none active:scale-[0.97] disabled:pointer-events-none disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-[15px]",
  {
    variants: {
      variant: {
        default: "bg-ink text-ink-foreground hover:bg-ink/90",
        soft: "bg-ink-soft text-ink-strong hover:bg-ink-soft-hover",
        secondary: "bg-chip text-foreground hover:bg-selected",
        outline: "bg-surface text-foreground shadow-card hover:bg-hover",
        ghost: "text-muted-foreground hover:bg-hover hover:text-foreground",
        destructive: "bg-danger text-white hover:bg-danger/90",
        link: "text-ink underline-offset-4 hover:underline active:scale-100",
      },
      size: {
        default: "h-8 px-3 text-ui",
        sm: "h-[26px] px-2.5 text-[12.5px] rounded-md",
        lg: "h-10 px-4 text-[14px] rounded-lg",
        icon: "size-8",
        "icon-sm": "size-7 rounded-sm",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<"button"> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "button";
  return (
    <Comp
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}
