import { ContextMenu as Menu } from "radix-ui";
import type * as React from "react";
import { cn } from "../lib/utils.ts";

// The right-click twin of dropdown-menu.tsx, in the same look.

export const ContextMenu = (props: React.ComponentProps<typeof Menu.Root>) => (
  <Menu.Root data-slot="context-menu" {...props} />
);
export const ContextMenuTrigger = (props: React.ComponentProps<typeof Menu.Trigger>) => (
  <Menu.Trigger data-slot="context-menu-trigger" {...props} />
);

export function ContextMenuContent({
  className,
  ...props
}: React.ComponentProps<typeof Menu.Content>) {
  return (
    <Menu.Portal>
      <Menu.Content
        data-slot="context-menu-content"
        className={cn(
          "z-50 max-h-(--radix-context-menu-content-available-height) min-w-[200px] origin-(--radix-context-menu-content-transform-origin) overflow-y-auto rounded-lg bg-surface p-1 text-foreground shadow-pop data-[state=open]:animate-pop-in",
          className,
        )}
        {...props}
      />
    </Menu.Portal>
  );
}

export function ContextMenuItem({
  className,
  variant = "default",
  ...props
}: React.ComponentProps<typeof Menu.Item> & { variant?: "default" | "destructive" }) {
  return (
    <Menu.Item
      data-slot="context-menu-item"
      data-variant={variant}
      className={cn(
        "relative flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-ui text-text-soft outline-none select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[highlighted]:bg-hover data-[highlighted]:text-foreground data-[variant=destructive]:text-danger data-[variant=destructive]:[&_svg]:text-danger [&_svg]:shrink-0 [&_svg]:text-muted-foreground [&_svg:not([class*='size-'])]:size-[15px]",
        className,
      )}
      {...props}
    />
  );
}

export function ContextMenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof Menu.Separator>) {
  return (
    <Menu.Separator
      data-slot="context-menu-separator"
      className={cn("-mx-1 my-1 h-px bg-divider", className)}
      {...props}
    />
  );
}
