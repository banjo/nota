import { CheckIcon } from "lucide-react";
import { DropdownMenu as Menu } from "radix-ui";
import type * as React from "react";
import { cn } from "../lib/utils.ts";

export const DropdownMenu = (props: React.ComponentProps<typeof Menu.Root>) => (
  <Menu.Root data-slot="dropdown-menu" {...props} />
);
export const DropdownMenuTrigger = (props: React.ComponentProps<typeof Menu.Trigger>) => (
  <Menu.Trigger data-slot="dropdown-menu-trigger" {...props} />
);
export const DropdownMenuGroup = (props: React.ComponentProps<typeof Menu.Group>) => (
  <Menu.Group data-slot="dropdown-menu-group" {...props} />
);
export const DropdownMenuRadioGroup = (props: React.ComponentProps<typeof Menu.RadioGroup>) => (
  <Menu.RadioGroup data-slot="dropdown-menu-radio-group" {...props} />
);

export function DropdownMenuContent({
  className,
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof Menu.Content>) {
  return (
    <Menu.Portal>
      <Menu.Content
        data-slot="dropdown-menu-content"
        sideOffset={sideOffset}
        className={cn(
          "z-50 max-h-(--radix-dropdown-menu-content-available-height) min-w-[180px] origin-(--radix-dropdown-menu-content-transform-origin) overflow-y-auto rounded-lg bg-surface p-1 text-foreground shadow-pop data-[state=open]:animate-pop-in",
          className,
        )}
        {...props}
      />
    </Menu.Portal>
  );
}

const item =
  "relative flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-ui text-text-soft outline-none select-none pointer-coarse:h-11 pointer-coarse:text-[15px] data-[highlighted]:bg-hover data-[highlighted]:text-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&_svg]:shrink-0 [&_svg]:text-muted-foreground [&_svg:not([class*='size-'])]:size-[15px]";

export function DropdownMenuItem({
  className,
  variant = "default",
  ...props
}: React.ComponentProps<typeof Menu.Item> & { variant?: "default" | "destructive" }) {
  return (
    <Menu.Item
      data-slot="dropdown-menu-item"
      data-variant={variant}
      className={cn(
        item,
        "data-[variant=destructive]:text-danger data-[variant=destructive]:[&_svg]:text-danger",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuRadioItem({
  className,
  children,
  ...props
}: React.ComponentProps<typeof Menu.RadioItem>) {
  return (
    <Menu.RadioItem
      data-slot="dropdown-menu-radio-item"
      className={cn(item, "pr-8", className)}
      {...props}
    >
      {children}
      <span className="absolute right-2 flex size-4 items-center justify-center">
        <Menu.ItemIndicator>
          <CheckIcon className="size-3.5" />
        </Menu.ItemIndicator>
      </span>
    </Menu.RadioItem>
  );
}

export function DropdownMenuLabel({
  className,
  ...props
}: React.ComponentProps<typeof Menu.Label>) {
  return (
    <Menu.Label
      data-slot="dropdown-menu-label"
      className={cn("px-2 pt-1.5 pb-1 text-micro font-semibold text-faint", className)}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof Menu.Separator>) {
  return (
    <Menu.Separator
      data-slot="dropdown-menu-separator"
      className={cn("-mx-1 my-1 h-px bg-divider", className)}
      {...props}
    />
  );
}

export function DropdownMenuShortcut({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="dropdown-menu-shortcut"
      className={cn("ml-auto text-micro text-faint", className)}
      {...props}
    />
  );
}
