import type { IconLook } from "@nota/client";
import { cn } from "@nota/ui/utils";
import type { ReactNode } from "react";
import { iconColorVar, isEmoji } from "../lib/icons.ts";

/**
 * A folder's or view's icon in its colour: the icon's <svg>, its emoji,
 * or `fallback` (the default icon) when it has none. Sized by the
 * surrounding `[&_svg]:size-*`; `emojiClassName` sets an emoji's size.
 */
export function IconGlyph({
  look,
  fallback,
  className,
  emojiClassName = "size-[15px] text-[13px]",
}: {
  look?: IconLook;
  fallback?: ReactNode;
  className?: string;
  emojiClassName?: string;
}) {
  const color = iconColorVar(look?.color);
  if (look?.svg)
    return (
      <span
        aria-hidden
        // The sets draw with stroke-width 2; rows draw lucide-react at 1.75.
        className={cn("grid place-items-center [&_[stroke-width]]:[stroke-width:1.75]", className)}
        style={{ color }}
        // Trusted: the icon sets are embedded in Nota (internal/icons).
        dangerouslySetInnerHTML={{ __html: look.svg }}
      />
    );
  if (isEmoji(look?.icon))
    return (
      <span
        aria-hidden
        className={cn("grid place-items-center leading-none", emojiClassName, className)}
      >
        {look.icon}
      </span>
    );
  return (
    <span aria-hidden className={cn("grid place-items-center", className)} style={{ color }}>
      {fallback}
    </span>
  );
}
