import { Command as CommandPrimitive } from "cmdk";
import { SearchIcon } from "lucide-react";
import type * as React from "react";
import { cn } from "../lib/utils.ts";

export function Command({ className, ...props }: React.ComponentProps<typeof CommandPrimitive>) {
  return (
    <CommandPrimitive
      data-slot="command"
      className={cn(
        "flex h-full w-full flex-col overflow-hidden rounded-xl bg-surface text-foreground",
        className,
      )}
      {...props}
    />
  );
}

export function CommandInput({
  className,
  trailing,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Input> & { trailing?: React.ReactNode }) {
  return (
    <div
      data-slot="command-input-wrapper"
      className="flex h-[52px] items-center gap-2.5 border-b border-divider px-4 text-faint"
    >
      <SearchIcon className="size-[17px] shrink-0" strokeWidth={1.75} />
      <CommandPrimitive.Input
        data-slot="command-input"
        className={cn(
          "h-full flex-1 bg-transparent text-[15px] text-foreground outline-none placeholder:text-faint",
          className,
        )}
        {...props}
      />
      {trailing}
    </div>
  );
}

export function CommandList({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.List>) {
  return (
    <CommandPrimitive.List
      data-slot="command-list"
      className={cn(
        "max-h-[min(460px,60vh)] scroll-py-1.5 overflow-x-hidden overflow-y-auto overscroll-contain p-1.5",
        className,
      )}
      {...props}
    />
  );
}

export function CommandEmpty(props: React.ComponentProps<typeof CommandPrimitive.Empty>) {
  return (
    <CommandPrimitive.Empty
      data-slot="command-empty"
      className="px-3 py-6 text-center text-ui text-muted-foreground"
      {...props}
    />
  );
}

export function CommandGroup({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Group>) {
  return (
    <CommandPrimitive.Group
      data-slot="command-group"
      className={cn(
        "[&_[cmdk-group-heading]]:px-2.5 [&_[cmdk-group-heading]]:pt-2.5 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:text-micro [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:text-faint",
        className,
      )}
      {...props}
    />
  );
}

export function CommandSeparator({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Separator>) {
  return (
    <CommandPrimitive.Separator
      data-slot="command-separator"
      className={cn("-mx-1.5 my-1 h-px bg-divider", className)}
      {...props}
    />
  );
}

export function CommandItem({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Item>) {
  return (
    <CommandPrimitive.Item
      data-slot="command-item"
      className={cn(
        "relative flex h-9 cursor-default items-center gap-2.5 rounded-lg px-2.5 text-ui text-text-soft outline-none select-none pointer-coarse:h-11 pointer-coarse:text-[15px]",
        "data-[selected=true]:bg-hover data-[selected=true]:text-foreground data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50",
        "[&_svg]:shrink-0 [&_svg]:text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}

export function CommandShortcut({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="command-shortcut"
      // No keyboard shortcuts to show on a touch screen.
      className={cn("ml-auto text-micro tracking-wide text-faint pointer-coarse:hidden", className)}
      {...props}
    />
  );
}
