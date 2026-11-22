import type * as React from "react";
import { cn } from "../lib/utils.ts";

export function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "h-8 w-full min-w-0 rounded-md bg-surface px-2.5 text-ui text-foreground shadow-card outline-none transition-shadow duration-150 placeholder:text-faint",
        "focus-visible:shadow-[0_0_0_1px_var(--ink),0_0_0_4px_var(--ink-soft)] focus-visible:outline-none",
        "disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:shadow-[0_0_0_1px_var(--danger)]",
        className,
      )}
      {...props}
    />
  );
}
