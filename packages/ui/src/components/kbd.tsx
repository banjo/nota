import type * as React from "react";
import { cn } from "../lib/utils.ts";

export function Kbd({ className, ...props }: React.ComponentProps<"kbd">) {
  return (
    <kbd
      data-slot="kbd"
      className={cn("font-sans text-micro tracking-wide text-faint", className)}
      {...props}
    />
  );
}
