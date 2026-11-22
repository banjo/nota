import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// Nota's font-size utilities (styles.css) would otherwise read as text
// colours and be merged away against `text-foreground`.
const twMerge = extendTailwindMerge({
  extend: { classGroups: { "font-size": [{ text: ["ui", "small", "micro", "editor"] }] } },
});

/** Joins class names, letting later Tailwind utilities win. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
